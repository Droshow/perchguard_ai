package webhook

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestKataInjector_Default_InjectsRuntimeClassWhenUnset(t *testing.T) {
	pod := &corev1.Pod{}
	injector := &KataInjector{RuntimeClassName: "kata-qemu"}

	if err := injector.Default(context.Background(), pod); err != nil {
		t.Fatalf("Default returned error: %v", err)
	}

	if pod.Spec.RuntimeClassName == nil || *pod.Spec.RuntimeClassName != "kata-qemu" {
		t.Fatalf("expected RuntimeClassName=kata-qemu, got %v", pod.Spec.RuntimeClassName)
	}
}

func TestKataInjector_Default_LeavesExplicitRuntimeClassAlone(t *testing.T) {
	existing := "gvisor"
	pod := &corev1.Pod{Spec: corev1.PodSpec{RuntimeClassName: &existing}}
	injector := &KataInjector{RuntimeClassName: "kata-qemu"}

	if err := injector.Default(context.Background(), pod); err != nil {
		t.Fatalf("Default returned error: %v", err)
	}

	if pod.Spec.RuntimeClassName == nil || *pod.Spec.RuntimeClassName != "gvisor" {
		t.Fatalf("expected explicit RuntimeClassName=gvisor to survive, got %v", pod.Spec.RuntimeClassName)
	}
}
