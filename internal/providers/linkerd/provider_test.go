package linkerd

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/fake"
)

func TestServiceProfilesBlockEnvyRoutes(t *testing.T) {
	sp := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "linkerd.io/v1alpha2", "kind": "ServiceProfile", "metadata": map[string]any{"name": "service-b.staging.svc.cluster.local", "namespace": "caller"}}}
	dyn := fake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{serviceProfiles: "ServiceProfileList"}, sp)
	p := &Provider{dynamic: dyn}
	if p.conflicts(context.Background(), map[string]bool{"service-b.staging.svc.cluster.local": true}) == nil {
		t.Fatal("ServiceProfile conflict ignored")
	}

	if e := p.conflicts(context.Background(), map[string]bool{"unrelated": true}); e != nil {
		t.Fatal(e)
	}
}
