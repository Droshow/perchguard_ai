package controller

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Droshow/PerchGuard/perchguard/pkg/operator/api/v1alpha1"
)

// matchedNamespaces lists the namespaces matching spec.workloadSelector — shared
// between PolicyTranslator (coarse NetworkPolicy) and IsolationEnforcer
// (CiliumNetworkPolicy), which both need the identical lookup.
func matchedNamespaces(ctx context.Context, cl client.Client, ws v1alpha1.WorkloadSelector) ([]corev1.Namespace, error) {
	if ws.NamespaceSelector == nil {
		return nil, nil
	}
	sel, err := metav1.LabelSelectorAsSelector(ws.NamespaceSelector)
	if err != nil {
		return nil, fmt.Errorf("invalid namespaceSelector: %w", err)
	}
	var list corev1.NamespaceList
	if err := cl.List(ctx, &list, &client.ListOptions{LabelSelector: sel}); err != nil {
		return nil, err
	}
	return list.Items, nil
}
