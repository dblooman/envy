// Package gatewayapi reconciles complete shared routing snapshots into Kubernetes Gateway API resources.
package gatewayapi

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/dblooman/envy/internal/domain"
	"github.com/dblooman/envy/internal/routing"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
	gatewayclient "sigs.k8s.io/gateway-api/pkg/client/clientset/versioned"
)

const installationLabel = "envy.dev/installation"
const compositionLabel = "envy.dev/composition"
const ownershipAnnotation = "envy.dev/ownership-token"
const roleLabel = "envy.dev/route-role"

type Provider struct {
	client       gatewayclient.Interface
	installation string
	guard        func(context.Context) error
	gatewayClass string
}

func New(client gatewayclient.Interface, installation string, guard func(context.Context) error) *Provider {
	return NewWithGatewayClass(client, installation, guard, "")
}

func NewWithGatewayClass(client gatewayclient.Interface, installation string, guard func(context.Context) error, gatewayClass string) *Provider {
	return &Provider{
		client:       client,
		installation: installation,
		guard:        guard,
		gatewayClass: gatewayClass,
	}
}

func (p *Provider) writable(ctx context.Context) error {
	if p.guard == nil {
		return fmt.Errorf("provider mutation requires leadership guard")
	}
	return p.guard(ctx)
}

func (p *Provider) owned(m metav1.Object, token string) bool {
	return token != "" && m.GetLabels()[installationLabel] == p.installation && m.GetAnnotations()[ownershipAnnotation] == token
}

func aggregateToken(installation string) string {
	return "aggregate:" + installation
}

func hostOverlap(a, b string) bool {
	if a == b || a == "*" || b == "*" {
		return true
	}
	if strings.HasPrefix(a, "*.") && strings.HasSuffix(b, a[1:]) {
		return true
	}
	return strings.HasPrefix(b, "*.") && strings.HasSuffix(a, b[1:])
}

func domains(snapshot domain.RouteSnapshot) []domain.RouteDomain {
	byHost := map[string]domain.RouteDomain{}
	for _, d := range snapshot.Domains {
		byHost[d.ServiceHost] = d
	}
	for _, e := range snapshot.MeshEntries {
		byHost[e.Domain.ServiceHost] = e.Domain
	}
	out := make([]domain.RouteDomain, 0, len(byHost))
	for _, d := range byHost {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ServiceHost < out[j].ServiceHost })
	return out
}

// parseServiceHost parses a host such as "service-b.staging.svc.cluster.local" or "service-b" into name and namespace.
func parseServiceHost(host, fallbackNamespace string) (name, namespace string) {
	parts := strings.Split(host, ".")
	if len(parts) >= 2 {
		return parts[0], parts[1]
	}
	return parts[0], fallbackNamespace
}

func (p *Provider) Validate(ctx context.Context, snapshot domain.RouteSnapshot) error {
	_, err := p.inspect(ctx, snapshot)
	return err
}

