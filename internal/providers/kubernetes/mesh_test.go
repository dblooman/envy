package kubernetes

import (
	"context"
	"github.com/dblooman/envy/internal/domain"
	core "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/fake"
	"testing"
)

func TestBaselineMeshParticipation(t *testing.T) {
	for _, tc := range []struct {
		mesh, proxy string
		valid       bool
	}{{"cilium", "", true}, {"linkerd", "linkerd-proxy", true}, {"linkerd", "", false}, {"istio", "istio-proxy", true}, {"istio", "linkerd-proxy", false}} {
		t.Run(tc.mesh+tc.proxy, func(t *testing.T) {
			k := fake.NewSimpleClientset(&core.Namespace{Name: "baseline"}, &core.Service{Name: "api", Namespace: "baseline", Spec: core.ServiceSpec{Selector: map[string]string{"app": "api"}, Ports: []core.ServicePort{{Name: "http", Port: 8080, Protocol: core.ProtocolTCP}}}}, &core.Pod{Name: "api", Namespace: "baseline", Labels: map[string]string{"app": "api"}, Spec: core.PodSpec{Containers: []core.Container{{Name: "api"}}}, Status: core.PodStatus{Conditions: []core.PodCondition{{Type: core.PodReady, Status: core.ConditionTrue}}, ContainerStatuses: []core.ContainerStatus{{Name: tc.proxy, Ready: true}}}})
			p := NewWithInjection(k, "test", nil, map[string]string{}).WithMesh(tc.mesh)
			err := p.ValidateBaseline(context.Background(), domain.Baseline{Routing: domain.BaselineRouting{Namespace: "baseline"}, Components: map[string]domain.BaselineBinding{"api": {ServiceHost: "api.baseline.svc.cluster.local", Port: 8080}}}, nil)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v err=%v", tc.valid, err)
			}
		})
	}
}

func TestOwnedLinkerdWorkloadsHaveQuotaCompatibleProxyResources(t *testing.T) {
	p := NewWithInjection(fake.NewSimpleClientset(), "test", nil, map[string]string{}).WithMesh("linkerd")
	for _, key := range []string{"config.linkerd.io/proxy-cpu-request", "config.linkerd.io/proxy-cpu-limit", "config.linkerd.io/proxy-memory-request", "config.linkerd.io/proxy-memory-limit"} {
		if p.podAnnotations[key] == "" {
			t.Fatalf("missing injected container resource: %s", key)
		}
	}
	if len(p.injection) != 0 || p.podAnnotations["linkerd.io/inject"] != "enabled" {
		t.Fatal("Linkerd injection configuration is not isolated")
	}
}
