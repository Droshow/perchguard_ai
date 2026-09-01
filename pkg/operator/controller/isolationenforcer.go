// IsolationEnforcer is the Phase 10c reconciler: it turns the AgentIsolationPolicy
// singleton PolicyTranslator already wrote into CiliumNetworkPolicy objects — the
// FQDN-level egress enforcement plain NetworkPolicy structurally cannot provide
// (see pkg/operator/render/render.go's NetworkPolicy doc comment).
package controller

import (
	"context"
	"errors"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	ctrl "sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/Droshow/PerchGuard/perchguard/pkg/operator/api/v1alpha1"
	"github.com/Droshow/PerchGuard/perchguard/pkg/operator/render"
)

// IsolationEnforcer reconciles CiliumNetworkPolicy objects from the
// AgentIsolationPolicy singleton. Unlike PolicyTranslator, it only ever writes
// when spec.mode is Enforce — Observe (the shipped default) is a strict no-op, so
// deploying this reconciler has zero effect on cluster traffic until a human/
// GitOps process flips the singleton's mode.
type IsolationEnforcer struct {
	Client client.Client
}

// SetupWithManager watches the AgentIsolationPolicy singleton itself — its
// natural trigger is "PolicyTranslator already wrote a change," which decouples
// the two reconcilers cleanly without needing to fan one raw trigger channel out
// to two consumers.
func (r *IsolationEnforcer) SetupWithManager(mgr manager.Manager) error {
	return builder.ControllerManagedBy(mgr).
		Named("isolationenforcer").
		For(&v1alpha1.AgentIsolationPolicy{}).
		Complete(r)
}

// Reconcile loads the AgentIsolationPolicy singleton and, only when spec.mode is
// Enforce, renders and Server-Side-Applies a CiliumNetworkPolicy per matched
// namespace. Namespaces that were governed on a previous reconcile but no
// longer match (selector change, mode flipped away from Enforce) have their
// CiliumNetworkPolicy deleted rather than left stale. Any apply/delete failure
// sets a Degraded condition and requeues with backoff, mirroring
// PolicyTranslator's error handling.
func (r *IsolationEnforcer) Reconcile(ctx context.Context, _ ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	existing := &v1alpha1.AgentIsolationPolicy{}
	if err := r.Client.Get(ctx, types.NamespacedName{Name: SingletonName}, existing); err != nil {
		if apierrors.IsNotFound(err) {
			// PolicyTranslator hasn't created the singleton yet — nothing to do
			// until it does, at which point a create event retriggers this.
			return ctrl.Result{}, nil
		}
		return ctrl.Result{RequeueAfter: 15 * time.Second}, fmt.Errorf("get AgentIsolationPolicy/%s: %w", SingletonName, err)
	}

	var matchedNames []string
	if existing.Spec.Mode == v1alpha1.PolicyModeEnforce {
		ns, err := matchedNamespaces(ctx, r.Client, existing.Spec.WorkloadSelector)
		if err != nil {
			return ctrl.Result{RequeueAfter: 15 * time.Second}, fmt.Errorf("list namespaces: %w", err)
		}
		for _, n := range ns {
			matchedNames = append(matchedNames, n.Name)
		}
	}

	var applyErrs []error
	appliedNames := make([]string, 0, len(matchedNames))
	for _, nsName := range matchedNames {
		cnp := render.CiliumNetworkPolicy(nsName, existing.Spec.Egress)
		if err := r.Client.Patch(ctx, cnp, client.Apply, client.ForceOwnership, isolationEnforcerFieldOwner); err != nil {
			wrapped := fmt.Errorf("apply CiliumNetworkPolicy in %s: %w", nsName, err)
			applyErrs = append(applyErrs, wrapped)
			logger.Error(wrapped, "failed to apply CiliumNetworkPolicy")
			continue
		}
		appliedNames = append(appliedNames, nsName)
	}

	var previouslyApplied []string
	if existing.Status.CiliumNetworkPoliciesApplied != nil {
		previouslyApplied = existing.Status.CiliumNetworkPoliciesApplied.Namespaces
	}
	applyErrs = append(applyErrs, deleteStale(ctx, r.Client, previouslyApplied, appliedNames, func(ns string) client.Object {
		obj := &unstructured.Unstructured{}
		obj.SetAPIVersion("cilium.io/v2")
		obj.SetKind("CiliumNetworkPolicy")
		obj.SetName(render.CiliumNetworkPolicyName)
		obj.SetNamespace(ns)
		return obj
	})...)
	applyErr := errors.Join(applyErrs...)

	// Only the condition types this reconciler owns — see
	// AgentIsolationPolicyStatus's doc comment for why.
	conditions := ownedConditions(existing.Status.Conditions, conditionCiliumPoliciesApplied, conditionIsolationEnforcerDegraded)
	setCondition(&conditions, conditionCiliumPoliciesApplied, existing.Generation, applyErr == nil, "Applied", fmt.Sprintf("%d/%d namespaces", len(appliedNames), len(matchedNames)))
	if applyErr != nil {
		setCondition(&conditions, conditionIsolationEnforcerDegraded, existing.Generation, false, "CiliumApplyFailed", applyErr.Error())
	}

	// A fresh object for the status Server-Side-Apply, same reason as
	// PolicyTranslator.Reconcile's `status` object. LastAppliedHash/
	// ObservedGeneration/NetworkPoliciesApplied are deliberately left zero/nil —
	// those are owned by PolicyTranslator, not this reconciler.
	status := &v1alpha1.AgentIsolationPolicy{
		TypeMeta:   existing.TypeMeta,
		ObjectMeta: metav1.ObjectMeta{Name: SingletonName},
	}
	status.Status.Conditions = conditions
	status.Status.CiliumNetworkPoliciesApplied = &v1alpha1.NetworkPolicyStatus{Count: len(appliedNames), Namespaces: appliedNames}

	if err := r.Client.Status().Patch(ctx, status, client.Apply, client.ForceOwnership, isolationEnforcerFieldOwner); err != nil {
		return ctrl.Result{RequeueAfter: 15 * time.Second}, fmt.Errorf("apply AgentIsolationPolicy/%s status: %w", SingletonName, err)
	}

	if applyErr != nil {
		return ctrl.Result{RequeueAfter: 15 * time.Second}, applyErr
	}
	return ctrl.Result{}, nil
}