func (p *Provider) Reconcile(ctx context.Context, snapshot domain.RouteSnapshot) (domain.RouteObservation, error) {
	observedRoutes, err := p.inspect(ctx, snapshot)
	if err != nil {
		return domain.RouteObservation{}, err
	}
	if err := p.writable(ctx); err != nil {
		return domain.RouteObservation{}, err
	}

	// 1. Delete stale ingress HTTPRoutes.
	wantIngress := map[string]domain.RouteEntry{}
	for _, e := range snapshot.IngressEntries {
		wantIngress[e.Domain.Namespace+"/envy-ingress-"+e.CompositionID] = e
	}

	observedNames := make([]string, 0, len(observedRoutes))
	for name := range observedRoutes {
		observedNames = append(observedNames, name)
	}
	sort.Strings(observedNames)

	for _, name := range observedNames {
		route := observedRoutes[name]
		if route.Labels[installationLabel] != p.installation || route.Labels[roleLabel] != "ingress" {
			continue
		}
		if _, ok := wantIngress[route.Namespace+"/"+route.Name]; ok {
			continue
		}
		id := route.Labels[compositionLabel]
		token := snapshot.OwnedCompositions[id]
		if id == "" || route.Name != "envy-ingress-"+id || !p.owned(route, token) {
			return domain.RouteObservation{}, fmt.Errorf("stale ingress ownership is not established by persisted state: %s/%s", route.Namespace, route.Name)
		}
		if err = p.writable(ctx); err != nil {
			return domain.RouteObservation{}, err
		}
		uid := types.UID(route.UID)
		err = p.client.GatewayV1().HTTPRoutes(route.Namespace).Delete(ctx, route.Name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}})
		if err != nil && !apierrors.IsNotFound(err) {
			return domain.RouteObservation{}, err
		}
	}

	// 2. Reconcile Mesh aggregate HTTPRoutes (GAMMA Service parentRef).
	entries := append([]domain.RouteEntry(nil), snapshot.MeshEntries...)
	sort.Slice(entries, func(i, j int) bool { return entries[i].CompositionID < entries[j].CompositionID })

	for _, d := range domains(snapshot) {
		svcName, svcNamespace := parseServiceHost(d.ServiceHost, d.Namespace)
		svcGroup := gatewayv1.Group("")
		svcKind := gatewayv1.Kind("Service")
		parentNs := gatewayv1.Namespace(svcNamespace)
		parentPort := gatewayv1.PortNumber(d.Port)

		aggregate := &gatewayv1.HTTPRoute{
			ObjectMeta: metav1.ObjectMeta{
				Name:      d.AggregateName,
				Namespace: d.Namespace,
				Labels: map[string]string{
					installationLabel: p.installation,
					roleLabel:         "aggregate",
				},
				Annotations: map[string]string{
					ownershipAnnotation: aggregateToken(p.installation),
				},
			},
			Spec: gatewayv1.HTTPRouteSpec{
				CommonRouteSpec: gatewayv1.CommonRouteSpec{
					ParentRefs: []gatewayv1.ParentReference{
						{
							Group:     &svcGroup,
							Kind:      &svcKind,
							Name:      gatewayv1.ObjectName(svcName),
							Namespace: &parentNs,
							Port:      &parentPort,
						},
					},
				},
			},
		}

		// Composition rules: match baggage header.
		for _, e := range entries {
			if e.Domain.ServiceHost != d.ServiceHost {
				continue
			}
			destName, destNamespace := parseServiceHost(e.DestinationHost, domain.NamespaceForID(e.CompositionID))
			destNs := gatewayv1.Namespace(destNamespace)
			destPort := gatewayv1.PortNumber(e.Port)
			regMatch := gatewayv1.HeaderMatchRegularExpression
			ruleName := gatewayv1.SectionName("composition-" + e.CompositionID)

			aggregate.Spec.Rules = append(aggregate.Spec.Rules, gatewayv1.HTTPRouteRule{
				Name: &ruleName,
				Matches: []gatewayv1.HTTPRouteMatch{
					{
						Headers: []gatewayv1.HTTPHeaderMatch{
							{
								Type:  &regMatch,
								Name:  "baggage",
								Value: routing.BaggagePattern(e.CompositionID),
							},
						},
					},
				},
				BackendRefs: []gatewayv1.HTTPBackendRef{
					{
						BackendRef: gatewayv1.BackendRef{
							BackendObjectReference: gatewayv1.BackendObjectReference{
								Group:     &svcGroup,
								Kind:      &svcKind,
								Name:      gatewayv1.ObjectName(destName),
								Namespace: &destNs,
								Port:      &destPort,
							},
						},
					},
				},
			})
		}

		// Fallback baseline rule: default unconditional route.
		baselineRuleName := gatewayv1.SectionName("baseline")
		aggregate.Spec.Rules = append(aggregate.Spec.Rules, gatewayv1.HTTPRouteRule{
			Name: &baselineRuleName,
			BackendRefs: []gatewayv1.HTTPBackendRef{
				{
					BackendRef: gatewayv1.BackendRef{
						BackendObjectReference: gatewayv1.BackendObjectReference{
							Group:     &svcGroup,
							Kind:      &svcKind,
							Name:      gatewayv1.ObjectName(svcName),
							Namespace: &parentNs,
							Port:      &parentPort,
						},
					},
				},
			},
		})

		if err = p.ensureHTTPRoute(ctx, aggregate, observedRoutes[d.Namespace+"/"+d.AggregateName]); err != nil {
			return domain.RouteObservation{}, err
		}
	}

	// 3. Reconcile exact Preview Ingress HTTPRoutes (attached to Gateway).
	ingressNames := make([]string, 0, len(wantIngress))
	for name := range wantIngress {
		ingressNames = append(ingressNames, name)
	}
	sort.Strings(ingressNames)

	for _, name := range ingressNames {
		e := wantIngress[name]
		destName, destNamespace := parseServiceHost(e.DestinationHost, e.Domain.Namespace)
		svcGroup := gatewayv1.Group("")
		svcKind := gatewayv1.Kind("Service")
		destNs := gatewayv1.Namespace(destNamespace)
		destPort := gatewayv1.PortNumber(e.Port)
		gatewayGroup := gatewayv1.Group("gateway.networking.k8s.io")
		gatewayKind := gatewayv1.Kind("Gateway")
		gatewayNs := gatewayv1.Namespace(e.Domain.Namespace)

		ingressRoute := &gatewayv1.HTTPRoute{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "envy-ingress-" + e.CompositionID,
				Namespace: e.Domain.Namespace,
				Labels: map[string]string{
					installationLabel: p.installation,
					compositionLabel:  e.CompositionID,
					roleLabel:         "ingress",
				},
				Annotations: map[string]string{
					ownershipAnnotation: e.OwnershipToken,
				},
			},
			Spec: gatewayv1.HTTPRouteSpec{
				CommonRouteSpec: gatewayv1.CommonRouteSpec{
					ParentRefs: []gatewayv1.ParentReference{
						{
							Group:     &gatewayGroup,
							Kind:      &gatewayKind,
							Name:      gatewayv1.ObjectName(e.Domain.Gateway),
							Namespace: &gatewayNs,
						},
					},
				},
				Hostnames: []gatewayv1.Hostname{gatewayv1.Hostname(e.Host)},
				Rules: []gatewayv1.HTTPRouteRule{
					{
						Filters: []gatewayv1.HTTPRouteFilter{
							{
								Type: gatewayv1.HTTPRouteFilterRequestHeaderModifier,
								RequestHeaderModifier: &gatewayv1.HTTPHeaderFilter{
									Set: []gatewayv1.HTTPHeader{
										{
											Name:  "baggage",
											Value: "composition=" + e.CompositionID,
										},
									},
								},
							},
							{
								Type: gatewayv1.HTTPRouteFilterResponseHeaderModifier,
								ResponseHeaderModifier: &gatewayv1.HTTPHeaderFilter{
									Set: []gatewayv1.HTTPHeader{
										{
											Name:  gatewayv1.HTTPHeaderName(domain.PreviewRouteHeader),
											Value: e.CompositionID,
										},
									},
								},
							},
						},
						BackendRefs: []gatewayv1.HTTPBackendRef{
							{
								BackendRef: gatewayv1.BackendRef{
									BackendObjectReference: gatewayv1.BackendObjectReference{
										Group:     &svcGroup,
										Kind:      &svcKind,
										Name:      gatewayv1.ObjectName(destName),
										Namespace: &destNs,
										Port:      &destPort,
									},
								},
							},
						},
					},
				},
			},
		}

		if err = p.ensureHTTPRoute(ctx, ingressRoute, observedRoutes[name]); err != nil {
			return domain.RouteObservation{}, err
		}
	}

	// 4. Ensure ReferenceGrants in target preview namespaces for cross-namespace routing.
	activeCompositions := map[string]string{}
	for _, e := range snapshot.MeshEntries {
		activeCompositions[e.CompositionID] = e.Domain.Namespace
	}
	for id, baselineNs := range activeCompositions {
		targetNs := domain.NamespaceForID(id)
		token := snapshot.OwnedCompositions[id]
		if err := p.ensureReferenceGrant(ctx, targetNs, baselineNs, id, token); err != nil {
			return domain.RouteObservation{}, err
		}
	}
	// Clean up ReferenceGrants for retired owned compositions.
	for id, token := range snapshot.OwnedCompositions {
		if _, active := activeCompositions[id]; !active {
			targetNs := domain.NamespaceForID(id)
			if err := p.cleanReferenceGrant(ctx, targetNs, id, token); err != nil {
				return domain.RouteObservation{}, err
			}
		}
	}

	return domain.RouteObservation{Ready: true, Message: "gateway-api routing resources accepted; proxy convergence requires verification"}, nil
}

