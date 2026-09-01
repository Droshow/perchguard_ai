package render

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/Droshow/PerchGuard/perchguard/pkg/operator/api/v1alpha1"
)

func TestCiliumNetworkPolicy_FQDNRulesFromAllowedDestinations(t *testing.T) {
	egress := v1alpha1.EgressPolicy{
		AllowedDestinations:      []string{"api.internal.company.com", "*.ngrok.io"},
		BlockUnknownDestinations: true,
		AllowedPorts:             []int32{443},
	}

	obj := CiliumNetworkPolicy("insurance-agent", egress)

	if got, want := obj.GetAPIVersion(), "cilium.io/v2"; got != want {
		t.Errorf("APIVersion = %q, want %q", got, want)
	}
	if got, want := obj.GetKind(), "CiliumNetworkPolicy"; got != want {
		t.Errorf("Kind = %q, want %q", got, want)
	}
	if got, want := obj.GetName(), CiliumNetworkPolicyName; got != want {
		t.Errorf("Name = %q, want %q", got, want)
	}
	if got, want := obj.GetNamespace(), "insurance-agent"; got != want {
		t.Errorf("Namespace = %q, want %q", got, want)
	}

	rules, found, err := unstructured.NestedSlice(obj.Object, "spec", "egress")
	if err != nil || !found {
		t.Fatalf("spec.egress not found: found=%v err=%v", found, err)
	}
	// DNS rule + one toFQDNs rule per AllowedDestinations entry (no catch-all —
	// BlockUnknownDestinations is true).
	if want := 1 + len(egress.AllowedDestinations); len(rules) != want {
		t.Fatalf("len(spec.egress) = %d, want %d", len(rules), want)
	}

	// Bare entry renders exact match + "*."-subdomain wildcard, not exact-only —
	// see fqdnRules' doc comment for why (docs.hl7.org-style regression this fixes).
	exactRule := rules[1].(map[string]interface{})
	fqdns, _, _ := unstructured.NestedSlice(exactRule, "toFQDNs")
	if len(fqdns) != 2 {
		t.Fatalf("len(toFQDNs) for bare entry = %d, want 2 (matchName + subdomain matchPattern)", len(fqdns))
	}
	if matchName, _, _ := unstructured.NestedString(fqdns[0].(map[string]interface{}), "matchName"); matchName != "api.internal.company.com" {
		t.Errorf("toFQDNs[0].matchName = %q, want %q", matchName, "api.internal.company.com")
	}
	if matchPattern, _, _ := unstructured.NestedString(fqdns[1].(map[string]interface{}), "matchPattern"); matchPattern != "*.api.internal.company.com" {
		t.Errorf("toFQDNs[1].matchPattern = %q, want %q", matchPattern, "*.api.internal.company.com")
	}

	wildcardRule := rules[2].(map[string]interface{})
	fqdns, _, _ = unstructured.NestedSlice(wildcardRule, "toFQDNs")
	if len(fqdns) != 1 {
		t.Fatalf("len(toFQDNs) for wildcard entry = %d, want 1", len(fqdns))
	}
	if matchPattern, _, _ := unstructured.NestedString(fqdns[0].(map[string]interface{}), "matchPattern"); matchPattern != "*.ngrok.io" {
		t.Errorf("toFQDNs[0].matchPattern = %q, want %q", matchPattern, "*.ngrok.io")
	}

	toPorts, _, _ := unstructured.NestedSlice(exactRule, "toPorts")
	ports, _, _ := unstructured.NestedSlice(toPorts[0].(map[string]interface{}), "ports")
	if len(ports) != 1 {
		t.Fatalf("len(toPorts[0].ports) = %d, want 1", len(ports))
	}
	if port, _, _ := unstructured.NestedString(ports[0].(map[string]interface{}), "port"); port != "443" {
		t.Errorf("port = %q, want %q", port, "443")
	}
}

