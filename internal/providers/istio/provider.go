// Package istio reconciles complete shared routing snapshots into Istio resources.
package istio

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/dblooman/envy/internal/domain"
	"github.com/dblooman/envy/internal/routing"
	"google.golang.org/protobuf/proto"
	networking "istio.io/api/networking/v1alpha3"
	networkingv1 "istio.io/client-go/pkg/apis/networking/v1"
	istioclient "istio.io/client-go/pkg/clientset/versioned"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

const installationLabel = "envy.dev/installation"
const compositionLabel = "envy.dev/composition"
const ownershipAnnotation = "envy.dev/ownership-token"
const roleLabel = "envy.dev/route-role"

type Provider struct {
	client       istioclient.Interface
	installation string
	guard        func(context.Context) error
}

func New(client istioclient.Interface, installation string, guard func(context.Context) error) *Provider {
	return &Provider{client, installation, guard}
}
func (p *Provider) writable(ctx context.Context) error {
	if p.guard == nil {
		return fmt.Errorf("provider mutation requires leadership guard")
	}
	return p.guard(ctx)
}
func (p *Provider) owned(v *networkingv1.VirtualService, token string) bool {
	return token != "" && v.Labels[installationLabel] == p.installation && v.Annotations[ownershipAnnotation] == token
}
func route(host string, port int32) *networking.HTTPRouteDestination {
	return &networking.HTTPRouteDestination{Destination: &networking.Destination{Host: host, Port: &networking.PortSelector{Number: uint32(port)}}}
}
func aggregateToken(installation string) string { return "aggregate:" + installation }
func hostOverlap(a, b string) bool {
	if a == b || a == "*" || b == "*" {
		return true
	}
	if strings.HasPrefix(a, "*.") && strings.HasSuffix(b, a[1:]) {
		return true
	}
	return strings.HasPrefix(b, "*.") && strings.HasSuffix(a, b[1:])
}
func mesh(v *networkingv1.VirtualService) bool {
	if len(v.Spec.Gateways) == 0 {
		return true
	}
	return slices.Contains(v.Spec.Gateways, "mesh")
}
func preview(v *networkingv1.VirtualService, d domain.RouteDomain) bool {
	for _, g := range v.Spec.Gateways {
		if g == d.Namespace+"/"+d.Gateway || (g == d.Gateway && v.Namespace == d.Namespace) {
			return true
		}
	}
	return false
}
func normalizeHost(host, ns string) string {
	if !strings.Contains(host, ".") && host != "*" {
		return host + "." + ns + ".svc.cluster.local"
	}
	return host
}

