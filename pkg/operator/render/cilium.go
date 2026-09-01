package render

import (
	"fmt"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/Droshow/PerchGuard/perchguard/pkg/operator/api/v1alpha1"
)

// CiliumNetworkPolicyName is the fixed name of the generated FQDN-level egress
// policy in every governed namespace.
const CiliumNetworkPolicyName = "perchguard-fqdn-egress"

// CiliumNetworkPolicy renders a CiliumNetworkPolicy for one governed namespace:
// deny all egress except DNS resolution and the FQDN entries in
// egress.AllowedDestinations that don't conflict with egress.BlockedDestinations,
// restricted to egress.AllowedPorts. This is the FQDN-level parity plain
// Kubernetes NetworkPolicy structurally cannot provide (see NetworkPolicy's doc
// comment) — the reason Phase 10c exists.
//
// Mirrors pkg/admission/validator/data_exfiltration.go's block-before-allow
// precedence and BlockUnknownDestinations gate — a code-review pass on the first
// version of this function found it silently dropped both, which would have let
// the network layer permit exactly what the app layer blocks. See the two
// unexported helpers below for how each is now honored, and their doc comments
// for where Cilium's FQDN engine can't achieve byte-for-byte parity with the
// app-layer matcher and what this renders instead in that case.
//
// Built as unstructured.Unstructured rather than a typed Cilium client object —
// the Cilium Go module and its transitive dependency tree aren't worth pulling
// into go.mod for a handful of fields. Same trick the Terraform side already uses
// for Gateway API objects (kubernetes_manifest blocks in deployments/eks/gateway.tf).
func CiliumNetworkPolicy(namespace string, egress v1alpha1.EgressPolicy) *unstructured.Unstructured {
	ports := egress.AllowedPorts
	if len(ports) == 0 {
		ports = DefaultAllowedPorts
	}

	portRules := make([]interface{}, 0, len(ports))
	for _, p := range ports {
		portRules = append(portRules, map[string]interface{}{
			"port":     strconv.Itoa(int(p)),
			"protocol": "TCP",
		})
	}

	egressRules := []interface{}{dnsRule()}

	allowed, skipped := excludeBlockedConflicts(egress.AllowedDestinations, egress.BlockedDestinations)
	for _, dest := range allowed {
		egressRules = append(egressRules, fqdnRules(dest, portRules)...)
	}

	scope := fmt.Sprintf(
		"fqdn allow-list — %d/%d configured allowedDestinations rendered (%d skipped: also matched by blockedDestinations, block takes precedence)",
		len(allowed), len(egress.AllowedDestinations), len(skipped),
	)
	if !egress.BlockUnknownDestinations {
		egressRules = append(egressRules, map[string]interface{}{
			"toFQDNs": []interface{}{map[string]interface{}{"matchPattern": "*"}},
			"toPorts": []interface{}{map[string]interface{}{"ports": portRules}},
		})
		scope += "; blockUnknownDestinations=false so unlisted FQDNs are also permitted here — Cilium's toFQDNs model can't express \"allow unknown except these\", so blockedDestinations is enforced by the app-layer DataExfiltrationValidator only in this mode, not by this network policy"
	}

	obj := &unstructured.Unstructured{}
	obj.SetAPIVersion("cilium.io/v2")
	obj.SetKind("CiliumNetworkPolicy")
	obj.SetName(CiliumNetworkPolicyName)
	obj.SetNamespace(namespace)
	obj.SetLabels(map[string]string{
		"app.kubernetes.io/managed-by": "perchguard-operator",
	})
	obj.SetAnnotations(map[string]string{
		"perchguard.io/enforcement-scope": scope,
	})
	_ = unstructured.SetNestedField(obj.Object, map[string]interface{}{}, "spec", "endpointSelector")
	_ = unstructured.SetNestedSlice(obj.Object, egressRules, "spec", "egress")

	return obj
}

