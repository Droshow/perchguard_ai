package controller

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/Droshow/PerchGuard/perchguard/pkg/operator/api/v1alpha1"
	"github.com/Droshow/PerchGuard/perchguard/pkg/operator/render"
)

// Fixtures mirror pkg/operator/render's baseExfilConfig/render_test.go, wrapped in a
// real policies.yaml so PolicyTranslator exercises the exact pkg/policy.LoadWithMeta
// path the running operator uses — a mounted file, not a second ConfigMap watch (see
// PolicyTranslator's doc comment).
const fixturePolicy = `
policies:
  dataExfiltration:
    enabled: true
    allowedDestinations: ["api.internal.company.com"]
    blockedDestinations: ["attacker.com"]
    blockUnknownDestinations: false
`

const fixturePolicyMutated = `
policies:
  dataExfiltration:
    enabled: true
    allowedDestinations: ["api.internal.company.com", "storage.googleapis.com"]
    blockedDestinations: ["attacker.com", "*.ngrok.io"]
    blockUnknownDestinations: true
`

// setupEnvtest starts a real kube-apiserver/etcd pair with the AgentIsolationPolicy
// CRD installed and returns a client.Client scoped to it — shared bootstrap for
// every envtest-based reconciler test in this package. Needs KUBEBUILDER_ASSETS
// set; install via `go run sigs.k8s.io/controller-runtime/tools/setup-envtest@latest
// use` and export what it prints, or let `setup-envtest use -p env` do both.
func setupEnvtest(t *testing.T) client.Client {
	t.Helper()

	repoRoot, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}

	env := &envtest.Environment{
		// deployments/k3s holds more than CRDs (Deployments, RBAC, ...) but envtest
		// silently skips any YAML doc whose kind isn't CustomResourceDefinition, so
		// pointing at the whole directory is safe and needs no fixture duplication.
		CRDDirectoryPaths:     []string{filepath.Join(repoRoot, "deployments", "k3s")},
		ErrorIfCRDPathMissing: true,
	}
	cfg, err := env.Start()
	if err != nil {
		t.Fatalf("start envtest environment (is KUBEBUILDER_ASSETS set? see setup-envtest): %v", err)
	}
	t.Cleanup(func() {
		if err := env.Stop(); err != nil {
			t.Errorf("stop envtest environment: %v", err)
		}
	})

	scheme := clientgoscheme.Scheme
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("register AgentIsolationPolicy scheme: %v", err)
	}
	cl, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatalf("create client: %v", err)
	}
	return cl
}

// TestPolicyTranslator_Reconcile is the envtest-based reconciler test called for in
// PHASE10-K8S-ISOLATION-OPERATOR.md's testing/verification section: apply a fixture
// policy file, reconcile, assert AgentIsolationPolicy/default gets the translated
// spec and a Reconciled condition; mutate the file, reconcile again, assert
// status.lastAppliedHash changes.
func TestPolicyTranslator_Reconcile(t *testing.T) {
	cl := setupEnvtest(t)

	policyPath := filepath.Join(t.TempDir(), "policies.yaml")
	if err := os.WriteFile(policyPath, []byte(fixturePolicy), 0o644); err != nil {
		t.Fatalf("write fixture policy: %v", err)
	}

	r := &PolicyTranslator{Client: cl, PolicyPath: policyPath}
	ctx := context.Background()

	if _, err := r.Reconcile(ctx, reconcile.Request{}); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}

	got := &v1alpha1.AgentIsolationPolicy{}
	if err := cl.Get(ctx, types.NamespacedName{Name: SingletonName}, got); err != nil {
		t.Fatalf("get AgentIsolationPolicy/%s: %v", SingletonName, err)
	}
	if want := []string{"api.internal.company.com"}; !equalStrings(got.Spec.Egress.AllowedDestinations, want) {
		t.Errorf("AllowedDestinations = %v, want %v", got.Spec.Egress.AllowedDestinations, want)
	}
	if cond := meta.FindStatusCondition(got.Status.Conditions, conditionReconciled); cond == nil || cond.Status != metav1.ConditionTrue {
		t.Errorf("condition %s = %+v, want status True", conditionReconciled, cond)
	}
	firstHash := got.Status.LastAppliedHash
	if firstHash == "" {
		t.Fatal("LastAppliedHash is empty after first reconcile")
	}

	if err := os.WriteFile(policyPath, []byte(fixturePolicyMutated), 0o644); err != nil {
		t.Fatalf("write mutated fixture policy: %v", err)
	}
	if _, err := r.Reconcile(ctx, reconcile.Request{}); err != nil {
		t.Fatalf("second reconcile: %v", err)
	}

	updated := &v1alpha1.AgentIsolationPolicy{}
	if err := cl.Get(ctx, types.NamespacedName{Name: SingletonName}, updated); err != nil {
		t.Fatalf("get AgentIsolationPolicy/%s after mutation: %v", SingletonName, err)
	}
	if updated.Status.LastAppliedHash == firstHash {
		t.Errorf("LastAppliedHash did not change after policy mutation: still %s", firstHash)
	}
	if want := []string{"api.internal.company.com", "storage.googleapis.com"}; !equalStrings(updated.Spec.Egress.AllowedDestinations, want) {
		t.Errorf("AllowedDestinations after mutation = %v, want %v", updated.Spec.Egress.AllowedDestinations, want)
	}
}

