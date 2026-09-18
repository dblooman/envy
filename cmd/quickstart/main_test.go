package main

import (
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestInjectedPod(t *testing.T) {
	for _, tc := range []struct {
		name string
		pod  corev1.Pod
		want bool
	}{
		{name: "plain sample"},
		{name: "gateway placeholder", pod: corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "istio-proxy", Image: "auto"}}}}},
		{name: "injected", pod: corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{"sidecar.istio.io/status": "{}"}}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "istio-proxy", Image: "istio/proxyv2:1.31.0"}}}}, want: true},
		{name: "native sidecar", pod: corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{"sidecar.istio.io/status": "{}"}}, Spec: corev1.PodSpec{InitContainers: []corev1.Container{{Name: "istio-proxy", Image: "istio/proxyv2:1.31.0"}}}}, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := injectedPod(tc.pod); got != tc.want {
				t.Fatalf("injectedPod = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestUntil(t *testing.T) {
	if err := until(context.Background(), func() error { return nil }); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := until(ctx, func() error { return errors.New("not ready") }); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
}

func TestInjectionState(t *testing.T) {
	if injected, restart := injectionState(nil); injected || restart {
		t.Fatal("empty pod list should wait, not restart")
	}

	if injected, restart := injectionState([]corev1.Pod{{}}); injected || !restart {
		t.Fatal("uninjected pod should trigger a restart")
	}

	now := metav1.Now()
	if injected, restart := injectionState([]corev1.Pod{{ObjectMeta: metav1.ObjectMeta{DeletionTimestamp: &now}}}); injected || restart {
		t.Fatal("terminating pods should be ignored")
	}
}
