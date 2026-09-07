// PerchGuard operator — translates configs/policies.yaml into real Kubernetes
// objects: AgentIsolationPolicy status + a coarse per-namespace NetworkPolicy
// (Phase 10b), plus a CiliumNetworkPolicy per namespace once spec.mode is
// Enforce (Phase 10c — Observe by default, so this reconciler is a no-op unless
// a human/GitOps process flips that gate). Deliberately a separate binary/
// Deployment from cmd/main.go's admission-control HTTP path: a controller-runtime
// manager issue here must not be able to take down /intercept.
//
// Run: go run cmd/perchguard-operator/main.go
// Env: PERCHGUARD_POLICY        ./configs/policies.yaml (mounted policies.yaml path)
//
//	PERCHGUARD_POLICY_INTERVAL 10s (poll interval for policy-file changes)
//	PERCHGUARD_METRICS_ADDR    :8081 (controller-runtime metrics/health server)
package main

import (
	"log"
	"os"
	"time"

	"github.com/go-logr/stdr"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"

	"github.com/Droshow/PerchGuard/perchguard/pkg/envutil"
	"github.com/Droshow/PerchGuard/perchguard/pkg/operator/api/v1alpha1"
	"github.com/Droshow/PerchGuard/perchguard/pkg/operator/controller"
	"github.com/Droshow/PerchGuard/perchguard/pkg/policy"
)

func main() {
	ctrl.SetLogger(stdr.New(log.New(os.Stderr, "", log.LstdFlags)))

	policyPath := envutil.GetEnv("PERCHGUARD_POLICY", "./configs/policies.yaml")
	pollInterval, err := time.ParseDuration(envutil.GetEnv("PERCHGUARD_POLICY_INTERVAL", "10s"))
	if err != nil {
		log.Fatalf("invalid PERCHGUARD_POLICY_INTERVAL: %v", err)
	}
	metricsAddr := envutil.GetEnv("PERCHGUARD_METRICS_ADDR", ":8081")

	// Fail fast on a broken policy file at startup, same as cmd/main.go's admission
	// path — better to crash-loop visibly than run with a stale/empty spec.
	initial, err := policy.LoadWithMeta(policyPath)
	if err != nil {
		log.Fatalf("load policy from %s: %v", policyPath, err)
	}

	scheme := clientgoscheme.Scheme
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		log.Fatalf("register AgentIsolationPolicy scheme: %v", err)
	}

	cfg, err := ctrl.GetConfig()
	if err != nil {
		log.Fatalf("load kubeconfig: %v", err)
	}

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:                 scheme,
		HealthProbeBindAddress: metricsAddr,
	})
	if err != nil {
		log.Fatalf("create manager: %v", err)
	}
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		log.Fatalf("register healthz check: %v", err)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		log.Fatalf("register readyz check: %v", err)
	}

	// Reduced to a single-signal trigger channel — Watcher's onReload payload isn't
	// needed here, PolicyTranslator.Reconcile always re-reads PolicyPath itself, so
	// both this file-watch trigger and the manager's own Namespace-change trigger
	// converge on the same "recompute everything" path.
	triggers := make(chan struct{}, 1)
	watcher := policy.NewWatcher(policyPath, pollInterval, func(*policy.LoadResult) {
		select {
		case triggers <- struct{}{}:
		default: // reconcile already pending, no need to queue a second one
		}
	})
	watcher.Start(initial.Hash)
	defer watcher.Stop()

	translator := &controller.PolicyTranslator{
		Client:     mgr.GetClient(),
		PolicyPath: policyPath,
	}
	if err := translator.SetupWithManager(mgr, triggers); err != nil {
		log.Fatalf("wire PolicyTranslator: %v", err)
	}

	enforcer := &controller.IsolationEnforcer{Client: mgr.GetClient()}
	if err := enforcer.SetupWithManager(mgr); err != nil {
		log.Fatalf("wire IsolationEnforcer: %v", err)
	}

	log.Printf("perchguard-operator starting: policy=%s metrics=%s", policyPath, metricsAddr)
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		log.Fatalf("manager exited: %v", err)
	}
}
