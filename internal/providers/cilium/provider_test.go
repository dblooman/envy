package cilium

import (
	"context"
	"errors"
	"testing"

	"github.com/dblooman/envy/internal/domain"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	gatewayclientfake "sigs.k8s.io/gateway-api/pkg/client/clientset/versioned/fake"
)

const (
	testNamespace       = "staging"
	testBaselineService = "service-b.staging.svc.cluster.local"
	testAggregateName   = "envy-service-b"
)

func testEntry(id string) domain.RouteEntry {
	return domain.RouteEntry{
		Domain: domain.RouteDomain{
			Namespace:     testNamespace,
			Gateway:       "platform-gateway",
			ServiceHost:   testBaselineService,
			Port:          8080,
			AggregateName: testAggregateName,
		},
		CompositionID:   id,
		Host:            "cmp-" + id + ".envy.localhost",
		DestinationHost: "service-b.envy-" + id + ".svc.cluster.local",
		Port:            8080,
		OwnershipToken:  "token-" + id,
	}
}

func TestCiliumNativeCECReconcile(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, map[schema.GroupVersionResource]string{
		ciliumEnvoyConfigGVR: "CiliumEnvoyConfigList",
	})

	p := NewNative(dyn, "test-install", func(context.Context) error { return nil })

	a := testEntry("a")
	snapshot := domain.RouteSnapshot{
		Domains:     []domain.RouteDomain{a.Domain},
		MeshEntries: []domain.RouteEntry{a},
		OwnedCompositions: map[string]string{
			"a": a.OwnershipToken,
		},
	}

	obs, err := p.Reconcile(ctx, snapshot)
	if err != nil {
		t.Fatalf("Reconcile error: %v", err)
	}
	if !obs.Ready {
		t.Fatalf("Expected ready observation, got: %v", obs)
	}

	cecClient := dyn.Resource(ciliumEnvoyConfigGVR).Namespace(testNamespace)
	got, err := cecClient.Get(ctx, testAggregateName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get created CiliumEnvoyConfig: %v", err)
	}

	spec, ok := got.Object["spec"].(map[string]any)
	if !ok {
		t.Fatalf("missing spec in CiliumEnvoyConfig: %v", got.Object)
	}
	services, ok := spec["services"].([]any)
	if !ok || len(services) == 0 {
		t.Fatalf("missing or empty services in CiliumEnvoyConfig")
	}

	// Idempotency: reconcile again
	dyn.ClearActions()
	if _, err = p.Reconcile(ctx, snapshot); err != nil {
		t.Fatalf("reconcile unchanged failed: %v", err)
	}
}

func TestCiliumGatewayAPIMode(t *testing.T) {
	ctx := context.Background()
	gwClient := gatewayclientfake.NewSimpleClientset()
	p := NewGatewayAPI(gwClient, "test-install", func(context.Context) error { return nil })

	a := testEntry("a")
	snapshot := domain.RouteSnapshot{
		MeshEntries:    []domain.RouteEntry{a},
		IngressEntries: []domain.RouteEntry{a},
		OwnedCompositions: map[string]string{
			"a": a.OwnershipToken,
		},
	}

	obs, err := p.Reconcile(ctx, snapshot)
	if err != nil {
		t.Fatalf("Reconcile error: %v", err)
	}
	if !obs.Ready {
		t.Fatalf("Expected ready observation, got: %v", obs)
	}

	// Check that HTTPRoute was created via Gateway API
	route, err := gwClient.GatewayV1().HTTPRoutes(testNamespace).Get(ctx, testAggregateName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get aggregate HTTPRoute in Gateway API mode: %v", err)
	}
	if len(route.Spec.Rules) != 2 { // composition-a + baseline
		t.Fatalf("expected 2 rules in aggregate HTTPRoute, got %d", len(route.Spec.Rules))
	}
}

func TestCiliumLeadershipGuard(t *testing.T) {
	scheme := runtime.NewScheme()
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, map[schema.GroupVersionResource]string{
		ciliumEnvoyConfigGVR: "CiliumEnvoyConfigList",
	})
	p := NewNative(dyn, "test-install", func(context.Context) error { return errors.New("lost leadership") })

	a := testEntry("a")
	_, err := p.Reconcile(context.Background(), domain.RouteSnapshot{
		Domains:     []domain.RouteDomain{a.Domain},
		MeshEntries: []domain.RouteEntry{a},
	})
	if err == nil {
		t.Fatal("expected error when leadership is lost")
	}
}