// dnsRule is the mandatory DNS-resolution rule — required for any FQDN rule
// below to resolve at all.
func dnsRule() map[string]interface{} {
	return map[string]interface{}{
		"toEndpoints": []interface{}{
			map[string]interface{}{
				"matchLabels": map[string]interface{}{
					"k8s:io.kubernetes.pod.namespace": "kube-system",
					"k8s-app":                         "kube-dns",
				},
			},
		},
		"toPorts": []interface{}{
			map[string]interface{}{
				"ports": []interface{}{
					map[string]interface{}{"port": "53", "protocol": "ANY"},
				},
				"rules": map[string]interface{}{
					"dns": []interface{}{
						map[string]interface{}{"matchPattern": "*"},
					},
				},
			},
		},
	}
}

// fqdnRules renders the toFQDNs rule(s) for one AllowedDestinations entry.
//
// A "*."-prefixed entry (e.g. "*.ngrok.io") maps directly to Cilium's own
// matchPattern wildcard syntax — no translation needed.
//
// A bare entry (e.g. "hl7.org") is rendered as exact match PLUS a "*."
// subdomain wildcard, not exact match alone. The app-layer matcher
// (matchesHostPattern) treats a bare pattern as "hostname appears anywhere in
// the URL", which in practice means it matches subdomains too (docs.hl7.org
// matches "hl7.org") — configs/policies.yaml already relies on this for bare
// entries like "hl7.org"/"fhir.org". Cilium's FQDN engine is DNS-label based
// and has no equivalent of the app layer's raw substring match (which would
// also — incorrectly — match unrelated names like "evilhl7.org.attacker.com");
// exact+subdomain is the closest sound DNS-level interpretation of what a bare
// entry is meant to allow, and erring stricter than the app layer here is the
// safe failure direction for a security control.
func fqdnRules(dest string, portRules []interface{}) []interface{} {
	toPorts := []interface{}{map[string]interface{}{"ports": portRules}}
	if strings.HasPrefix(dest, "*.") {
		return []interface{}{
			map[string]interface{}{
				"toFQDNs": []interface{}{map[string]interface{}{"matchPattern": dest}},
				"toPorts": toPorts,
			},
		}
	}
	return []interface{}{
		map[string]interface{}{
			"toFQDNs": []interface{}{
				map[string]interface{}{"matchName": dest},
				map[string]interface{}{"matchPattern": "*." + dest},
			},
			"toPorts": toPorts,
		},
	}
}

// excludeBlockedConflicts drops any allowed entry that blockedDestinations also
// covers — mirrors data_exfiltration.go's block-before-allow precedence, which
// the first version of this function didn't: it rendered every allowed entry
// regardless of blockedDestinations, so a block-list carve-out under a broader
// allowed wildcard (allow "*.example.com", block "evil.example.com") was
// silently permitted at the network layer despite being denied at the app
// layer. A conflict is either entry being an exact string match, or a wildcard
// entry whose suffix covers the other (in either direction) — the network
// layer can't express "allow this wildcard except that one name", so the whole
// wildcard entry is dropped rather than partially honored.
func excludeBlockedConflicts(allowed, blocked []string) (kept, skipped []string) {
	for _, a := range allowed {
		conflict := false
		for _, b := range blocked {
			if a == b || wildcardCovers(a, b) || wildcardCovers(b, a) {
				conflict = true
				break
			}
		}
		if conflict {
			skipped = append(skipped, a)
		} else {
			kept = append(kept, a)
		}
	}
	return kept, skipped
}

// wildcardCovers reports whether wildcard pattern (e.g. "*.example.com")
// covers name (e.g. "evil.example.com" or "example.com" itself).
func wildcardCovers(wildcard, name string) bool {
	if !strings.HasPrefix(wildcard, "*.") {
		return false
	}
	suffix := wildcard[1:] // ".example.com"
	return name == suffix[1:] || strings.HasSuffix(name, suffix)
}
