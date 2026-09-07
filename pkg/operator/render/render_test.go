package render

import (
	"testing"

	networkingv1 "k8s.io/api/networking/v1"

	"github.com/Droshow/PerchGuard/perchguard/pkg/operator/api/v1alpha1"
	"github.com/Droshow/PerchGuard/perchguard/pkg/policy"
)

// baseExfilConfig mirrors pkg/admission/validator/data_exfiltration_test.go's
// baseExfilPolicy fixture, wrapped in the full policy.Config the render package
// consumes, so both layers are proven against the same rule set.
func baseExfilConfig() *policy.Config {
	return &policy.Config{
		Policies: policy.Policies{
			DataExfiltration: policy.DataExfiltrationPolicy{
				Enabled:                  true,
				AllowedDestinations:      []string{"api.internal.company.com", "storage.googleapis.com"},
				BlockedDestinations:      []string{"attacker.com", "*.ngrok.io"},
				BlockUnknownDestinations: false,
			},
		},
	}
}

func TestSpec_MirrorsDataExfiltrationPolicy(t *testing.T) {
	cfg := baseExfilConfig()
	spec := Spec(cfg, "deadbeef")

	if got, want := spec.Egress.AllowedDestinations, cfg.Policies.DataExfiltration.AllowedDestinations; !equalStrings(got, want) {
		t.Errorf("AllowedDestinations = %v, want %v", got, want)
	}
	if got, want := spec.Egress.BlockedDestinations, cfg.Policies.DataExfiltration.BlockedDestinations; !equalStrings(got, want) {
		t.Errorf("BlockedDestinations = %v, want %v", got, want)
	}
	if spec.Egress.BlockUnknownDestinations != cfg.Policies.DataExfiltration.BlockUnknownDestinations {
		t.Errorf("BlockUnknownDestinations = %v, want %v", spec.Egress.BlockUnknownDestinations, cfg.Policies.DataExfiltration.BlockUnknownDestinations)
	}
	if spec.SourceConfigRef.ContentHash != "deadbeef" {
		t.Errorf("ContentHash = %q, want %q", spec.SourceConfigRef.ContentHash, "deadbeef")
	}
	if spec.Mode != v1alpha1.PolicyModeObserve {
		t.Errorf("Mode = %q, want Observe by default", spec.Mode)
	}
}

func TestSpec_AllowedPortsDefaultedWhenEmpty(t *testing.T) {
	cfg := baseExfilConfig() // AllowedPorts left unset
	spec := Spec(cfg, "h")

	if got, want := spec.Egress.AllowedPorts, DefaultAllowedPorts; !equalInt32(got, want) {
		t.Errorf("AllowedPorts = %v, want default %v", got, want)
	}
}

func TestSpec_AllowedPortsFromPolicyWhenSet(t *testing.T) {
	cfg := baseExfilConfig()
	cfg.Policies.DataExfiltration.AllowedPorts = []int{8443, 9090}
	spec := Spec(cfg, "h")

	want := []int32{8443, 9090}
	if got := spec.Egress.AllowedPorts; !equalInt32(got, want) {
		t.Errorf("AllowedPorts = %v, want %v", got, want)
	}
}

func TestNetworkPolicy_DefaultDenyExceptDNSAndAllowedPorts(t *testing.T) {
	egress := v1alpha1.EgressPolicy{AllowedPorts: []int32{443, 80}}
	np := NetworkPolicy("insurance-agent", egress)

	if np.Namespace != "insurance-agent" {
		t.Errorf("Namespace = %q, want insurance-agent", np.Namespace)
	}
	if len(np.Spec.PolicyTypes) != 1 || np.Spec.PolicyTypes[0] != networkingv1.PolicyTypeEgress {
		t.Errorf("PolicyTypes = %v, want [Egress] (default-deny requires declaring the type with no matching ingress rule)", np.Spec.PolicyTypes)
	}
	if len(np.Spec.PodSelector.MatchLabels) != 0 || len(np.Spec.PodSelector.MatchExpressions) != 0 {
		t.Errorf("PodSelector should be empty (selects all pods in namespace), got %+v", np.Spec.PodSelector)
	}

	// Rule 0: DNS on 53/udp + 53/tcp.
	if len(np.Spec.Egress) != 2 {
		t.Fatalf("expected 2 egress rules (DNS, allowed ports), got %d", len(np.Spec.Egress))
	}
	dnsPorts := np.Spec.Egress[0].Ports
	if len(dnsPorts) != 2 {
		t.Fatalf("expected 2 DNS ports (udp+tcp), got %d", len(dnsPorts))
	}
	for _, p := range dnsPorts {
		if p.Port == nil || p.Port.IntVal != 53 {
			t.Errorf("DNS rule port = %v, want 53", p.Port)
		}
	}

	// Rule 1: the allowed ports, TCP only.
	portRule := np.Spec.Egress[1].Ports
	if len(portRule) != 2 {
		t.Fatalf("expected 2 allowed-port entries, got %d", len(portRule))
	}
	seen := map[int32]bool{}
	for _, p := range portRule {
		if p.Protocol == nil || *p.Protocol != "TCP" {
			t.Errorf("allowed-port protocol = %v, want TCP", p.Protocol)
		}
		seen[p.Port.IntVal] = true
	}
	if !seen[443] || !seen[80] {
		t.Errorf("allowed ports = %v, want 443 and 80 present", seen)
	}
}

func TestNetworkPolicy_DefaultsPortsWhenEgressHasNone(t *testing.T) {
	np := NetworkPolicy("healthcare-agent", v1alpha1.EgressPolicy{})
	portRule := np.Spec.Egress[1].Ports
	if len(portRule) != len(DefaultAllowedPorts) {
		t.Fatalf("expected %d default ports, got %d", len(DefaultAllowedPorts), len(portRule))
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

func equalInt32(a, b []int32) bool {
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