// domains includes empty domains so removal of the last composition restores
// the baseline route before its namespace can be deleted.
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
func (p *Provider) Validate(ctx context.Context, snapshot domain.RouteSnapshot) error {
	list, err := p.client.NetworkingV1().VirtualServices("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, d := range domains(snapshot) {
		if d.Namespace == "" || d.ServiceHost == "" || d.AggregateName == "" || d.Port < 1 {
			return fmt.Errorf("invalid routing domain")
		}
		for _, v := range list.Items {
			if v.Namespace == d.Namespace && v.Name == d.AggregateName {
				if !p.owned(v, aggregateToken(p.installation)) {
					return fmt.Errorf("aggregate routing ownership conflict: %s/%s", v.Namespace, v.Name)
				}
				continue
			}
			for _, h := range v.Spec.Hosts {
				if mesh(v) && hostOverlap(normalizeHost(h, v.Namespace), d.ServiceHost) {
					return fmt.Errorf("baseline mesh host is already owned by VirtualService %s/%s", v.Namespace, v.Name)
				}
			}
		}
	}

	// Gateway objects can share the same ingress proxy. Exact-host ownership
	// must therefore be checked across all such Gateways, not just one name.
	gateways, err := p.client.NetworkingV1().Gateways("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	shared := map[string]bool{}
	for _, g := range gateways.Items {
		if g.Spec.Selector["istio"] == "ingressgateway" {
			shared[g.Namespace+"/"+g.Name] = true
		}
	}
	for _, e := range snapshot.IngressEntries {
		for _, v := range list.Items {
			sameIngress := preview(v, e.Domain)
			for _, name := range v.Spec.Gateways {
				if !strings.Contains(name, "/") {
					name = v.Namespace + "/" + name
				}
				sameIngress = sameIngress || shared[name]
			}
			if !sameIngress {
				continue
			}
			for _, h := range v.Spec.Hosts {
				if hostOverlap(h, e.Host) && !(v.Namespace == e.Domain.Namespace && v.Name == "envy-ingress-"+e.CompositionID && p.owned(v, e.OwnershipToken)) {
					return fmt.Errorf("preview host conflict: %s overlaps VirtualService %s/%s", e.Host, v.Namespace, v.Name)
				}
			}
		}
	}
	return nil
}
func (p *Provider) Reconcile(ctx context.Context, snapshot domain.RouteSnapshot) (domain.RouteObservation, error) {
	if err := p.Validate(ctx, snapshot); err != nil {
		return domain.RouteObservation{}, err
	}
	if err := p.writable(ctx); err != nil {
		return domain.RouteObservation{}, err
	}
	entries := append([]domain.RouteEntry(nil), snapshot.MeshEntries...)
	sort.Slice(entries, func(i, j int) bool { return entries[i].CompositionID < entries[j].CompositionID })
	want := map[string]domain.RouteEntry{}
	for _, e := range snapshot.IngressEntries {
		want[e.Domain.Namespace+"/envy-ingress-"+e.CompositionID] = e
	}
	list, err := p.client.NetworkingV1().VirtualServices("").List(ctx, metav1.ListOptions{LabelSelector: installationLabel + "=" + p.installation + "," + roleLabel + "=ingress"})
	if err != nil {
		return domain.RouteObservation{}, err
	}
	for _, v := range list.Items {
		if _, ok := want[v.Namespace+"/"+v.Name]; ok {
			continue
		}
		id := v.Labels[compositionLabel]
		token := snapshot.OwnedCompositions[id]
		if id == "" || v.Name != "envy-ingress-"+id || !p.owned(v, token) {
			return domain.RouteObservation{}, fmt.Errorf("stale ingress ownership is not established by persisted state: %s/%s", v.Namespace, v.Name)
		}
		if err = p.writable(ctx); err != nil {
			return domain.RouteObservation{}, err
		}
		uid := types.UID(v.UID)
		err = p.client.NetworkingV1().VirtualServices(v.Namespace).Delete(ctx, v.Name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}})
		if err != nil && !apierrors.IsNotFound(err) {
			return domain.RouteObservation{}, err
		}
	}
	for _, d := range domains(snapshot) {
		aggregate := &networkingv1.VirtualService{Name: d.AggregateName, Namespace: d.Namespace, Labels: map[string]string{installationLabel: p.installation, roleLabel: "aggregate"}, Annotations: map[string]string{ownershipAnnotation: aggregateToken(p.installation)}, Spec: networking.VirtualService{Hosts: []string{d.ServiceHost}, Gateways: []string{"mesh"}}}
		for _, e := range entries {
			if e.Domain.ServiceHost != d.ServiceHost {
				continue
			}
			aggregate.Spec.Http = append(aggregate.Spec.Http, &networking.HTTPRoute{Name: "composition-" + e.CompositionID, Match: []*networking.HTTPMatchRequest{{Headers: map[string]*networking.StringMatch{"baggage": {MatchType: &networking.StringMatch_Regex{Regex: routing.BaggagePattern(e.CompositionID)}}}}}, Route: []*networking.HTTPRouteDestination{route(e.DestinationHost, e.Port)}})
		}
		aggregate.Spec.Http = append(aggregate.Spec.Http, &networking.HTTPRoute{Name: "baseline", Route: []*networking.HTTPRouteDestination{route(d.ServiceHost, d.Port)}})
		if err = p.ensure(ctx, aggregate); err != nil {
			return domain.RouteObservation{}, err
		}
	}
	names := make([]string, 0, len(want))
	for name := range want {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		e := want[name]
		v := &networkingv1.VirtualService{Name: "envy-ingress-" + e.CompositionID, Namespace: e.Domain.Namespace, Labels: map[string]string{installationLabel: p.installation, compositionLabel: e.CompositionID, roleLabel: "ingress"}, Annotations: map[string]string{ownershipAnnotation: e.OwnershipToken}, Spec: networking.VirtualService{Hosts: []string{e.Host}, Gateways: []string{e.Domain.Gateway}, Http: []*networking.HTTPRoute{{Name: "composition", Headers: &networking.Headers{Request: &networking.Headers_HeaderOperations{Set: map[string]string{"baggage": "composition=" + e.CompositionID}}}, Route: []*networking.HTTPRouteDestination{route(e.DestinationHost, e.Port)}}}}}
		if err = p.ensure(ctx, v); err != nil {
			return domain.RouteObservation{}, err
		}
	}
	return domain.RouteObservation{Ready: true, Message: "routing resources accepted; proxy convergence requires verification"}, nil
}
func (p *Provider) ensure(ctx context.Context, want *networkingv1.VirtualService) error {
	api := p.client.NetworkingV1().VirtualServices(want.Namespace)
	got, err := api.Get(ctx, want.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		if err = p.writable(ctx); err != nil {
			return err
		}
		_, err = api.Create(ctx, want, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	if !p.owned(got, want.Annotations[ownershipAnnotation]) {
		return fmt.Errorf("routing object ownership conflict: %s", want.Name)
	}
	if !proto.Equal(&got.Spec, &want.Spec) {
		// Generated protobuf values contain synchronization state and must not be
		// copied after use (proto.Equal above initializes that state).
		proto.Reset(&got.Spec)
		proto.Merge(&got.Spec, &want.Spec)
		if err = p.writable(ctx); err != nil {
			return err
		}
		_, err = api.Update(ctx, got, metav1.UpdateOptions{})
	}
	return err
}
