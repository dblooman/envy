package gatewayapi

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/dblooman/envy/internal/domain"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
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

func TestGatewayAPIAggregateSnapshotsAndReferenceGrants(t *testing.T) {
	ctx := context.Background()
	client := gatewayclientfake.NewSimpleClientset()
	p := New(client, "test-install", func(context.Context) error { return nil })

	a, b := testEntry("a"), testEntry("b")
	snapshot := domain.RouteSnapshot{
		MeshEntries:    []domain.RouteEntry{b, a},
		IngressEntries: []domain.RouteEntry{a, b},
		OwnedCompositions: map[string]string{
			"a": a.OwnershipToken,
			"b": b.OwnershipToken,
		},
	}

	obs, err := p.Reconcile(ctx, snapshot)
	if err != nil {
		t.Fatalf("Reconcile error: %v", err)
	}
	if !obs.Ready {
		t.Fatalf("Expected ready observation, got: %v", obs)
	}

	// 1. Verify Mesh Aggregate HTTPRoute
	route, err := client.GatewayV1().HTTPRoutes(testNamespace).Get(ctx, testAggregateName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get aggregate route: %v", err)
	}
	if len(route.Spec.Rules) != 3 {
		t.Fatalf("expected 3 rules (comp-a, comp-b, baseline), got %d", len(route.Spec.Rules))
	}
	if string(*route.Spec.Rules[0].Name) != "composition-a" || string(*route.Spec.Rules[2].Name) != "baseline" {
		t.Fatalf("unexpected rule order: %v, %v", *route.Spec.Rules[0].Name, *route.Spec.Rules[2].Name)
	}

	// Verify header match on composition-a
	ruleA := route.Spec.Rules[0]
	if len(ruleA.Matches) == 0 || len(ruleA.Matches[0].Headers) == 0 {
		t.Fatalf("missing header match on composition-a")
	}
	if ruleA.Matches[0].Headers[0].Name != "baggage" {
		t.Fatalf("expected header 'baggage', got '%s'", ruleA.Matches[0].Headers[0].Name)
	}

	// 2. Verify Ingress HTTPRoute
	ingressA, err := client.GatewayV1().HTTPRoutes(testNamespace).Get(ctx, "envy-ingress-a", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get ingress-a: %v", err)
	}
	if len(ingressA.Spec.Rules) != 1 || len(ingressA.Spec.Rules[0].Filters) != 2 {
		t.Fatalf("expected 1 rule with 2 filters on ingress-a")
	}
	filters := ingressA.Spec.Rules[0].Filters
	if filters[0].Type != gatewayv1.HTTPRouteFilterRequestHeaderModifier || filters[0].RequestHeaderModifier.Set[0].Value != "composition=a" {
		t.Fatalf("request baggage header filter missing or invalid")
	}
	if filters[1].Type != gatewayv1.HTTPRouteFilterResponseHeaderModifier || filters[1].ResponseHeaderModifier.Set[0].Value != "a" {
		t.Fatalf("response preview route header filter missing or invalid")
	}

	// 3. Verify ReferenceGrant in envy-a
	rgA, err := client.GatewayV1beta1().ReferenceGrants("envy-a").Get(ctx, "envy-allow-mesh-routing", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("missing ReferenceGrant in envy-a: %v", err)
	}
	if string(rgA.Spec.From[0].Namespace) != testNamespace {
		t.Fatalf("ReferenceGrant from namespace mismatch: %s", rgA.Spec.From[0].Namespace)
	}

	// 4. Idempotency check: reconcile again with unchanged snapshot
	client.ClearActions()
	if _, err = p.Reconcile(ctx, snapshot); err != nil {
		t.Fatalf("reconcile unchanged failed: %v", err)
	}
	for _, action := range client.Actions() {
		if action.GetVerb() == "update" || action.GetVerb() == "create" || action.GetVerb() == "delete" {
			t.Fatalf("unchanged snapshot caused mutation: %s %s", action.GetVerb(), action.GetResource().Resource)
		}
	}

	// 5. Ingress route teardown
	snapshot.IngressEntries = []domain.RouteEntry{b}
	if _, err = p.Reconcile(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	if _, err = client.GatewayV1().HTTPRoutes(testNamespace).Get(ctx, "envy-ingress-a", metav1.GetOptions{}); err == nil {
		t.Fatal("deleted ingress route still present")
	}

	// 6. Mesh route teardown
	snapshot.MeshEntries = []domain.RouteEntry{b}
	if _, err = p.Reconcile(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	route, _ = client.GatewayV1().HTTPRoutes(testNamespace).Get(ctx, testAggregateName, metav1.GetOptions{})
	if len(route.Spec.Rules) != 2 || string(*route.Spec.Rules[0].Name) != "composition-b" {
		t.Fatalf("expected 2 rules with comp-b, got %d", len(route.Spec.Rules))
	}
}

func TestGatewayAPILostLeadershipPreventsMutation(t *testing.T) {
	client := gatewayclientfake.NewSimpleClientset()
	p := New(client, "test-install", func(context.Context) error { return errors.New("lost leader") })

	a := testEntry("a")
	_, err := p.Reconcile(context.Background(), domain.RouteSnapshot{
		MeshEntries:    []domain.RouteEntry{a},
		IngressEntries: []domain.RouteEntry{a},
	})
	if err == nil {
		t.Fatal("expected error on lost leadership guard")
	}
	for _, action := range client.Actions() {
		if action.GetVerb() == "create" || action.GetVerb() == "update" || action.GetVerb() == "delete" {
			t.Fatalf("mutation performed despite leadership loss: %s", action.GetVerb())
		}
	}
}

func TestGatewayAPIValidateBaseline(t *testing.T) {
	ctx := context.Background()
	wildcardHost := gatewayv1.Hostname("*.envy.localhost")
	gwGroup := gatewayv1.Group(gatewayv1.GroupName)
	gwKind := gatewayv1.Kind("Gateway")
	port8080 := gatewayv1.PortNumber(8080)

	newGateway := func() *gatewayv1.Gateway {
		return &gatewayv1.Gateway{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "platform-gateway",
				Namespace: "staging",
			},
			Spec: gatewayv1.GatewaySpec{
				GatewayClassName: "linkerd",
				Listeners: []gatewayv1.Listener{
					{
						Name:     "http",
						Port:     80,
						Protocol: gatewayv1.HTTPProtocolType,
						Hostname: &wildcardHost,
					},
				},
			},
		}
	}

	baseline := domain.Baseline{
		ID:       "staging",
		Endpoint: "http://baseline.envy.localhost",
		Routing: domain.BaselineRouting{
			Namespace:      "staging",
			Gateway:        "platform-gateway",
			EntryComponent: "service-b",
		},
		Components: map[string]domain.BaselineBinding{
			"service-b": {
				ServiceHost: "service-b.staging.svc.cluster.local",
				Port:        8080,
			},
		},
	}

	validBaselineRoute := func() *gatewayv1.HTTPRoute {
		return &gatewayv1.HTTPRoute{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "baseline-ingress",
				Namespace: "staging",
			},
			Spec: gatewayv1.HTTPRouteSpec{
				CommonRouteSpec: gatewayv1.CommonRouteSpec{
					ParentRefs: []gatewayv1.ParentReference{
						{
							Group: &gwGroup,
							Kind:  &gwKind,
							Name:  gatewayv1.ObjectName("platform-gateway"),
						},
					},
				},
				Hostnames: []gatewayv1.Hostname{"baseline.envy.localhost"},
				Rules: []gatewayv1.HTTPRouteRule{
					{
						Filters: []gatewayv1.HTTPRouteFilter{
							{
								Type: gatewayv1.HTTPRouteFilterRequestHeaderModifier,
								RequestHeaderModifier: &gatewayv1.HTTPHeaderFilter{
									Remove: []string{"baggage"},
								},
							},
						},
						BackendRefs: []gatewayv1.HTTPBackendRef{
							{
								BackendRef: gatewayv1.BackendRef{
									BackendObjectReference: gatewayv1.BackendObjectReference{
										Name: gatewayv1.ObjectName("service-b"),
										Port: &port8080,
									},
								},
							},
						},
					},
				},
			},
		}
	}

	t.Run("missing route", func(t *testing.T) {
		client := gatewayclientfake.NewSimpleClientset()
		_, _ = client.GatewayV1().Gateways(testNamespace).Create(ctx, newGateway(), metav1.CreateOptions{})
		p := NewWithGatewayClass(client, "test-install", func(context.Context) error { return nil }, "linkerd")
		err := p.ValidateBaseline(ctx, baseline, nil)
		if err == nil || err.Error() != "baseline requires exactly one existing ingress route" {
			t.Fatalf("expected missing route error, got: %v", err)
		}
	})

	t.Run("valid route with baggage removal", func(t *testing.T) {
		client := gatewayclientfake.NewSimpleClientset()
		_, _ = client.GatewayV1().Gateways(testNamespace).Create(ctx, newGateway(), metav1.CreateOptions{})
		_, _ = client.GatewayV1().HTTPRoutes(testNamespace).Create(ctx, validBaselineRoute(), metav1.CreateOptions{})
		p := NewWithGatewayClass(client, "test-install", func(context.Context) error { return nil }, "linkerd")
		if err := p.ValidateBaseline(ctx, baseline, nil); err != nil {
			t.Fatalf("expected valid baseline route to pass, got: %v", err)
		}
	})

	t.Run("missing baggage removal filter", func(t *testing.T) {
		client := gatewayclientfake.NewSimpleClientset()
		_, _ = client.GatewayV1().Gateways(testNamespace).Create(ctx, newGateway(), metav1.CreateOptions{})
		route := validBaselineRoute()
		route.Spec.Rules[0].Filters = nil
		_, _ = client.GatewayV1().HTTPRoutes(testNamespace).Create(ctx, route, metav1.CreateOptions{})
		p := NewWithGatewayClass(client, "test-install", func(context.Context) error { return nil }, "linkerd")
		err := p.ValidateBaseline(ctx, baseline, nil)
		if err == nil || err.Error() != "baseline ingress requires a direct route with baggage removal" {
			t.Fatalf("expected direct route error, got: %v", err)
		}
	})

	t.Run("sets baggage error", func(t *testing.T) {
		client := gatewayclientfake.NewSimpleClientset()
		_, _ = client.GatewayV1().Gateways(testNamespace).Create(ctx, newGateway(), metav1.CreateOptions{})
		route := validBaselineRoute()
		route.Spec.Rules[0].Filters[0].RequestHeaderModifier.Set = []gatewayv1.HTTPHeader{
			{Name: "baggage", Value: "spoofed"},
		}
		_, _ = client.GatewayV1().HTTPRoutes(testNamespace).Create(ctx, route, metav1.CreateOptions{})
		p := NewWithGatewayClass(client, "test-install", func(context.Context) error { return nil }, "linkerd")
		err := p.ValidateBaseline(ctx, baseline, nil)
		if err == nil || err.Error() != "baseline ingress must not set baggage" {
			t.Fatalf("expected set baggage error, got: %v", err)
		}
	})

	t.Run("wildcard route conflicts with preview domain", func(t *testing.T) {
		client := gatewayclientfake.NewSimpleClientset()
		_, _ = client.GatewayV1().Gateways(testNamespace).Create(ctx, newGateway(), metav1.CreateOptions{})
		route := validBaselineRoute()
		route.Spec.Hostnames = []gatewayv1.Hostname{"*.envy.localhost"}
		_, _ = client.GatewayV1().HTTPRoutes(testNamespace).Create(ctx, route, metav1.CreateOptions{})
		p := NewWithGatewayClass(client, "test-install", func(context.Context) error { return nil }, "linkerd")
		err := p.ValidateBaseline(ctx, baseline, nil)
		if err == nil || !strings.Contains(err.Error(), "ingress host cmp-catalog-validation.envy.localhost is already claimed") {
			t.Fatalf("expected conflict error for wildcard route, got: %v", err)
		}
	})

	t.Run("multiple rules rejected", func(t *testing.T) {
		client := gatewayclientfake.NewSimpleClientset()
		_, _ = client.GatewayV1().Gateways(testNamespace).Create(ctx, newGateway(), metav1.CreateOptions{})
		route := validBaselineRoute()
		route.Spec.Rules = append(route.Spec.Rules, route.Spec.Rules[0])
		_, _ = client.GatewayV1().HTTPRoutes(testNamespace).Create(ctx, route, metav1.CreateOptions{})
		p := NewWithGatewayClass(client, "test-install", func(context.Context) error { return nil }, "linkerd")
		err := p.ValidateBaseline(ctx, baseline, nil)
		if err == nil || err.Error() != "baseline ingress must have one exact-host unconditional HTTP route" {
			t.Fatalf("expected multiple rules error, got: %v", err)
		}
	})

	t.Run("conditional match rejected", func(t *testing.T) {
		client := gatewayclientfake.NewSimpleClientset()
		_, _ = client.GatewayV1().Gateways(testNamespace).Create(ctx, newGateway(), metav1.CreateOptions{})
		route := validBaselineRoute()
		route.Spec.Rules[0].Matches = []gatewayv1.HTTPRouteMatch{
			{
				Headers: []gatewayv1.HTTPHeaderMatch{
					{Name: "x-foo", Value: "bar"},
				},
			},
		}
		_, _ = client.GatewayV1().HTTPRoutes(testNamespace).Create(ctx, route, metav1.CreateOptions{})
		p := NewWithGatewayClass(client, "test-install", func(context.Context) error { return nil }, "linkerd")
		err := p.ValidateBaseline(ctx, baseline, nil)
		if err == nil || err.Error() != "baseline ingress must have one exact-host unconditional HTTP route" {
			t.Fatalf("expected conditional match error, got: %v", err)
		}
	})

	t.Run("wrong backend target", func(t *testing.T) {
		client := gatewayclientfake.NewSimpleClientset()
		_, _ = client.GatewayV1().Gateways(testNamespace).Create(ctx, newGateway(), metav1.CreateOptions{})
		route := validBaselineRoute()
		wrongPort := gatewayv1.PortNumber(9999)
		route.Spec.Rules[0].BackendRefs[0].Port = &wrongPort
		_, _ = client.GatewayV1().HTTPRoutes(testNamespace).Create(ctx, route, metav1.CreateOptions{})
		p := NewWithGatewayClass(client, "test-install", func(context.Context) error { return nil }, "linkerd")
		err := p.ValidateBaseline(ctx, baseline, nil)
		if err == nil || err.Error() != "baseline ingress must remove baggage and route directly to the entry binding" {
			t.Fatalf("expected wrong backend target error, got: %v", err)
		}
	})

	t.Run("mismatched gateway class", func(t *testing.T) {
		client := gatewayclientfake.NewSimpleClientset()
		_, _ = client.GatewayV1().Gateways(testNamespace).Create(ctx, newGateway(), metav1.CreateOptions{})
		_, _ = client.GatewayV1().HTTPRoutes(testNamespace).Create(ctx, validBaselineRoute(), metav1.CreateOptions{})
		pMismatch := NewWithGatewayClass(client, "test-install", func(context.Context) error { return nil }, "cilium")
		if err := pMismatch.ValidateBaseline(ctx, baseline, nil); err == nil {
			t.Fatal("expected error on gateway class mismatch")
		}
	})
}

