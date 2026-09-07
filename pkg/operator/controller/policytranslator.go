// Package controller holds the PolicyTranslator reconciler — the only reconciler in
// Phase 10b. It watches for policy/namespace changes and Server-Side-Applies a coarse
// egress NetworkPolicy per governed namespace plus the AgentIsolationPolicy/default
// status, gated by spec.mode (Observe = render only, Enforce = actually apply).
package controller

import (
	"context"
	"errors"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	ctrl "sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/Droshow/PerchGuard/perchguard/pkg/operator/api/v1alpha1"
	"github.com/Droshow/PerchGuard/perchguard/pkg/operator/render"
	"github.com/Droshow/PerchGuard/perchguard/pkg/policy"
)

const (
	// SingletonName is the fixed name of the one AgentIsolationPolicy this phase
	// manages — see PHASE10-K8S-ISOLATION-OPERATOR.md's CRD spec (singleton, name:
	// "default").
	SingletonName = "default"

	// Distinct field owners per reconciler — both Server-Side-Apply the same
	// AgentIsolationPolicy/default status subresource, and a code-review pass
	// found that sharing one owner name let a stale read-modify-write from one
	// clobber a newer write from the other. See AgentIsolationPolicyStatus's doc
	// comment for the full fix (pointer subfields + listType=map Conditions).
	policyTranslatorFieldOwner  = client.FieldOwner("perchguard-policytranslator")
	isolationEnforcerFieldOwner = client.FieldOwner("perchguard-isolationenforcer")

	conditionReconciled             = "Reconciled"
	conditionNetworkPoliciesApplied = "NetworkPoliciesApplied"
	conditionCiliumPoliciesApplied  = "CiliumNetworkPoliciesApplied"
	// Distinct Degraded condition types per reconciler for the same reason as the
	// distinct field owners above — a shared "Degraded" type would be the same
	// listType=map key contested by two owners.
	conditionPolicyTranslatorDegraded  = "PolicyTranslatorDegraded"
	conditionIsolationEnforcerDegraded = "CiliumEnforcementDegraded"
)

// PolicyTranslator reconciles cluster state (NetworkPolicy per governed namespace,
// AgentIsolationPolicy/default status) from a policy.Config loaded from disk.
//
// The config is read via pkg/policy.LoadWithMeta — the exact function
// cmd/main.go's admission path uses — from a ConfigMap volume mount, not a second,
// independent watch of the ConfigMap object through the K8s API. This keeps there
// being exactly one parser of the policy file in this deployment, avoiding the
// two-clocks divergence a duplicate watch would introduce (see
// PHASE10-K8S-ISOLATION-OPERATOR.md's Phase 10b design notes).
type PolicyTranslator struct {
	Client client.Client

	// PolicyPath is the mounted policies.yaml path (same file the admission
	// controller reads from its own copy of the perchguard-policies ConfigMap).
	PolicyPath string
}

// SetupWithManager wires the reconciler to watch Namespace changes (so a newly
// perchguard/intercept: "true"-labeled namespace gets a NetworkPolicy without
// waiting on a policy edit), the AgentIsolationPolicy singleton itself (so an
// Observe→Enforce flip re-renders immediately instead of waiting on an unrelated
// Namespace event or policy-file edit), and a generic trigger channel fed by a
// pkg/policy.Watcher on PolicyPath (so a policy edit re-renders without polling the
// K8s API a second time for the same data). Reconcile ignores the triggering
// request's identity and always recomputes the singleton, so any source firing
// is equivalent — only the wake-up matters.
//
// The Namespace watch carries a label predicate so routine churn from
// non-governed namespaces (CI namespaces, ephemeral test namespaces) doesn't
// trigger a full reconcile — a policy-file read, a namespace List, and N+2
// sequential Patch calls — for events this reconciler has no reason to act on.
func (r *PolicyTranslator) SetupWithManager(mgr manager.Manager, triggers <-chan struct{}) error {
	ch := make(chan event.GenericEvent)
	go func() {
		for range triggers {
			ch <- event.GenericEvent{Object: &v1alpha1.AgentIsolationPolicy{ObjectMeta: metav1.ObjectMeta{Name: SingletonName}}}
		}
	}()

	return builder.ControllerManagedBy(mgr).
		Named("policytranslator").
		For(&corev1.Namespace{}, builder.WithPredicates(interceptLabelPredicate())).
		Watches(&v1alpha1.AgentIsolationPolicy{}, &handler.EnqueueRequestForObject{}).
		WatchesRawSource(source.Channel(ch, &handler.EnqueueRequestForObject{})).
		Complete(r)
}

