// PerchGuard operator — translates configs/policies.yaml into real Kubernetes
// objects: AgentIsolationPolicy status + a coarse per-namespace NetworkPolicy
// (Phase 10b), plus a CiliumNetworkPolicy per namespace once spec.mode is
// Enforce (Phase 10c — Observe by default, so this reconciler is a no-op unless
// a human/GitOps process flips that gate). Deliberately a separate binary/
// Deployment from cmd/main.go's admission-control HTTP path: a controller-runtime
// manager issue here must not be able to take down /intercept.
//
// Also serves the Phase 10e Kata-injection mutating webhook on the same manager
// (see pkg/operator/webhook) — chosen over a separate binary/Deployment because
// it shares the same RBAC/TLS/deploy surface and neither reconciler nor webhook
// can take down /intercept regardless.
//
// Run: go run cmd/perchguard-operator/main.go
// Env: PERCHGUARD_POLICY          ./configs/policies.yaml (mounted policies.yaml path)
//
//	PERCHGUARD_POLICY_INTERVAL   10s (poll interval for policy-file changes)
//	PERCHGUARD_METRICS_ADDR      :8081 (controller-runtime metrics/health server)
//	PERCHGUARD_WEBHOOK_PORT      9443 (mutating webhook HTTPS port)
//	PERCHGUARD_WEBHOOK_CERT_DIR  /tmp/k8s-webhook-server/serving-certs (tls.crt/tls.key dir)
//	PERCHGUARD_KATA_RUNTIME_CLASS kata-qemu (RuntimeClass name injected into pods)
package main

import (
	"log"
	"os"
	"strconv"
	"time"

	"github.com/go-logr/stdr"
	corev1 "k8s.io/api/core/v1"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/Droshow/PerchGuard/perchguard/pkg/envutil"
	"github.com/Droshow/PerchGuard/perchguard/pkg/operator/api/v1alpha1"
	"github.com/Droshow/PerchGuard/perchguard/pkg/operator/controller"
	pgwebhook "github.com/Droshow/PerchGuard/perchguard/pkg/operator/webhook"
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
	webhookPort, err := strconv.Atoi(envutil.GetEnv("PERCHGUARD_WEBHOOK_PORT", "9443"))
	if err != nil {
		log.Fatalf("invalid PERCHGUARD_WEBHOOK_PORT: %v", err)
	}
	webhookCertDir := envutil.GetEnv("PERCHGUARD_WEBHOOK_CERT_DIR", "/tmp/k8s-webhook-server/serving-certs")
	kataRuntimeClass := envutil.GetEnv("PERCHGUARD_KATA_RUNTIME_CLASS", "kata-qemu")

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
		WebhookServer: webhook.NewServer(webhook.Options{
			Port:    webhookPort,
			CertDir: webhookCertDir,
		}),
	})
	if err != nil {
		log.Fatalf("create manager: %v", err)
	}

	// Phase 10e: mutating webhook injecting the Kata RuntimeClass. Which
	// namespaces this is even invoked for is scoped at the Kubernetes level via
	// deployments/k3s/kata/mutating-webhook.yaml's namespaceSelector, not here.
	mgr.GetWebhookServer().Register(
		"/mutate-agent-pods-kata",
		admission.WithDefaulter[*corev1.Pod](scheme, &pgwebhook.KataInjector{RuntimeClassName: kataRuntimeClass}),
	)
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