// TestPolicyTranslator_Reconcile_DeletesStaleNetworkPolicyOnUnenforce is the
// regression test for the code-review finding that neither reconciler ever
// deleted a previously-applied policy object: apply a NetworkPolicy under
// Enforce, flip the singleton to Observe, reconcile again, and assert the
// NetworkPolicy is actually gone rather than just absent from status.
func TestPolicyTranslator_Reconcile_DeletesStaleNetworkPolicyOnUnenforce(t *testing.T) {
	cl := setupEnvtest(t)
	ctx := context.Background()

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Name:   "insurance-agent",
		Labels: map[string]string{"perchguard/intercept": "true"},
	}}
	if err := cl.Create(ctx, ns); err != nil {
		t.Fatalf("create namespace: %v", err)
	}

	policyPath := filepath.Join(t.TempDir(), "policies.yaml")
	if err := os.WriteFile(policyPath, []byte(fixturePolicy), 0o644); err != nil {
		t.Fatalf("write fixture policy: %v", err)
	}

	r := &PolicyTranslator{Client: cl, PolicyPath: policyPath}

	// First reconcile creates the singleton (defaults to Observe); flip it to
	// Enforce and reconcile again so the NetworkPolicy actually gets applied.
	if _, err := r.Reconcile(ctx, reconcile.Request{}); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	aip := &v1alpha1.AgentIsolationPolicy{}
	if err := cl.Get(ctx, types.NamespacedName{Name: SingletonName}, aip); err != nil {
		t.Fatalf("get AgentIsolationPolicy/%s: %v", SingletonName, err)
	}
	aip.Spec.Mode = v1alpha1.PolicyModeEnforce
	if err := cl.Update(ctx, aip); err != nil {
		t.Fatalf("flip mode to Enforce: %v", err)
	}
	if _, err := r.Reconcile(ctx, reconcile.Request{}); err != nil {
		t.Fatalf("reconcile under Enforce: %v", err)
	}

	np := &networkingv1.NetworkPolicy{}
	if err := cl.Get(ctx, types.NamespacedName{Name: render.NetworkPolicyName, Namespace: ns.Name}, np); err != nil {
		t.Fatalf("NetworkPolicy not applied under Enforce: %v", err)
	}

	// Flip back to Observe and reconcile — the previously-applied NetworkPolicy
	// must be deleted, not left stale.
	if err := cl.Get(ctx, types.NamespacedName{Name: SingletonName}, aip); err != nil {
		t.Fatalf("re-get AgentIsolationPolicy/%s: %v", SingletonName, err)
	}
	aip.Spec.Mode = v1alpha1.PolicyModeObserve
	if err := cl.Update(ctx, aip); err != nil {
		t.Fatalf("flip mode back to Observe: %v", err)
	}
	if _, err := r.Reconcile(ctx, reconcile.Request{}); err != nil {
		t.Fatalf("reconcile back under Observe: %v", err)
	}

	err := cl.Get(ctx, types.NamespacedName{Name: render.NetworkPolicyName, Namespace: ns.Name}, &networkingv1.NetworkPolicy{})
	if !apierrors.IsNotFound(err) {
		t.Fatalf("stale NetworkPolicy still present after Enforce→Observe: err = %v", err)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