// interceptLabelPredicate matches namespaces carrying render.InterceptLabelSelector's
// labels — the same convention the Fargate/k3s manifests and the webhook's own
// namespaceSelector already use, so this isn't a second opt-in convention.
// CRITICAL FIX: must detect both ADD (label added to namespace) and REMOVE
// (label removed from namespace) events. The old implementation only checked
// ObjectNew, so label removals were silently dropped: a namespace would keep
// its NetworkPolicy indefinitely with no error, falsifying the "opt-out works"
// contract. Now checks both old and new: UpdateFunc returns true if either
// object has the labels (so a transition from "has labels" → "no labels" still
// triggers), and CreateFunc/DeleteFunc check only the single object available.
func interceptLabelPredicate() predicate.Predicate {
	hasLabels := func(obj client.Object) bool {
		for k, v := range render.InterceptLabelSelector.MatchLabels {
			if obj.GetLabels()[k] != v {
				return false
			}
		}
		return true
	}
	
	return predicate.Funcs{
		CreateFunc: func(ce event.CreateEvent) bool {
			return hasLabels(ce.Object)
		},
		UpdateFunc: func(ue event.UpdateEvent) bool {
			// Trigger if EITHER old or new has the labels (so label removal still
			// triggers reconciliation to clean up the policy). If a namespace is
			// transitioning out of governance, we need the event to fire so deleteStale
			// can remove its NetworkPolicy.
			return hasLabels(ue.ObjectOld) || hasLabels(ue.ObjectNew)
		},
		DeleteFunc: func(de event.DeleteEvent) bool {
			return hasLabels(de.Object)
		},
	}
}

