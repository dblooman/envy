package gatewayapi

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ktesting "k8s.io/client-go/testing"

	"github.com/dblooman/envy/internal/domain"
	"github.com/dblooman/envy/internal/mesh"
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

func TestGatewayAPISnapshotsAndReferenceGrants(t *testing.T) {
	ctx := context.Background()
	client := gatewayclientfake.NewSimpleClientset()
	p := New(client, "test-install", func(context.Context) error { return nil })
	snapshot := domain.RouteSnapshot{OwnedCompositions: map[string]string{}}
	for i := range 20 {
		e := testEntry(fmt.Sprintf("c%d", i))
		snapshot.MeshEntries = append(snapshot.MeshEntries, e)
		snapshot.IngressEntries = append(snapshot.IngressEntries, e)
		snapshot.OwnedCompositions[e.CompositionID] = e.OwnershipToken
	}

	obs, err := p.Reconcile(ctx, snapshot)
	if err != nil {
		t.Fatal(err)
	}

	if obs.Ready {
		t.Fatal("API writes alone must not imply controller acceptance")
	}

	list, _ := client.GatewayV1().HTTPRoutes("").List(ctx, metav1.ListOptions{})
	if len(list.Items) != 41 {
		t.Fatalf("got %d routes", len(list.Items))
	}

	for _, r := range list.Items {
		if len(r.Spec.Rules) != 1 {
			t.Fatal("route capacity exceeded")
		}
	}

	grants, _ := client.GatewayV1beta1().ReferenceGrants("").List(ctx, metav1.ListOptions{})
	if len(grants.Items) != 20 {
		t.Fatal("missing cross namespace grants")
	}

	for _, g := range grants.Items {
		if len(g.Spec.To) != 1 || g.Spec.To[0].Name == nil || *g.Spec.To[0].Name != "service-b" {
			t.Fatal("grant must be restricted to destination Service")
		}
	}

	client.ClearActions()
	if _, err = p.Reconcile(ctx, snapshot); err != nil {
		t.Fatal(err)
	}

	for _, a := range client.Actions() {
		if a.GetVerb() == "update" || a.GetVerb() == "create" || a.GetVerb() == "delete" {
			t.Fatal("unchanged snapshot mutated resources")
		}
	}

	snapshot.MeshEntries = nil
	snapshot.IngressEntries = nil
	if _, err = p.Reconcile(ctx, snapshot); err != nil {
		t.Fatal(err)
	}

	list, _ = client.GatewayV1().HTTPRoutes("").List(ctx, metav1.ListOptions{})
	grants, _ = client.GatewayV1beta1().ReferenceGrants("").List(ctx, metav1.ListOptions{})
	if len(list.Items) != 0 || len(grants.Items) != 0 {
		t.Fatal("retired routes or grants remain")
	}
}

