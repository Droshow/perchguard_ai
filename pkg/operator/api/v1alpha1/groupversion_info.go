// Package v1alpha1 contains the AgentIsolationPolicy CRD types for PerchGuard's
// K8s-native isolation operator (Phase 10b).
// +kubebuilder:object:generate=true
// +groupName=security.perchguard.io
package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

var (
	// GroupVersion is group version used to register these objects.
	GroupVersion = schema.GroupVersion{Group: "security.perchguard.io", Version: "v1alpha1"}

	// SchemeBuilder is used to add go types to the GroupVersionKind scheme.
	// (controller-runtime marks this Builder deprecated in favor of hand-rolling an
	// apimachinery runtime.SchemeBuilder — kept here since it's still what kubebuilder
	// itself scaffolds today and keeps this file to the standard, well-known shape.)
	SchemeBuilder = &scheme.Builder{GroupVersion: GroupVersion}

	// AddToScheme adds the types in this group-version to the given scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)
