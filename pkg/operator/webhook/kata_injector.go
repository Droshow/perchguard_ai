// Package webhook holds the Phase 10e mutating admission webhook that injects
// Kata Containers' RuntimeClass into pods scheduled in opted-in namespaces.
//
// Deliberately fail-open-safe: this webhook is a defense-in-depth convenience
// (it saves an agent-workload deployment from having to set runtimeClassName
// itself), not the enforcement boundary. Network policy (pkg/operator/render/cilium.go,
// applied by IsolationEnforcer) is the real boundary and doesn't depend on this
// webhook's uptime — see deployments/k3s/kata/mutating-webhook.yaml's
// failurePolicy: Ignore.
package webhook

import (
	"context"

	corev1 "k8s.io/api/core/v1"
)

// KataInjector implements admission.Defaulter[*corev1.Pod] (wired via
// admission.WithDefaulter in cmd/perchguard-operator/main.go). Scoping to which
// namespaces this even gets called for is done at the Kubernetes level via the
// MutatingWebhookConfiguration's namespaceSelector (perchguard/sandbox: "kata"),
// the same opt-in-label pattern deployments/k3s/webhook-config.yaml already uses
// for perchguard/intercept — so this type does no namespace filtering itself.
type KataInjector struct {
	// RuntimeClassName is the RuntimeClass to inject, e.g. "kata-qemu".
	RuntimeClassName string
}

// Default injects RuntimeClassName into pod.Spec.RuntimeClassName unless the pod
// already specifies one — an explicit runtimeClassName on the pod spec is a
// deliberate choice by whoever wrote the manifest and is left alone.
func (k *KataInjector) Default(_ context.Context, pod *corev1.Pod) error {
	if pod.Spec.RuntimeClassName != nil && *pod.Spec.RuntimeClassName != "" {
		return nil
	}
	rc := k.RuntimeClassName
	pod.Spec.RuntimeClassName = &rc
	return nil
}
