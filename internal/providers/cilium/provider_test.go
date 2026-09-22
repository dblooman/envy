package cilium

import (
	"context"
	"strings"
	"testing"

	"github.com/dblooman/envy/internal/domain"
	apps "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kube "k8s.io/client-go/kubernetes/fake"
	gateway "sigs.k8s.io/gateway-api/pkg/client/clientset/versioned/fake"
)

func TestCiliumPrerequisites(t *testing.T) {
	k := kube.NewSimpleClientset(&corev1.ConfigMap{Name: "cilium-config", Namespace: "kube-system", Data: map[string]string{"kube-proxy-replacement": "true", "enable-l7-proxy": "true", "enable-gateway-api": "true"}}, &apps.DaemonSet{Name: "cilium", Namespace: "kube-system", Status: apps.DaemonSetStatus{DesiredNumberScheduled: 1, NumberReady: 1}})
	p := New(gateway.NewSimpleClientset(), k, "test", nil, "")
	if err := p.CheckPrerequisites(context.Background()); err != nil {
		t.Fatal(err)
	}

	cm, _ := k.CoreV1().ConfigMaps("kube-system").Get(context.Background(), "cilium-config", metav1.GetOptions{})
	cm.Data["enable-l7-proxy"] = "false"
	k.CoreV1().ConfigMaps("kube-system").Update(context.Background(), cm, metav1.UpdateOptions{})
	if err := p.CheckPrerequisites(context.Background()); err == nil {
		t.Fatal("missing L7 proxy accepted")
	}
}

func TestBaselineRequiresManagedCiliumEndpoint(t *testing.T) {
	k := kube.NewSimpleClientset(&corev1.ConfigMap{Name: "cilium-config", Namespace: "kube-system", Data: map[string]string{"kube-proxy-replacement": "true", "enable-l7-proxy": "true", "enable-gateway-api": "true"}}, &apps.DaemonSet{Name: "cilium", Namespace: "kube-system", Status: apps.DaemonSetStatus{DesiredNumberScheduled: 1, NumberReady: 1}}, &corev1.Service{Name: "api", Namespace: "baseline", Spec: corev1.ServiceSpec{Selector: map[string]string{"app": "api"}}}, &corev1.Pod{Name: "api-pod", Namespace: "baseline", Labels: map[string]string{"app": "api"}})
	p := New(gateway.NewSimpleClientset(), k, "test", nil, "").WithEndpointClient(dynamicfake.NewSimpleDynamicClient(runtime.NewScheme()))
	err := p.ValidateBaseline(context.Background(), domain.Baseline{Routing: domain.BaselineRouting{Namespace: "baseline"}, Components: map[string]domain.BaselineBinding{"api": {ServiceHost: "api.baseline.svc.cluster.local"}}}, nil)
	if err == nil || !strings.Contains(err.Error(), "Cilium-managed endpoint") {
		t.Fatalf("unmanaged baseline accepted: %v", err)
	}
}
