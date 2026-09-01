package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PolicyMode gates whether the operator only renders objects (Observe) or actually
// applies them to the cluster (Enforce).
// +kubebuilder:validation:Enum=Observe;Enforce
type PolicyMode string

const (
	PolicyModeObserve PolicyMode = "Observe"
	PolicyModeEnforce PolicyMode = "Enforce"
)

// WorkloadSelector selects which namespaces this policy governs.
type WorkloadSelector struct {
	NamespaceSelector *metav1.LabelSelector `json:"namespaceSelector,omitempty"`
}

// EgressPolicy is a 1:1 translation of configs/policies.yaml's dataExfiltration
// block, plus fields needed to render a coarse NetworkPolicy today and a
// CiliumNetworkPolicy in Phase 10c.
type EgressPolicy struct {
	AllowedDestinations      []string `json:"allowedDestinations,omitempty"`
	BlockedDestinations      []string `json:"blockedDestinations,omitempty"`
	BlockUnknownDestinations bool     `json:"blockUnknownDestinations,omitempty"`

	// AllowedPorts are the ports this phase's coarse NetworkPolicy allows to any
	// destination — plain NetworkPolicy cannot match the hostnames above (Phase 10c,
	// Cilium FQDN rules, does that). Defaults to [443, 80] when empty.
	AllowedPorts []int32 `json:"allowedPorts,omitempty"`

	// AllowDNS is always true in this phase (DNS is required for any egress to
	// resolve) — not currently configurable, kept as a field for Phase 10c parity.
	AllowDNS bool `json:"allowDNS,omitempty"`
}

// SandboxPolicy is inert in this phase — reserved for Phase 10c/10d RuntimeClass work.
type SandboxPolicy struct {
	Enabled          bool   `json:"enabled,omitempty"`
	RuntimeClassName string `json:"runtimeClassName,omitempty"`
}

// SourceConfigRef points at the ConfigMap this policy was translated from, for
// provenance/drift-detection — keyed off the same content-hash scheme as
// pkg/policy/watcher.go.
type SourceConfigRef struct {
	ConfigMapName      string `json:"configMapName,omitempty"`
	ConfigMapNamespace string `json:"configMapNamespace,omitempty"`
	ContentHash        string `json:"contentHash,omitempty"`
}

// AgentIsolationPolicySpec defines the desired isolation posture for governed agent
// namespaces. This is a singleton (name: default) in Phase 10b/10c.
type AgentIsolationPolicySpec struct {
	Mode             PolicyMode       `json:"mode,omitempty"`
	WorkloadSelector WorkloadSelector `json:"workloadSelector,omitempty"`
	Egress           EgressPolicy     `json:"egress,omitempty"`
	Sandbox          SandboxPolicy    `json:"sandbox,omitempty"`
	SourceConfigRef  SourceConfigRef  `json:"sourceConfigRef,omitempty"`
}

// NetworkPolicyStatus reports which namespaces currently have a generated
// NetworkPolicy applied.
type NetworkPolicyStatus struct {
	Count      int      `json:"count"`
	Namespaces []string `json:"namespaces,omitempty"`
}

// AgentIsolationPolicyStatus reports the operator's last-reconciled state.
// NetworkPoliciesApplied/CiliumNetworkPoliciesApplied are pointers, and
// Conditions is a listType=map keyed on Type, so that PolicyTranslator and
// IsolationEnforcer — two reconcilers Server-Side-Applying this one status
// subresource under distinct field owners — can each omit the subfields and
// condition types the other one owns instead of round-tripping a stale
// snapshot of them on every reconcile (a dueling-writer bug a code-review pass
// caught: both reconcilers submitting the same fields under the same owner let
// a stale read-modify-write from one clobber a newer write from the other).
type AgentIsolationPolicyStatus struct {
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// LastAppliedHash and NetworkPoliciesApplied are owned by PolicyTranslator.
	LastAppliedHash        string               `json:"lastAppliedHash,omitempty"`
	NetworkPoliciesApplied *NetworkPolicyStatus `json:"networkPoliciesApplied,omitempty"`

	// CiliumNetworkPoliciesApplied is owned by IsolationEnforcer.
	CiliumNetworkPoliciesApplied *NetworkPolicyStatus `json:"ciliumNetworkPoliciesApplied,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=aip

// AgentIsolationPolicy is the cluster-scoped singleton (name: default) that
// translates configs/policies.yaml's egress rules into real Kubernetes objects.
type AgentIsolationPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AgentIsolationPolicySpec   `json:"spec,omitempty"`
	Status AgentIsolationPolicyStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// AgentIsolationPolicyList contains a list of AgentIsolationPolicy.
type AgentIsolationPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AgentIsolationPolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AgentIsolationPolicy{}, &AgentIsolationPolicyList{})
}