func TestGatewayAPIRejectConflictingAggregateOwnership(t *testing.T) {
	ctx := context.Background()
	otherAggregate := &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testAggregateName,
			Namespace: testNamespace,
			Labels: map[string]string{
				installationLabel: "other-installation",
				roleLabel:         "aggregate",
			},
			Annotations: map[string]string{
				ownershipAnnotation: "aggregate:other-installation",
			},
		},
	}
	client := gatewayclientfake.NewSimpleClientset()
	if _, err := client.GatewayV1().HTTPRoutes(testNamespace).Create(ctx, otherAggregate, metav1.CreateOptions{}); err != nil {
		t.Fatalf("failed to create other aggregate route: %v", err)
	}

	p := New(client, "test-install", func(context.Context) error { return nil })
	a := testEntry("a")
	_, err := p.Reconcile(ctx, domain.RouteSnapshot{
		MeshEntries: []domain.RouteEntry{a},
	})
	if err == nil {
		t.Fatal("expected ownership conflict error, got nil")
	}
}

func TestGatewayAPIRejectConflictingIngressHost(t *testing.T) {
	ctx := context.Background()
	client := gatewayclientfake.NewSimpleClientset()

	// Existing route claiming cmp-a.envy.localhost
	existingRoute := &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "existing-route",
			Namespace: testNamespace,
		},
		Spec: gatewayv1.HTTPRouteSpec{
			Hostnames: []gatewayv1.Hostname{"cmp-a.envy.localhost"},
		},
	}
	if _, err := client.GatewayV1().HTTPRoutes(testNamespace).Create(ctx, existingRoute, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}

	p := New(client, "test-install", func(context.Context) error { return nil })
	a := testEntry("a")
	_, err := p.Reconcile(ctx, domain.RouteSnapshot{
		IngressEntries: []domain.RouteEntry{a},
	})
	if err == nil {
		t.Fatal("expected conflict error when ingress host is already claimed")
	}
}