// Reconcile loads the current policy, lists namespaces matching
// spec.workloadSelector.namespaceSelector, renders a NetworkPolicy per matched
// namespace plus the AgentIsolationPolicy/default status, and Server-Side-Applies
// all of it when spec.mode is Enforce. Namespaces that were governed on a
// previous reconcile but no longer match (selector change, mode flipped away
// from Enforce) have their NetworkPolicy deleted rather than left stale. Any
// apply/delete failure sets a Degraded condition and requeues with backoff
// instead of leaving status stale until the next unrelated trigger.
func (r *PolicyTranslator) Reconcile(ctx context.Context, _ ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	loaded, err := policy.LoadWithMeta(r.PolicyPath)
	if err != nil {
		return ctrl.Result{RequeueAfter: 30 * time.Second}, fmt.Errorf("load policy from %s: %w", r.PolicyPath, err)
	}

	spec := render.Spec(loaded.Config, loaded.Hash)

	existing := &v1alpha1.AgentIsolationPolicy{}
	err = r.Client.Get(ctx, types.NamespacedName{Name: SingletonName}, existing)
	switch {
	case err == nil:
		spec.Mode = existing.Spec.Mode // operator renders; a human/GitOps owns the Observe/Enforce gate
	case apierrors.IsNotFound(err):
		// first reconcile — default to Observe (render only, apply nothing) until a
		// human flips the singleton to Enforce.
	default:
		return ctrl.Result{RequeueAfter: 15 * time.Second}, fmt.Errorf("get AgentIsolationPolicy/%s: %w", SingletonName, err)
	}

	var matched []corev1.Namespace
	if spec.Mode == v1alpha1.PolicyModeEnforce {
		matched, err = matchedNamespaces(ctx, r.Client, spec.WorkloadSelector)
		if err != nil {
			return ctrl.Result{RequeueAfter: 15 * time.Second}, fmt.Errorf("list namespaces: %w", err)
		}
	}

	var applyErrs []error
	appliedNames := make([]string, 0, len(matched))
	for _, ns := range matched {
		np := render.NetworkPolicy(ns.Name, spec.Egress)
		if err := r.Client.Patch(ctx, np, client.Apply, client.ForceOwnership, policyTranslatorFieldOwner); err != nil {
			wrapped := fmt.Errorf("apply NetworkPolicy in %s: %w", ns.Name, err)
			applyErrs = append(applyErrs, wrapped)
			logger.Error(wrapped, "failed to apply coarse NetworkPolicy")
			continue
		}
		appliedNames = append(appliedNames, ns.Name)
	}

	var previouslyApplied []string
	if existing.Status.NetworkPoliciesApplied != nil {
		previouslyApplied = existing.Status.NetworkPoliciesApplied.Namespaces
	}
	applyErrs = append(applyErrs, deleteStale(ctx, r.Client, previouslyApplied, appliedNames, func(ns string) client.Object {
		return &networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: render.NetworkPolicyName, Namespace: ns}}
	})...)
	applyErr := errors.Join(applyErrs...)

	desired := &v1alpha1.AgentIsolationPolicy{
		TypeMeta:   metav1.TypeMeta{APIVersion: v1alpha1.GroupVersion.String(), Kind: "AgentIsolationPolicy"},
		ObjectMeta: metav1.ObjectMeta{Name: SingletonName},
		Spec:       spec,
	}
	if err := r.Client.Patch(ctx, desired, client.Apply, client.ForceOwnership, policyTranslatorFieldOwner); err != nil {
		return ctrl.Result{RequeueAfter: 15 * time.Second}, fmt.Errorf("apply AgentIsolationPolicy/%s spec: %w", SingletonName, err)
	}

	// Only the condition types this reconciler owns — see AgentIsolationPolicyStatus's
	// doc comment for why. Seeded from `existing` so meta.SetStatusCondition still
	// preserves LastTransitionTime for a condition whose Status didn't change.
	conditions := ownedConditions(existing.Status.Conditions, conditionReconciled, conditionNetworkPoliciesApplied, conditionPolicyTranslatorDegraded)
	setCondition(&conditions, conditionReconciled, desired.Generation, applyErr == nil, "PolicyTranslated", "policy content parsed and translated")
	setCondition(&conditions, conditionNetworkPoliciesApplied, desired.Generation, applyErr == nil, "Applied", fmt.Sprintf("%d/%d namespaces", len(appliedNames), len(matched)))
	if applyErr != nil {
		setCondition(&conditions, conditionPolicyTranslatorDegraded, desired.Generation, false, "ApplyFailed", applyErr.Error())
	}

	// A fresh object for the status Server-Side-Apply below: reusing `desired`
	// would carry over the managedFields the spec Patch call above just populated
	// onto it from the server's response, and an SSA request must be sent with
	// managedFields nil. CiliumNetworkPoliciesApplied is deliberately left nil —
	// that subfield is owned by IsolationEnforcer, not this reconciler.
	status := &v1alpha1.AgentIsolationPolicy{
		TypeMeta:   desired.TypeMeta,
		ObjectMeta: metav1.ObjectMeta{Name: SingletonName},
	}
	status.Status.Conditions = conditions
	status.Status.LastAppliedHash = loaded.Hash
	status.Status.ObservedGeneration = desired.Generation
	status.Status.NetworkPoliciesApplied = &v1alpha1.NetworkPolicyStatus{Count: len(appliedNames), Namespaces: appliedNames}

	if err := r.Client.Status().Patch(ctx, status, client.Apply, client.ForceOwnership, policyTranslatorFieldOwner); err != nil {
		return ctrl.Result{RequeueAfter: 15 * time.Second}, fmt.Errorf("apply AgentIsolationPolicy/%s status: %w", SingletonName, err)
	}

	if applyErr != nil {
		return ctrl.Result{RequeueAfter: 15 * time.Second}, applyErr
	}
	return ctrl.Result{}, nil
}