func (p *Provider) cleanReferenceGrant(ctx context.Context, targetNamespace, compositionID, token string) error {
	api := p.client.GatewayV1beta1().ReferenceGrants(targetNamespace)
	got, err := api.Get(ctx, "envy-allow-mesh-routing", metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !p.owned(got, token) {
		return fmt.Errorf("reference-grant ownership conflict: %s/%s", got.Namespace, got.Name)
	}
	if err := p.writable(ctx); err != nil {
		return err
	}
	err = api.Delete(ctx, "envy-allow-mesh-routing", metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}

func (p *Provider) ensureReferenceGrant(ctx context.Context, targetNamespace, fromNamespace, compositionID, token string) error {
	want := &gatewayv1beta1.ReferenceGrant{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "envy-allow-mesh-routing",
			Namespace: targetNamespace,
			Labels: map[string]string{
				installationLabel: p.installation,
				compositionLabel:  compositionID,
			},
			Annotations: map[string]string{
				ownershipAnnotation: token,
			},
		},
		Spec: gatewayv1beta1.ReferenceGrantSpec{
			From: []gatewayv1beta1.ReferenceGrantFrom{
				{
					Group:     gatewayv1.GroupName,
					Kind:      "HTTPRoute",
					Namespace: gatewayv1.Namespace(fromNamespace),
				},
			},
			To: []gatewayv1beta1.ReferenceGrantTo{
				{
					Group: "",
					Kind:  "Service",
				},
			},
		},
	}

	api := p.client.GatewayV1beta1().ReferenceGrants(targetNamespace)
	got, err := api.Get(ctx, want.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		if err := p.writable(ctx); err != nil {
			return err
		}
		_, err = api.Create(ctx, want, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	if !p.owned(got, token) {
		return fmt.Errorf("reference-grant ownership conflict: %s/%s", got.Namespace, got.Name)
	}
	if !reflect.DeepEqual(got.Spec, want.Spec) {
		got.Spec = want.Spec
		if err := p.writable(ctx); err != nil {
			return err
		}
		_, err = api.Update(ctx, got, metav1.UpdateOptions{})
		return err
	}
	return nil
}

func (p *Provider) ensureHTTPRoute(ctx context.Context, want, got *gatewayv1.HTTPRoute) error {
	api := p.client.GatewayV1().HTTPRoutes(want.Namespace)
	if got == nil {
		if err := p.writable(ctx); err != nil {
			return err
		}
		_, err := api.Create(ctx, want, metav1.CreateOptions{})
		return err
	}
	if !p.owned(got, want.Annotations[ownershipAnnotation]) {
		return fmt.Errorf("routing object ownership conflict: %s/%s", want.Namespace, want.Name)
	}
	if !reflect.DeepEqual(got.Spec, want.Spec) {
		got.Spec = want.Spec
		if err := p.writable(ctx); err != nil {
			return err
		}
		_, err := api.Update(ctx, got, metav1.UpdateOptions{})
		return err
	}
	return nil
}

func (p *Provider) inspect(ctx context.Context, snapshot domain.RouteSnapshot) (map[string]*gatewayv1.HTTPRoute, error) {
	list, err := p.client.GatewayV1().HTTPRoutes("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	for _, d := range domains(snapshot) {
		if d.Namespace == "" || d.ServiceHost == "" || d.AggregateName == "" || d.Port < 1 {
			return nil, fmt.Errorf("invalid routing domain")
		}
		svcName, _ := parseServiceHost(d.ServiceHost, d.Namespace)
		for _, r := range list.Items {
			if r.Namespace == d.Namespace && r.Name == d.AggregateName {
				if !p.owned(&r, aggregateToken(p.installation)) {
					return nil, fmt.Errorf("aggregate routing ownership conflict: %s/%s", r.Namespace, r.Name)
				}
				continue
			}
			for _, pRef := range r.Spec.ParentRefs {
				pKind := "Gateway"
				if pRef.Kind != nil {
					pKind = string(*pRef.Kind)
				}
				pNs := r.Namespace
				if pRef.Namespace != nil {
					pNs = string(*pRef.Namespace)
				}
				if pKind == "Service" && pNs == d.Namespace && string(pRef.Name) == svcName && r.Labels[installationLabel] != p.installation {
					return nil, fmt.Errorf("baseline mesh service is already owned by HTTPRoute %s/%s", r.Namespace, r.Name)
				}
			}
		}
	}

	for _, e := range snapshot.IngressEntries {
		for _, r := range list.Items {
			if r.Labels[compositionLabel] == e.CompositionID && p.owned(&r, e.OwnershipToken) {
				continue
			}
			hosts := r.Spec.Hostnames
			if len(hosts) == 0 && attachesToGateway(&r, e.Domain.Namespace, e.Domain.Gateway) {
				return nil, fmt.Errorf("ingress host %s is already claimed by %s/%s", e.Host, r.Namespace, r.Name)
			}
			for _, h := range hosts {
				if hostOverlap(string(h), e.Host) {
					return nil, fmt.Errorf("ingress host %s is already claimed by %s/%s", e.Host, r.Namespace, r.Name)
				}
			}
		}
	}

	observed := make(map[string]*gatewayv1.HTTPRoute, len(list.Items))
	for i := range list.Items {
		r := &list.Items[i]
		observed[r.Namespace+"/"+r.Name] = r
	}
	return observed, nil
}