func TestGatewayAPIRejectConflictingMeshParentRef(t *testing.T) {
	ctx := context.Background()
	client := gatewayclientfake.NewSimpleClientset()

	svcGroup := gatewayv1.Group("")
	svcKind := gatewayv1.Kind("Service")
	svcNs := gatewayv1.Namespace(testNamespace)
	svcPort := gatewayv1.PortNumber(8080)

	// Existing route attaching to service-b as parentRef
	existingMeshRoute := &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "other-svc-mesh",
			Namespace: testNamespace,
			Labels: map[string]string{
				installationLabel: "other-install",
			},
		},
		Spec: gatewayv1.HTTPRouteSpec{
			CommonRouteSpec: gatewayv1.CommonRouteSpec{
				ParentRefs: []gatewayv1.ParentReference{
					{
						Group:     &svcGroup,
						Kind:      &svcKind,
						Name:      gatewayv1.ObjectName("service-b"),
						Namespace: &svcNs,
						Port:      &svcPort,
					},
				},
			},
		},
	}
	if _, err := client.GatewayV1().HTTPRoutes(testNamespace).Create(ctx, existingMeshRoute, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}

	p := New(client, "test-install", func(context.Context) error { return nil })
	a := testEntry("a")
	_, err := p.Reconcile(ctx, domain.RouteSnapshot{
		MeshEntries: []domain.RouteEntry{a},
	})
	if err == nil {
		t.Fatal("expected conflict error when mesh parentRef is already claimed")
	}
}

