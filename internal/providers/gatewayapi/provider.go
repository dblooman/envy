// Package gatewayapi implements the shared Cilium and Linkerd route lifecycle.
package gatewayapi

import (
	"context"
	"crypto/sha256"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/dblooman/envy/internal/domain"
	"github.com/dblooman/envy/internal/mesh"
	"github.com/dblooman/envy/internal/routing"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	kube "k8s.io/client-go/kubernetes"
	v1 "sigs.k8s.io/gateway-api/apis/v1"
	beta "sigs.k8s.io/gateway-api/apis/v1beta1"
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
	kube         kube.Interface
	profile      mesh.Profile
}

func New(client gatewayclient.Interface, installation string, guard func(context.Context) error) *Provider {
	return NewWithGatewayClass(client, installation, guard, "cilium")
}
func NewWithGatewayClass(client gatewayclient.Interface, installation string, guard func(context.Context) error, class string) *Provider {
	profile, _ := mesh.Resolve("cilium")
	return NewProfile(client, installation, guard, class, profile)
}
func NewProfile(client gatewayclient.Interface, installation string, guard func(context.Context) error, class string, profile mesh.Profile) *Provider {
	if class == "" {
		class = profile.GatewayClass
	}
	return &Provider{client: client, installation: installation, guard: guard, gatewayClass: class, profile: profile}
}
func ptr[T any](v T) *T          { return &v }
func key(m metav1.Object) string { return m.GetNamespace() + "/" + m.GetName() }
func (p *Provider) writable(ctx context.Context) error {
	if p.guard == nil {
		return fmt.Errorf("provider mutation requires leadership guard")
	}
	return p.guard(ctx)
}
func (p *Provider) owned(m metav1.Object, token string) bool {
	return token != "" && m.GetLabels()[installationLabel] == p.installation && m.GetAnnotations()[ownershipAnnotation] == token
}
func aggregateToken(installation string) string { return "aggregate:" + installation }
func hostOverlap(a, b string) bool {
	return a == b || a == "*" || b == "*" || strings.HasPrefix(a, "*.") && strings.HasSuffix(b, a[1:]) || strings.HasPrefix(b, "*.") && strings.HasSuffix(a, b[1:])
}
func parseServiceHost(host, ns string) (string, string) {
	parts := strings.Split(host, ".")
	if len(parts) > 1 {
		return parts[0], parts[1]
	}
	return host, ns
}
func domains(s domain.RouteSnapshot) []domain.RouteDomain {
	byHost := map[string]domain.RouteDomain{}
	for _, d := range s.Domains {
		byHost[d.ServiceHost] = d
	}
	for _, e := range s.MeshEntries {
		byHost[e.Domain.ServiceHost] = e.Domain
	}
	out := []domain.RouteDomain{}
	for _, d := range byHost {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ServiceHost < out[j].ServiceHost })
	return out
}
func (p *Provider) metadata(ns, name, role, id, token string) metav1.ObjectMeta {
	return metav1.ObjectMeta{Namespace: ns, Name: name, Labels: map[string]string{installationLabel: p.installation, roleLabel: role, compositionLabel: id}, Annotations: map[string]string{ownershipAnnotation: token}}
}
func backend(host, ns string, port int32) v1.HTTPBackendRef {
	name, ns := parseServiceHost(host, ns)
	return v1.HTTPBackendRef{BackendRef: v1.BackendRef{Weight: ptr(int32(1)), BackendObjectReference: v1.BackendObjectReference{Group: ptr(v1.Group("")), Kind: ptr(v1.Kind("Service")), Name: v1.ObjectName(name), Namespace: ptr(v1.Namespace(ns)), Port: ptr(v1.PortNumber(port))}}}
}
func meshName(e domain.RouteEntry) string {
	sum := sha256.Sum256([]byte(e.Domain.ServiceHost + "/" + e.CompositionID))
	return fmt.Sprintf("envy-mesh-%x", sum[:16])
}
func (p *Provider) desired(s domain.RouteSnapshot) (map[string]*v1.HTTPRoute, map[string]*beta.ReferenceGrant, error) {
	routes := map[string]*v1.HTTPRoute{}
	grants := map[string]*beta.ReferenceGrant{}
	for _, d := range domains(s) {
		if d.Namespace == "" || d.ServiceHost == "" || d.AggregateName == "" || d.Port < 1 {
			return nil, nil, fmt.Errorf("invalid routing domain")
		}
		name, ns := parseServiceHost(d.ServiceHost, d.Namespace)
		if ns != d.Namespace {
			return nil, nil, fmt.Errorf("producer route must share the parent Service namespace")
		}
		r := &v1.HTTPRoute{ObjectMeta: p.metadata(ns, d.AggregateName, "aggregate", "", aggregateToken(p.installation)), Spec: v1.HTTPRouteSpec{CommonRouteSpec: v1.CommonRouteSpec{ParentRefs: []v1.ParentReference{{Group: ptr(v1.Group("")), Kind: ptr(v1.Kind("Service")), Name: v1.ObjectName(name), Port: ptr(v1.PortNumber(d.Port))}}}, Rules: []v1.HTTPRouteRule{{BackendRefs: []v1.HTTPBackendRef{backend(d.ServiceHost, ns, d.Port)}}}}}
		routes[key(r)] = r
	}
	for _, e := range s.MeshEntries {
		token := s.OwnedCompositions[e.CompositionID]
		if token == "" {
			return nil, nil, fmt.Errorf("missing persisted ownership for %s", e.CompositionID)
		}
		base := routes[e.Domain.Namespace+"/"+e.Domain.AggregateName]
		r := base.DeepCopy()
		r.ObjectMeta = p.metadata(e.Domain.Namespace, meshName(e), "mesh", e.CompositionID, token)
		r.Spec.Rules = []v1.HTTPRouteRule{{Matches: []v1.HTTPRouteMatch{{Headers: []v1.HTTPHeaderMatch{{Name: "baggage", Type: ptr(v1.HeaderMatchRegularExpression), Value: routing.BaggagePattern(e.CompositionID)}}}}, BackendRefs: []v1.HTTPBackendRef{backend(e.DestinationHost, domain.NamespaceForID(e.CompositionID), e.Port)}}}
		routes[key(r)] = r
	}
	for _, e := range s.IngressEntries {
		if e.OwnershipToken == "" || s.OwnedCompositions[e.CompositionID] != e.OwnershipToken {
			return nil, nil, fmt.Errorf("missing ingress ownership for %s", e.CompositionID)
		}
		parent := v1.ParentReference{Group: ptr(v1.Group(v1.GroupName)), Kind: ptr(v1.Kind("Gateway")), Name: v1.ObjectName(e.Domain.Gateway), Namespace: ptr(v1.Namespace(e.Domain.GatewayNS()))}
		if e.Domain.GatewaySectionName != "" {
			parent.SectionName = ptr(v1.SectionName(e.Domain.GatewaySectionName))
		}
		r := &v1.HTTPRoute{ObjectMeta: p.metadata(e.Domain.Namespace, "envy-ingress-"+e.CompositionID, "ingress", e.CompositionID, e.OwnershipToken), Spec: v1.HTTPRouteSpec{CommonRouteSpec: v1.CommonRouteSpec{ParentRefs: []v1.ParentReference{parent}}, Hostnames: []v1.Hostname{v1.Hostname(e.Host)}, Rules: []v1.HTTPRouteRule{{Filters: []v1.HTTPRouteFilter{
			{Type: v1.HTTPRouteFilterRequestHeaderModifier, RequestHeaderModifier: &v1.HTTPHeaderFilter{Set: []v1.HTTPHeader{{Name: "baggage", Value: "composition=" + e.CompositionID}}}},
			{Type: v1.HTTPRouteFilterResponseHeaderModifier, ResponseHeaderModifier: &v1.HTTPHeaderFilter{Set: []v1.HTTPHeader{{Name: v1.HTTPHeaderName(domain.PreviewRouteHeader), Value: e.CompositionID}}}},
		}, BackendRefs: []v1.HTTPBackendRef{backend(e.DestinationHost, e.Domain.Namespace, e.Port)}}}}}
		routes[key(r)] = r
	}
	for _, r := range routes {
		// Include API-server defaults so an unchanged route remains a no-op
		// after reading it back from a real cluster.
		for i := range r.Spec.Rules {
			if len(r.Spec.Rules[i].Matches) == 0 {
				r.Spec.Rules[i].Matches = []v1.HTTPRouteMatch{{}}
			}
			for j := range r.Spec.Rules[i].Matches {
				r.Spec.Rules[i].Matches[j].Path = &v1.HTTPPathMatch{Type: ptr(v1.PathMatchPathPrefix), Value: ptr("/")}
			}
		}
		for _, rule := range r.Spec.Rules {
			for _, b := range rule.BackendRefs {
				ns := r.Namespace
				if b.Namespace != nil {
					ns = string(*b.Namespace)
				}
				if ns == r.Namespace {
					continue
				}
				id := r.Labels[compositionLabel]
				token := s.OwnedCompositions[id]
				if ns != domain.NamespaceForID(id) || token == "" {
					return nil, nil, fmt.Errorf("cross-namespace destination %s must belong to composition %s", ns, id)
				}
				sum := sha256.Sum256([]byte(r.Namespace + "/" + string(b.Name)))
				name := fmt.Sprintf("envy-allow-%x", sum[:12])
				svc := v1.ObjectName(b.Name)
				g := &beta.ReferenceGrant{ObjectMeta: p.metadata(ns, name, "grant", id, token), Spec: beta.ReferenceGrantSpec{From: []beta.ReferenceGrantFrom{{Group: v1.GroupName, Kind: "HTTPRoute", Namespace: v1.Namespace(r.Namespace)}}, To: []beta.ReferenceGrantTo{{Group: "", Kind: "Service", Name: &svc}}}}
				grants[key(g)] = g
			}
		}
	}
	return routes, grants, nil
}
func (p *Provider) Validate(ctx context.Context, s domain.RouteSnapshot) error {
	_, err := p.inspect(ctx, s)
	return err
}
func (p *Provider) inspect(ctx context.Context, s domain.RouteSnapshot) (map[string]*v1.HTTPRoute, error) {
	list, err := p.client.GatewayV1().HTTPRoutes("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	out := map[string]*v1.HTTPRoute{}
	for i := range list.Items {
		r := &list.Items[i]
		out[key(r)] = r
		for _, d := range domains(s) {
			svc, _ := parseServiceHost(d.ServiceHost, d.Namespace)
			for _, ref := range r.Spec.ParentRefs {
				ns := r.Namespace
				if ref.Namespace != nil {
					ns = string(*ref.Namespace)
				}
				kind := "Gateway"
				if ref.Kind != nil {
					kind = string(*ref.Kind)
				}
				serviceGroup := ref.Group != nil && (*ref.Group == "" || *ref.Group == "core")
				if kind == "Service" && serviceGroup && ns == d.Namespace && string(ref.Name) == svc && (ref.Port == nil || int32(*ref.Port) == d.Port) {
					token := s.OwnedCompositions[r.Labels[compositionLabel]]
					if r.Labels[roleLabel] == "aggregate" && r.Name == d.AggregateName {
						token = aggregateToken(p.installation)
					}
					if !p.owned(r, token) {
						return nil, fmt.Errorf("baseline mesh service already claimed by HTTPRoute %s", key(r))
					}
				}
			}
		}
		for _, e := range s.IngressEntries {
			if !attachesToGateway(r, e.Domain.GatewayNS(), e.Domain.Gateway) {
				continue
			}
			if e.Domain.GatewaySectionName != "" && !overlapsSection(r, e.Domain.GatewayNS(), e.Domain.Gateway, e.Domain.GatewaySectionName) {
				continue
			}
			if p.owned(r, e.OwnershipToken) && r.Name == "envy-ingress-"+e.CompositionID {
				continue
			}
			hosts := r.Spec.Hostnames
			if len(hosts) == 0 {
				hosts = []v1.Hostname{"*"}
			}
			for _, h := range hosts {
				if hostOverlap(string(h), e.Host) {
					return nil, fmt.Errorf("ingress host %s already claimed by %s", e.Host, key(r))
				}
			}
		}
	}
	return out, nil
}
func overlapsSection(r *v1.HTTPRoute, namespace, gateway, section string) bool {
	for _, ref := range r.Spec.ParentRefs {
		parent := &v1.HTTPRoute{ObjectMeta: r.ObjectMeta, Spec: v1.HTTPRouteSpec{CommonRouteSpec: v1.CommonRouteSpec{ParentRefs: []v1.ParentReference{ref}}}}
		if attachesToGateway(parent, namespace, gateway) && (ref.SectionName == nil || string(*ref.SectionName) == section) {
			return true
		}
	}
	return false
}
func sortedKeys[T any](m map[string]T) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
func (p *Provider) Reconcile(ctx context.Context, s domain.RouteSnapshot) (domain.RouteObservation, error) {
	want, grants, err := p.desired(s)
	if err != nil {
		return domain.RouteObservation{}, err
	}
	observed, err := p.inspect(ctx, s)
	if err != nil {
		return domain.RouteObservation{}, err
	}
	grantList, err := p.client.GatewayV1beta1().ReferenceGrants("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return domain.RouteObservation{}, err
	}
	oldGrants := map[string]*beta.ReferenceGrant{}
	for i := range grantList.Items {
		g := &grantList.Items[i]
		oldGrants[key(g)] = g
	}
	// Establish ownership of all objects before any write, including retired objects.
	for k, r := range observed {
		if w := want[k]; w != nil {
			if !p.owned(r, w.Annotations[ownershipAnnotation]) {
				return domain.RouteObservation{}, fmt.Errorf("route ownership conflict: %s", k)
			}
		} else if r.Labels[installationLabel] == p.installation {
			token := s.OwnedCompositions[r.Labels[compositionLabel]]
			if r.Labels[roleLabel] == "aggregate" {
				token = aggregateToken(p.installation)
			}
			if !p.owned(r, token) {
				return domain.RouteObservation{}, fmt.Errorf("stale route ownership not established: %s", k)
			}
		}
	}
	for k, g := range oldGrants {
		if w := grants[k]; w != nil {
			if !p.owned(g, w.Annotations[ownershipAnnotation]) {
				return domain.RouteObservation{}, fmt.Errorf("grant ownership conflict: %s", k)
			}
		} else if g.Labels[installationLabel] == p.installation && !p.owned(g, s.OwnedCompositions[g.Labels[compositionLabel]]) {
			return domain.RouteObservation{}, fmt.Errorf("stale grant ownership not established: %s", k)
		}
	}
	for _, k := range sortedKeys(grants) {
		g := grants[k]
		old := oldGrants[k]
		if old != nil && reflect.DeepEqual(old.Spec, g.Spec) {
			continue
		}
		if err = p.writable(ctx); err != nil {
			return domain.RouteObservation{}, err
		}
		api := p.client.GatewayV1beta1().ReferenceGrants(g.Namespace)
		if old == nil {
			_, err = api.Create(ctx, g, metav1.CreateOptions{})
		} else {
			g.ResourceVersion = old.ResourceVersion
			_, err = api.Update(ctx, g, metav1.UpdateOptions{})
		}
		if err != nil {
			return domain.RouteObservation{}, err
		}
	}
	changed := map[string]bool{}
	for _, k := range sortedKeys(want) {
		changed[k] = observed[k] == nil || !reflect.DeepEqual(observed[k].Spec, want[k].Spec)
		if err = p.ensureHTTPRoute(ctx, want[k], observed[k]); err != nil {
			return domain.RouteObservation{}, err
		}
	}
	for _, k := range sortedKeys(observed) {
		r := observed[k]
		if want[k] != nil || r.Labels[installationLabel] != p.installation {
			continue
		}
		if err = p.writable(ctx); err != nil {
			return domain.RouteObservation{}, err
		}
		uid := types.UID(r.UID)
		rv := r.ResourceVersion
		err = p.client.GatewayV1().HTTPRoutes(r.Namespace).Delete(ctx, r.Name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid, ResourceVersion: &rv}})
		if err != nil && !apierrors.IsNotFound(err) {
			return domain.RouteObservation{}, err
		}
		if _, err = p.client.GatewayV1().HTTPRoutes(r.Namespace).Get(ctx, r.Name, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
			if err != nil {
				return domain.RouteObservation{}, err
			}
			return domain.RouteObservation{Message: "waiting for retired route deletion: " + k}, nil
		}
	}
	for _, k := range sortedKeys(oldGrants) {
		g := oldGrants[k]
		if grants[k] != nil || g.Labels[installationLabel] != p.installation {
			continue
		}
		if err = p.writable(ctx); err != nil {
			return domain.RouteObservation{}, err
		}
		uid := types.UID(g.UID)
		rv := g.ResourceVersion
		err = p.client.GatewayV1beta1().ReferenceGrants(g.Namespace).Delete(ctx, g.Name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid, ResourceVersion: &rv}})
		if err != nil && !apierrors.IsNotFound(err) {
			return domain.RouteObservation{}, err
		}
		if _, err = p.client.GatewayV1beta1().ReferenceGrants(g.Namespace).Get(ctx, g.Name, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
			if err != nil {
				return domain.RouteObservation{}, err
			}
			return domain.RouteObservation{Message: "waiting for retired grant deletion: " + k}, nil
		}
	}
	for _, k := range sortedKeys(want) {
		r := observed[k] // This call's fresh list already contains unchanged route status.
		if changed[k] {
			r, err = p.client.GatewayV1().HTTPRoutes(want[k].Namespace).Get(ctx, want[k].Name, metav1.GetOptions{})
			if err != nil {
				return domain.RouteObservation{}, err
			}
		}
		controller := p.profile.MeshController
		if r.Labels[roleLabel] == "ingress" {
			controller = p.profile.GatewayController
		}
		if msg := routePending(r, controller); msg != "" {
			return domain.RouteObservation{Message: k + ": " + msg}, nil
		}
	}
	checkedGateways := map[string]bool{}
	for _, e := range s.IngressEntries {
		gatewayKey := e.Domain.GatewayNS() + "/" + e.Domain.Gateway + "/" + e.Domain.GatewaySectionName + "/" + e.Domain.Namespace
		if checkedGateways[gatewayKey] {
			continue
		}
		if err = p.CheckGateway(ctx, e.Domain.GatewayNS(), e.Domain.Gateway, e.Domain.GatewaySectionName, e.Domain.Namespace); err != nil {
			return domain.RouteObservation{Message: err.Error()}, nil
		}
		checkedGateways[gatewayKey] = true
	}
	return domain.RouteObservation{Ready: true, Message: "routing accepted by controllers; traffic verification required"}, nil
}
func (p *Provider) ensureHTTPRoute(ctx context.Context, want, got *v1.HTTPRoute) error {
	if got != nil {
		if !p.owned(got, want.Annotations[ownershipAnnotation]) {
			return fmt.Errorf("route ownership conflict: %s", key(got))
		}
		if reflect.DeepEqual(got.Spec, want.Spec) {
			return nil
		}
	}
	if err := p.writable(ctx); err != nil {
		return err
	}
	api := p.client.GatewayV1().HTTPRoutes(want.Namespace)
	if got == nil {
		_, err := api.Create(ctx, want, metav1.CreateOptions{})
		return err
	}
	got.Spec = want.Spec
	_, err := api.Update(ctx, got, metav1.UpdateOptions{})
	return err
}
func conditionPending(conditions []metav1.Condition, generation int64, names ...string) string {
	for _, name := range names {
		found := false
		for _, c := range conditions {
			if c.Type != name {
				continue
			}
			found = true
			if c.ObservedGeneration != generation {
				return fmt.Sprintf("waiting for %s at generation %d (controller observed generation %d)", name, generation, c.ObservedGeneration)
			}
			if c.Status != metav1.ConditionTrue {
				return name + ": " + c.Reason + " " + c.Message
			}
		}
		if !found {
			return "waiting for " + name
		}
	}
	return ""
}
func routePending(r *v1.HTTPRoute, controller string) string {
	for _, ref := range r.Spec.ParentRefs {
		found := false
		for _, s := range r.Status.Parents {
			if string(s.ControllerName) == controller && sameParent(s.ParentRef, ref, r.Namespace) {
				found = true
				if msg := conditionPending(s.Conditions, r.Generation, "Accepted", "ResolvedRefs"); msg != "" {
					return msg
				}
			}
		}
		if !found {
			return "waiting for parent status from " + controller
		}
	}
	return ""
}

func sameParent(a, b v1.ParentReference, ns string) bool {
	normalize := func(r v1.ParentReference) v1.ParentReference {
		if r.Group == nil {
			r.Group = ptr(v1.Group(v1.GroupName))
		}
		if r.Kind == nil {
			r.Kind = ptr(v1.Kind("Gateway"))
		}
		// Linkerd reports Kubernetes core Service parents using "core".
		if *r.Kind == "Service" && *r.Group == "core" {
			r.Group = ptr(v1.Group(""))
		}
		if r.Namespace == nil {
			r.Namespace = ptr(v1.Namespace(ns))
		}
		return r
	}
	return reflect.DeepEqual(normalize(a), normalize(b))
}