func TestGatewayAPISelectorRouteUsesExactMatchAndNormalizesIngress(t *testing.T) {
	ctx := context.Background()
	e := testEntry("selected")
	e.Host = "baseline.envy.localhost"
	e.SelectorHeader = "X-Envy-Preview"

	for _, profileName := range []string{"cilium", "linkerd"} {
		t.Run(profileName, func(t *testing.T) {
			client := gatewayclientfake.NewSimpleClientset()
			profile, err := mesh.Resolve(profileName)
			if err != nil {
				t.Fatal(err)
			}
			p := NewProfile(client, "test-install", func(context.Context) error { return nil }, profile.GatewayClass, profile)

			if _, err := p.Reconcile(ctx, domain.RouteSnapshot{
				SelectorEntries:   []domain.RouteEntry{e},
				OwnedCompositions: map[string]string{e.CompositionID: e.OwnershipToken},
			}); err != nil {
				t.Fatal(err)
			}

			route, err := client.GatewayV1().HTTPRoutes(testNamespace).Get(ctx, selectorName(e), metav1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if got := route.Spec.Rules[0].Matches[0].Headers; len(got) != 1 || got[0].Name != gatewayv1.HTTPHeaderName(e.SelectorHeader) || got[0].Type == nil || *got[0].Type != gatewayv1.HeaderMatchExact || got[0].Value != e.CompositionID {
				t.Fatalf("selector route must use one Core Exact header match: %+v", got)
			}

			request := route.Spec.Rules[0].Filters[0].RequestHeaderModifier
			if len(request.Remove) != 2 || request.Remove[0] != "baggage" || request.Remove[1] != e.SelectorHeader {
				t.Fatalf("selector route must remove supplied baggage and selector: %+v", request.Remove)
			}
			if len(request.Set) != 1 || request.Set[0].Name != "baggage" || request.Set[0].Value != "composition=selected,envy_message_isolation=false" {
				t.Fatalf("selector route must set canonical baggage: %+v", request.Set)
			}

			response := route.Spec.Rules[0].Filters[1].ResponseHeaderModifier
			if len(response.Set) != 1 || response.Set[0].Name != domain.PreviewRouteHeader || response.Set[0].Value != e.CompositionID {
				t.Fatalf("selector route must mark selected response: %+v", response.Set)
			}
		})
	}
}

func TestGatewayAPIHostnameIngressStripsSelectorAndBaggage(t *testing.T) {
	ctx := context.Background()
	client := gatewayclientfake.NewSimpleClientset()
	p := New(client, "test-install", func(context.Context) error { return nil })
	e := testEntry("hostname")
	e.SelectorHeader = "X-Envy-Preview"

	if _, err := p.Reconcile(ctx, domain.RouteSnapshot{
		IngressEntries:    []domain.RouteEntry{e},
		OwnedCompositions: map[string]string{e.CompositionID: e.OwnershipToken},
	}); err != nil {
		t.Fatal(err)
	}

	route, err := client.GatewayV1().HTTPRoutes(testNamespace).Get(ctx, ingressName(e), metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	request := route.Spec.Rules[0].Filters[0].RequestHeaderModifier
	if len(request.Remove) != 2 || request.Remove[0] != "baggage" || request.Remove[1] != e.SelectorHeader {
		t.Fatalf("hostname ingress must remove supplied baggage and selector: %+v", request.Remove)
	}
}

func TestGatewayAPISelectorOwnershipConflictAndCleanup(t *testing.T) {
	ctx := context.Background()
	e := testEntry("selector")
	e.Host = "baseline.envy.localhost"
	e.SelectorHeader = "X-Envy-Preview"
	snapshot := domain.RouteSnapshot{
		SelectorEntries:   []domain.RouteEntry{e},
		OwnedCompositions: map[string]string{e.CompositionID: e.OwnershipToken},
	}

	t.Run("conflict", func(t *testing.T) {
		foreign := &gatewayv1.HTTPRoute{
			ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: selectorName(e)},
			Spec:       gatewayv1.HTTPRouteSpec{CommonRouteSpec: gatewayv1.CommonRouteSpec{ParentRefs: []gatewayv1.ParentReference{{Name: gatewayv1.ObjectName(e.Domain.Gateway)}}}},
		}
		client := gatewayclientfake.NewSimpleClientset(foreign)
		p := New(client, "test-install", func(context.Context) error { return nil })
		if _, err := p.Reconcile(ctx, snapshot); err == nil {
			t.Fatal("selector route adopted a foreign resource")
		}
	})

	t.Run("matching selector conflict", func(t *testing.T) {
		exact := gatewayv1.HeaderMatchExact
		conflicting := &gatewayv1.HTTPRoute{
			ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: "operator-selector"},
			Spec: gatewayv1.HTTPRouteSpec{
				CommonRouteSpec: gatewayv1.CommonRouteSpec{ParentRefs: []gatewayv1.ParentReference{{Name: gatewayv1.ObjectName(e.Domain.Gateway)}}},
				Hostnames:       []gatewayv1.Hostname{gatewayv1.Hostname(e.Host)},
				Rules:           []gatewayv1.HTTPRouteRule{{Matches: []gatewayv1.HTTPRouteMatch{{Headers: []gatewayv1.HTTPHeaderMatch{{Name: gatewayv1.HTTPHeaderName(e.SelectorHeader), Type: &exact, Value: e.CompositionID}}}}}},
			},
		}
		client := gatewayclientfake.NewSimpleClientset(conflicting)
		p := New(client, "test-install", func(context.Context) error { return nil })
		if _, err := p.Reconcile(ctx, snapshot); err == nil {
			t.Fatal("selector route accepted a conflicting exact match")
		}
	})

	t.Run("cleanup", func(t *testing.T) {
		client := gatewayclientfake.NewSimpleClientset()
		p := New(client, "test-install", func(context.Context) error { return nil })
		if _, err := p.Reconcile(ctx, snapshot); err != nil {
			t.Fatal(err)
		}
		snapshot.SelectorEntries = nil
		if _, err := p.Reconcile(ctx, snapshot); err != nil {
			t.Fatal(err)
		}
		if _, err := client.GatewayV1().HTTPRoutes(testNamespace).Get(ctx, selectorName(e), metav1.GetOptions{}); !apierrors.IsNotFound(err) {
			t.Fatalf("retired selector route remains: %v", err)
		}
	})
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

func TestLinkerdAcceptsFreshImmutableProducerRoutesWithoutObservedGeneration(t *testing.T) {
	ctx := context.Background()
	client := gatewayclientfake.NewSimpleClientset()
	profile, err := mesh.Resolve("linkerd")
	if err != nil {
		t.Fatal(err)
	}

	p := NewProfile(client, "test-install", func(context.Context) error { return nil }, "eg", profile)
	e := testEntry("a")
	s := domain.RouteSnapshot{MeshEntries: []domain.RouteEntry{e}, OwnedCompositions: map[string]string{"a": "token-a"}}
	if observation, err := p.Reconcile(ctx, s); err != nil || observation.Ready {
		t.Fatalf("initial reconcile = %#v, %v", observation, err)
	}

	routes, err := client.GatewayV1().HTTPRoutes("").List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}

	for i := range routes.Items {
		r := &routes.Items[i]
		// The fake API does not assign generation; a real Kubernetes create does.
		if r.Generation == 0 {
			r.Generation = 1
			if _, err := client.GatewayV1().HTTPRoutes(r.Namespace).Update(ctx, r, metav1.UpdateOptions{}); err != nil {
				t.Fatal(err)
			}
		}

		controller := profile.MeshController
		generation := int64(0)
		if r.Labels[roleLabel] == "ingress" {
			controller, generation = profile.GatewayController, r.Generation
		} else if !strings.Contains(r.Name, "-") || r.Generation != 1 {
			t.Fatalf("Linkerd producer route is not immutable: %s generation %d", r.Name, r.Generation)
		}

		r.Status.Parents = []gatewayv1.RouteParentStatus{{ParentRef: r.Spec.ParentRefs[0], ControllerName: gatewayv1.GatewayController(controller), Conditions: readyConditions(generation, "Accepted", "ResolvedRefs")}}
		if _, err := client.GatewayV1().HTTPRoutes(r.Namespace).UpdateStatus(ctx, r, metav1.UpdateOptions{}); err != nil {
			t.Fatal(err)
		}
	}

	if observation, err := p.Reconcile(ctx, s); err != nil || !observation.Ready {
		t.Fatalf("Linkerd conditions on fresh immutable routes should be accepted: %#v, %v", observation, err)
	}

	// A generation-bearing stale condition is not accepted by the Linkerd exception.
	routes, _ = client.GatewayV1().HTTPRoutes("").List(ctx, metav1.ListOptions{})
	for i := range routes.Items {
		if routes.Items[i].Labels[roleLabel] == "mesh" {
			routes.Items[i].Status.Parents[0].Conditions[0].ObservedGeneration = 2
			_, _ = client.GatewayV1().HTTPRoutes(routes.Items[i].Namespace).UpdateStatus(ctx, &routes.Items[i], metav1.UpdateOptions{})
			break
		}
	}

	if observation, err := p.Reconcile(ctx, s); err != nil || observation.Ready {
		t.Fatalf("contradictory Linkerd generation must remain pending: %#v, %v", observation, err)
	}

	// A changed producer spec receives a new object identity instead of an update.
	before := ""
	routes, _ = client.GatewayV1().HTTPRoutes("").List(ctx, metav1.ListOptions{})
	for _, r := range routes.Items {
		if r.Labels[roleLabel] == "mesh" {
			before = r.Name
		}
	}

	s.MeshEntries[0].Port = 8081
	if observation, err := p.Reconcile(ctx, s); err != nil || observation.Ready {
		t.Fatalf("replacement waits for the new Linkerd route: %#v, %v", observation, err)
	}

	routes, _ = client.GatewayV1().HTTPRoutes("").List(ctx, metav1.ListOptions{})
	meshCount := 0
	for _, r := range routes.Items {
		if r.Labels[roleLabel] == "mesh" {
			meshCount++
			if r.Name == before {
				t.Fatal("Linkerd producer route was updated in place")
			}
		}
	}

	if meshCount != 1 {
		t.Fatalf("expected one replacement mesh route, got %d", meshCount)
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
			Status:    gatewayv1.GatewayStatus{Conditions: readyConditions(0, "Accepted", "Programmed"), Listeners: []gatewayv1.ListenerStatus{{Name: "http", Conditions: readyConditions(0, "Accepted", "ResolvedRefs", "Programmed")}}},
			Name:      "platform-gateway",
			Namespace: "staging",
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
		r := &gatewayv1.HTTPRoute{
			Name:      "baseline-ingress",
			Namespace: "staging",
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
								Name: gatewayv1.ObjectName("service-b"),
								Port: &port8080,
							},
						},
					},
				},
			},
		}
		r.Status.Parents = []gatewayv1.RouteParentStatus{{ParentRef: r.Spec.ParentRefs[0], ControllerName: "io.cilium/gateway-controller", Conditions: readyConditions(0, "Accepted", "ResolvedRefs")}}
		return r
	}

	t.Run("missing route", func(t *testing.T) {
		client := gatewayclientfake.NewSimpleClientset(testClass("linkerd"))
		_, _ = client.GatewayV1().Gateways(testNamespace).Create(ctx, newGateway(), metav1.CreateOptions{})
		p := NewWithGatewayClass(client, "test-install", func(context.Context) error { return nil }, "linkerd")
		err := p.ValidateBaseline(ctx, baseline, nil)
		if err == nil || err.Error() != "baseline requires exactly one existing ingress route" {
			t.Fatalf("expected missing route error, got: %v", err)
		}
	})

	t.Run("valid route with baggage removal", func(t *testing.T) {
		client := gatewayclientfake.NewSimpleClientset(testClass("linkerd"))
		_, _ = client.GatewayV1().Gateways(testNamespace).Create(ctx, newGateway(), metav1.CreateOptions{})
		_, _ = client.GatewayV1().HTTPRoutes(testNamespace).Create(ctx, validBaselineRoute(), metav1.CreateOptions{})
		p := NewWithGatewayClass(client, "test-install", func(context.Context) error { return nil }, "linkerd")
		if err := p.ValidateBaseline(ctx, baseline, nil); err != nil {
			t.Fatalf("expected valid baseline route to pass, got: %v", err)
		}
	})

	t.Run("missing baggage removal filter", func(t *testing.T) {
		client := gatewayclientfake.NewSimpleClientset(testClass("linkerd"))
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
		client := gatewayclientfake.NewSimpleClientset(testClass("linkerd"))
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
		client := gatewayclientfake.NewSimpleClientset(testClass("linkerd"))
		_, _ = client.GatewayV1().Gateways(testNamespace).Create(ctx, newGateway(), metav1.CreateOptions{})
		route := validBaselineRoute()
		route.Spec.Hostnames = []gatewayv1.Hostname{"*.envy.localhost"}
		_, _ = client.GatewayV1().HTTPRoutes(testNamespace).Create(ctx, route, metav1.CreateOptions{})
		p := NewWithGatewayClass(client, "test-install", func(context.Context) error { return nil }, "linkerd")
		err := p.ValidateBaseline(ctx, baseline, nil)
		if err == nil || !strings.Contains(err.Error(), "ingress host cmp-catalog-validation.envy.localhost already claimed") {
			t.Fatalf("expected conflict error for wildcard route, got: %v", err)
		}
	})

	t.Run("multiple rules rejected", func(t *testing.T) {
		client := gatewayclientfake.NewSimpleClientset(testClass("linkerd"))
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
		client := gatewayclientfake.NewSimpleClientset(testClass("linkerd"))
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
		client := gatewayclientfake.NewSimpleClientset(testClass("linkerd"))
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
		client := gatewayclientfake.NewSimpleClientset(testClass("linkerd"))
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
		Name:      testAggregateName,
		Namespace: testNamespace,
		Labels: map[string]string{
			installationLabel: "other-installation",
			roleLabel:         "aggregate",
		},
		Annotations: map[string]string{
			ownershipAnnotation: "aggregate:other-installation",
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
		Name:      "existing-route",
		Namespace: testNamespace,
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
		Name:      "other-svc-mesh",
		Namespace: testNamespace,
		Labels: map[string]string{
			installationLabel: "other-install",
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

func readyConditions(generation int64, names ...string) []metav1.Condition {
	out := []metav1.Condition{}
	for _, name := range names {
		out = append(out, metav1.Condition{Type: name, Status: metav1.ConditionTrue, ObservedGeneration: generation, Reason: "Accepted"})
	}

	return out
}

func testClass(name string) *gatewayv1.GatewayClass {
	return &gatewayv1.GatewayClass{Name: name, Spec: gatewayv1.GatewayClassSpec{ControllerName: "io.cilium/gateway-controller"}, Status: gatewayv1.GatewayClassStatus{Conditions: readyConditions(0, "Accepted")}}
}

func TestRouteRequiresCurrentControllerAndGeneration(t *testing.T) {
	r := &gatewayv1.HTTPRoute{Generation: 2, Spec: gatewayv1.HTTPRouteSpec{CommonRouteSpec: gatewayv1.CommonRouteSpec{ParentRefs: []gatewayv1.ParentReference{{Name: "svc"}}}}}
	r.Status.Parents = []gatewayv1.RouteParentStatus{{ParentRef: r.Spec.ParentRefs[0], ControllerName: "controller", Conditions: readyConditions(1, "Accepted", "ResolvedRefs")}}
	if routePending(r, "controller") == "" {
		t.Fatal("stale generation accepted")
	}

	r.Status.Parents[0].Conditions = readyConditions(2, "Accepted", "ResolvedRefs")
	if routePending(r, "other") == "" {
		t.Fatal("wrong controller accepted")
	}

	if msg := routePending(r, "controller"); msg != "" {
		t.Fatal(msg)
	}
}

func TestIngressOnlyGrantAndWriteOrdering(t *testing.T) {
	ctx := context.Background()
	client := gatewayclientfake.NewSimpleClientset()
	p := New(client, "test-install", func(context.Context) error { return nil })
	e := testEntry("entry")
	s := domain.RouteSnapshot{IngressEntries: []domain.RouteEntry{e}, OwnedCompositions: map[string]string{"entry": e.OwnershipToken}}
	if _, err := p.Reconcile(ctx, s); err != nil {
		t.Fatal(err)
	}

	firstWrite := ""
	for _, a := range client.Actions() {
		if a.GetVerb() == "create" {
			firstWrite = a.GetResource().Resource
			break
		}
	}

	if firstWrite != "referencegrants" {
		t.Fatalf("first write %s must authorize cross-namespace ingress", firstWrite)
	}

	grants, _ := client.GatewayV1beta1().ReferenceGrants("envy-entry").List(ctx, metav1.ListOptions{})
	if len(grants.Items) != 1 {
		t.Fatal("ingress-only override lacks grant")
	}
}

func TestOwnershipConflictsPrecedeAllWrites(t *testing.T) {
	ctx := context.Background()
	e := testEntry("a")
	foreign := &gatewayv1.HTTPRoute{Name: "envy-ingress-a", Namespace: "staging"}
	client := gatewayclientfake.NewSimpleClientset(foreign)
	p := New(client, "test-install", func(context.Context) error { return nil })
	if _, err := p.Reconcile(ctx, domain.RouteSnapshot{IngressEntries: []domain.RouteEntry{e}, OwnedCompositions: map[string]string{"a": e.OwnershipToken}}); err == nil {
		t.Fatal("adopted foreign route")
	}

	for _, a := range client.Actions() {
		if a.GetVerb() == "create" || a.GetVerb() == "update" || a.GetVerb() == "delete" {
			t.Fatal("mutated before ownership validation")
		}
	}
}

func TestPartialWriteFailureRetriesSafely(t *testing.T) {
	ctx := context.Background()
	e := testEntry("a")
	client := gatewayclientfake.NewSimpleClientset()
	p := New(client, "test-install", func(context.Context) error { return nil })
	fail := true
	client.PrependReactor("create", "httproutes", func(action ktesting.Action) (bool, runtime.Object, error) {
		if fail {
			fail = false
			return true, nil, errors.New("temporary API failure")
		}

		return false, nil, nil
	})
	s := domain.RouteSnapshot{MeshEntries: []domain.RouteEntry{e}, IngressEntries: []domain.RouteEntry{e}, OwnedCompositions: map[string]string{"a": e.OwnershipToken}}
	if _, err := p.Reconcile(ctx, s); err == nil {
		t.Fatal("failure hidden")
	}

	if _, err := p.Reconcile(ctx, s); err != nil {
		t.Fatal(err)
	}

	routes, _ := client.GatewayV1().HTTPRoutes("staging").List(ctx, metav1.ListOptions{})
	if len(routes.Items) != 3 {
		t.Fatal("retry did not complete desired state")
	}
}

func TestOtherGatewayDoesNotClaimPreviewHost(t *testing.T) {
	ctx := context.Background()
	e := testEntry("a")
	other := &gatewayv1.HTTPRoute{Name: "other", Namespace: "staging", Spec: gatewayv1.HTTPRouteSpec{CommonRouteSpec: gatewayv1.CommonRouteSpec{ParentRefs: []gatewayv1.ParentReference{{Name: "different-gateway"}}}, Hostnames: []gatewayv1.Hostname{gatewayv1.Hostname(e.Host)}}}
	client := gatewayclientfake.NewSimpleClientset(other)
	p := New(client, "test-install", nil)
	if err := p.Validate(ctx, domain.RouteSnapshot{IngressEntries: []domain.RouteEntry{e}}); err != nil {
		t.Fatal(err)
	}
}

func TestRetirementWaitsForDeletionAndPropagatesFailures(t *testing.T) {
	for _, resource := range []string{"httproutes", "referencegrants"} {
		t.Run(resource, func(t *testing.T) {
			ctx := context.Background()
			client := gatewayclientfake.NewSimpleClientset()
			p := New(client, "test", func(context.Context) error { return nil })
			e := testEntry("retire")
			s := domain.RouteSnapshot{MeshEntries: []domain.RouteEntry{e}, IngressEntries: []domain.RouteEntry{e}, OwnedCompositions: map[string]string{e.CompositionID: e.OwnershipToken}}
			if _, err := p.Reconcile(ctx, s); err != nil {
				t.Fatal(err)
			}

			s.MeshEntries = nil
			s.IngressEntries = nil
			fail := true
			hold := true
			client.PrependReactor("delete", resource, func(ktesting.Action) (bool, runtime.Object, error) {
				if fail {
					return true, nil, errors.New("delete denied")
				}

				return hold, nil, nil
			})
			if _, err := p.Reconcile(ctx, s); err == nil {
				t.Fatal("cleanup failure was swallowed")
			}

			fail = false
			obs, err := p.Reconcile(ctx, s)
			if err != nil || obs.Ready || !strings.Contains(obs.Message, "deletion") {
				t.Fatalf("pending deletion cached as complete: %+v %v", obs, err)
			}

			grants, _ := client.GatewayV1beta1().ReferenceGrants("").List(ctx, metav1.ListOptions{})
			if len(grants.Items) != 1 {
				t.Fatal("grant removed before dependent routes retired")
			}

			hold = false
			obs, err = p.Reconcile(ctx, s)
			if err != nil || !obs.Ready {
				t.Fatalf("cleanup did not recover: %+v %v", obs, err)
			}
		})
	}
}

func TestLinkerdCoreParentStillRequiresGenerationEvidence(t *testing.T) {
	r := &gatewayv1.HTTPRoute{Namespace: "baseline", Generation: 1, Spec: gatewayv1.HTTPRouteSpec{CommonRouteSpec: gatewayv1.CommonRouteSpec{ParentRefs: []gatewayv1.ParentReference{{Group: ptr(gatewayv1.Group("")), Kind: ptr(gatewayv1.Kind("Service")), Name: "api"}}}}}
	statusParent := r.Spec.ParentRefs[0]
	statusParent.Group = ptr(gatewayv1.Group("core"))
	statusParent.Namespace = ptr(gatewayv1.Namespace("baseline"))
	r.Status.Parents = []gatewayv1.RouteParentStatus{{ControllerName: "linkerd.io/policy-controller", ParentRef: statusParent, Conditions: readyConditions(0, "Accepted", "ResolvedRefs")}}
	if msg := routePending(r, "linkerd.io/policy-controller"); !strings.Contains(msg, "observed generation 0") {
		t.Fatalf("missing generation must block Linkerd readiness: %q", msg)
	}

	r.Status.Parents[0].Conditions = readyConditions(1, "Accepted", "ResolvedRefs")
	if msg := routePending(r, "linkerd.io/policy-controller"); msg != "" {
		t.Fatalf("equivalent core parent rejected: %s", msg)
	}
}

func TestListenerConflictIgnoresOtherGatewayParents(t *testing.T) {
	other := gatewayv1.SectionName("other")
	https := gatewayv1.SectionName("https")
	route := &gatewayv1.HTTPRoute{Namespace: "baseline", Spec: gatewayv1.HTTPRouteSpec{CommonRouteSpec: gatewayv1.CommonRouteSpec{ParentRefs: []gatewayv1.ParentReference{
		{Name: "managed", SectionName: &other}, {Name: "unrelated", SectionName: &https},
	}}}}
	if overlapsSection(route, "baseline", "managed", "https") {
		t.Fatal("unrelated Gateway listener caused a conflict")
	}

	route.Spec.ParentRefs[0].SectionName = nil
	if !overlapsSection(route, "baseline", "managed", "https") {
		t.Fatal("all-listener parent must conflict")
	}
}

func TestIngressPropagatesIsolationForGatewayMeshes(t *testing.T) {
	ctx := context.Background()
	client := gatewayclientfake.NewSimpleClientset()
	p := New(client, "test", func(context.Context) error { return nil })
	e := testEntry("isolated")
	e.MessageIsolation = true
	_, err := p.Reconcile(ctx, domain.RouteSnapshot{IngressEntries: []domain.RouteEntry{e}, OwnedCompositions: map[string]string{e.CompositionID: e.OwnershipToken}})
	if err != nil {
		t.Fatal(err)
	}

	route, err := client.GatewayV1().HTTPRoutes(testNamespace).Get(ctx, "envy-ingress-isolated", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if route.Spec.Rules[0].Filters[0].RequestHeaderModifier.Set[0].Value != "composition=isolated,envy_message_isolation=true" {
		t.Fatal("missing isolation context")
	}
}