func TestCiliumNetworkPolicy_DefaultsPortsWhenEgressHasNone(t *testing.T) {
	obj := CiliumNetworkPolicy("ns", v1alpha1.EgressPolicy{
		AllowedDestinations:      []string{"example.com"},
		BlockUnknownDestinations: true,
	})

	rules, _, _ := unstructured.NestedSlice(obj.Object, "spec", "egress")
	toPorts, _, _ := unstructured.NestedSlice(rules[1].(map[string]interface{}), "toPorts")
	ports, _, _ := unstructured.NestedSlice(toPorts[0].(map[string]interface{}), "ports")
	if len(ports) != len(DefaultAllowedPorts) {
		t.Fatalf("len(ports) = %d, want %d (DefaultAllowedPorts)", len(ports), len(DefaultAllowedPorts))
	}
}

// TestCiliumNetworkPolicy_BlockedDestinationConflictIsExcluded is the regression
// test for the code-review finding: a wildcard AllowedDestinations entry that
// covers a BlockedDestinations entry must not be rendered as an allow rule —
// the network layer must not permit what the app layer explicitly blocks.
func TestCiliumNetworkPolicy_BlockedDestinationConflictIsExcluded(t *testing.T) {
	egress := v1alpha1.EgressPolicy{
		AllowedDestinations:      []string{"*.example.com", "safe.other.com"},
		BlockedDestinations:      []string{"evil.example.com"},
		BlockUnknownDestinations: true,
	}

	obj := CiliumNetworkPolicy("ns", egress)
	rules, _, _ := unstructured.NestedSlice(obj.Object, "spec", "egress")

	// DNS rule + only the non-conflicting "safe.other.com" entry — the
	// "*.example.com" wildcard is dropped entirely because it covers the
	// blocked "evil.example.com".
	if len(rules) != 2 {
		t.Fatalf("len(spec.egress) = %d, want 2 (DNS + safe.other.com only)", len(rules))
	}

	allowedRule := rules[1].(map[string]interface{})
	fqdns, _, _ := unstructured.NestedSlice(allowedRule, "toFQDNs")
	if matchName, _, _ := unstructured.NestedString(fqdns[0].(map[string]interface{}), "matchName"); matchName != "safe.other.com" {
		t.Errorf("remaining allowed entry = %q, want %q", matchName, "safe.other.com")
	}

	annotations := obj.GetAnnotations()
	scope := annotations["perchguard.io/enforcement-scope"]
	if scope == "" {
		t.Fatal("perchguard.io/enforcement-scope annotation missing")
	}
}

// TestCiliumNetworkPolicy_BlockUnknownFalseAddsCatchAll is the regression test
// for the other confirmed finding: blockUnknownDestinations=false must actually
// permit unlisted FQDNs at the network layer, not silently render a strict
// allow-list-only policy regardless of the configured value.
func TestCiliumNetworkPolicy_BlockUnknownFalseAddsCatchAll(t *testing.T) {
	egress := v1alpha1.EgressPolicy{
		AllowedDestinations:      []string{"api.internal.company.com"},
		BlockUnknownDestinations: false,
	}

	obj := CiliumNetworkPolicy("ns", egress)
	rules, _, _ := unstructured.NestedSlice(obj.Object, "spec", "egress")

	// DNS + the one allowed entry + a catch-all matchPattern:"*" rule.
	if len(rules) != 3 {
		t.Fatalf("len(spec.egress) = %d, want 3 (DNS + allowed entry + catch-all)", len(rules))
	}
	catchAll := rules[2].(map[string]interface{})
	fqdns, _, _ := unstructured.NestedSlice(catchAll, "toFQDNs")
	if matchPattern, _, _ := unstructured.NestedString(fqdns[0].(map[string]interface{}), "matchPattern"); matchPattern != "*" {
		t.Errorf("catch-all matchPattern = %q, want %q", matchPattern, "*")
	}
}
