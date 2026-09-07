package controller

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ownedConditions filters a status's full Conditions list down to only the
// types the caller owns. AgentIsolationPolicyStatus.Conditions is a
// listType=map keyed on Type, and PolicyTranslator/IsolationEnforcer each
// Server-Side-Apply this one status subresource under distinct field owners —
// so each must submit only its own condition entries (seeded from the previous
// value of just those entries, for LastTransitionTime continuity) rather than
// round-tripping the whole list, which would make it the owner of condition
// types it doesn't actually manage.
func ownedConditions(all []metav1.Condition, types ...string) []metav1.Condition {
	owned := make(map[string]bool, len(types))
	for _, t := range types {
		owned[t] = true
	}
	var out []metav1.Condition
	for _, c := range all {
		if owned[c.Type] {
			out = append(out, c)
		}
	}
	return out
}

// setCondition finds-or-appends condType in conditions via apimachinery's own
// meta.SetStatusCondition — not a local reimplementation, which the first
// version of this file was (a code-review finding: it duplicated find/update/
// append/LastTransitionTime-preservation logic that k8s.io/apimachinery/pkg/api/meta
// already provides as a well-tested indirect dependency of this module).
func setCondition(conditions *[]metav1.Condition, condType string, observedGeneration int64, ok bool, reason, message string) {
	status := metav1.ConditionTrue
	if !ok {
		status = metav1.ConditionFalse
	}
	meta.SetStatusCondition(conditions, metav1.Condition{
		Type:               condType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: observedGeneration,
	})
}

// deleteStale removes newObj(ns) for every namespace in previous that isn't in
// current — the cleanup PolicyTranslator/IsolationEnforcer skipped before: when
// spec.mode flips Enforce→Observe or a namespace stops matching the selector,
// the policy object they'd applied earlier used to stay live in the cluster
// forever, silently diverging from what status claimed. A NotFound on delete is
// not an error (already gone, or never actually applied due to an earlier
// failure); errors are collected rather than returned on the first failure so
// one stuck namespace doesn't block cleanup of the others.
func deleteStale(ctx context.Context, cl client.Client, previous, current []string, newObj func(ns string) client.Object) []error {
	currentSet := make(map[string]struct{}, len(current))
	for _, ns := range current {
		currentSet[ns] = struct{}{}
	}
	var errs []error
	for _, ns := range previous {
		if _, ok := currentSet[ns]; ok {
			continue
		}
		if err := cl.Delete(ctx, newObj(ns)); err != nil && !apierrors.IsNotFound(err) {
			errs = append(errs, fmt.Errorf("delete stale policy in %s: %w", ns, err))
		}
	}
	return errs
}
