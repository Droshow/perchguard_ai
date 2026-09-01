// Package render turns a parsed PerchGuard policy config into Kubernetes objects.
// Pure functions only — no cluster access, no I/O — so they're unit-testable against
// the same fixtures pkg/admission/validator uses, and reusable from both the
// reconciler and its tests.
package render

import (
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/Droshow/PerchGuard/perchguard/pkg/operator/api/v1alpha1"
	"github.com/Droshow/PerchGuard/perchguard/pkg/policy"
)

// NetworkPolicyName is the fixed name of the generated coarse egress NetworkPolicy in
// every governed namespace.
const NetworkPolicyName = "perchguard-coarse-egress"

// DefaultAllowedPorts is used when configs/policies.yaml's
// dataExfiltration.allowedPorts is empty, so existing configs don't need updating.
var DefaultAllowedPorts = []int32{443, 80}

// InterceptLabelSelector is the namespace label PerchGuard's webhook and this
// operator both select on — deployments/k3s/webhook-config.yaml's
// namespaceSelector uses the identical key/value.
var InterceptLabelSelector = &metav1.LabelSelector{
	MatchLabels: map[string]string{"perchguard/intercept": "true"},
}

// Spec translates configs/policies.yaml's dataExfiltration block into an
// AgentIsolationPolicySpec — a 1:1 mirror for everything except AllowedPorts
// (defaulted here when unset) and AllowDNS (always true this phase; DNS is required
// for any egress to resolve, not currently a configurable knob).
func Spec(cfg *policy.Config, sourceHash string) v1alpha1.AgentIsolationPolicySpec {
	return v1alpha1.AgentIsolationPolicySpec{
		Mode:             v1alpha1.PolicyModeObserve,
		WorkloadSelector: v1alpha1.WorkloadSelector{NamespaceSelector: InterceptLabelSelector},
		Egress: v1alpha1.EgressPolicy{
			AllowedDestinations:      cfg.Policies.DataExfiltration.AllowedDestinations,
			BlockedDestinations:      cfg.Policies.DataExfiltration.BlockedDestinations,
			BlockUnknownDestinations: cfg.Policies.DataExfiltration.BlockUnknownDestinations,
			AllowedPorts:             allowedPorts(cfg),
			AllowDNS:                 true,
		},
		Sandbox: v1alpha1.SandboxPolicy{Enabled: false},
		SourceConfigRef: v1alpha1.SourceConfigRef{
			ConfigMapName:      "perchguard-policies",
			ConfigMapNamespace: "perchguard",
			ContentHash:        sourceHash,
		},
	}
}

func allowedPorts(cfg *policy.Config) []int32 {
	src := cfg.Policies.DataExfiltration.AllowedPorts
	if len(src) == 0 {
		return append([]int32(nil), DefaultAllowedPorts...)
	}
	out := make([]int32, len(src))
	for i, p := range src {
		out[i] = int32(p)
	}
	return out
}

// NetworkPolicy renders the coarse default-deny-egress NetworkPolicy for one
// governed namespace: deny all egress except DNS and the spec's allowed ports.
//
// This intentionally does NOT enforce the hostname-level allow/block lists in
// spec.Egress — plain Kubernetes NetworkPolicy can only match podSelector/
// namespaceSelector/CIDR + ports, not hostnames. Per-hostname parity with the
// app-layer DataExfiltrationValidator needs Cilium's toFQDNs (Phase 10c). Callers
// must not present this object as equivalent to the validator's rule set.
func NetworkPolicy(namespace string, egress v1alpha1.EgressPolicy) *networkingv1.NetworkPolicy {
	ports := egress.AllowedPorts
	if len(ports) == 0 {
		ports = DefaultAllowedPorts
	}

	tcp := corev1.ProtocolTCP
	udp := corev1.ProtocolUDP
	dnsPort := intstr.FromInt32(53)

	rules := []networkingv1.NetworkPolicyEgressRule{
		{
			// DNS — required for any resolution to work at all, regardless of policy.
			Ports: []networkingv1.NetworkPolicyPort{
				{Protocol: &udp, Port: &dnsPort},
				{Protocol: &tcp, Port: &dnsPort},
			},
		},
	}

	allowed := make([]networkingv1.NetworkPolicyPort, 0, len(ports))
	for _, p := range ports {
		port := intstr.FromInt32(p)
		allowed = append(allowed, networkingv1.NetworkPolicyPort{Protocol: &tcp, Port: &port})
	}
	if len(allowed) > 0 {
		rules = append(rules, networkingv1.NetworkPolicyEgressRule{Ports: allowed})
	}

	return &networkingv1.NetworkPolicy{
		TypeMeta: metav1.TypeMeta{APIVersion: "networking.k8s.io/v1", Kind: "NetworkPolicy"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      NetworkPolicyName,
			Namespace: namespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "perchguard-operator",
			},
			Annotations: map[string]string{
				"perchguard.io/enforcement-scope": "coarse-port-only — hostname allow/block lists are not enforced at this layer, see Phase 10c",
			},
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{}, // empty selector = all pods in the namespace
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress},
			Egress:      rules,
		},
	}
}
