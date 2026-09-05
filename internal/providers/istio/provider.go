// Package istio reconciles complete shared routing snapshots into Istio resources.
package istio

import (
	"context"
	"fmt"
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

const namespace = "envy-baseline"
const baselineService = "service-b.envy-baseline.svc.cluster.local"
const baselineGateway = "gateway.envy-baseline.svc.cluster.local"
const installationLabel = "envy.dev/installation"
const compositionLabel = "envy.dev/composition"
const ownershipAnnotation = "envy.dev/ownership-token"
const roleLabel = "envy.dev/route-role"
const aggregateName = "envy-service-b"

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
	for _, g := range v.Spec.Gateways {
		if g == "mesh" {
			return true
		}
	}
	return false
}
func preview(v *networkingv1.VirtualService) bool {
	for _, g := range v.Spec.Gateways {
		if g == namespace+"/envy-preview" || (g == "envy-preview" && v.Namespace == namespace) {
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
func (p *Provider) Validate(ctx context.Context, snapshot domain.RouteSnapshot) error {
	list, err := p.client.NetworkingV1().VirtualServices("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	tokens := map[string]string{}
	for _, e := range snapshot.IngressEntries {
		tokens[e.Host] = e.OwnershipToken
	}
	for i := range list.Items {
		v := list.Items[i]
		if v.Namespace == namespace && v.Name == aggregateName {
			if !p.owned(v, aggregateToken(p.installation)) {
				return fmt.Errorf("aggregate routing ownership conflict: %s/%s", v.Namespace, v.Name)
			}
			continue
		}
		for _, h := range v.Spec.Hosts {
			if mesh(v) && hostOverlap(normalizeHost(h, v.Namespace), baselineService) {
				return fmt.Errorf("baseline mesh host is already owned by VirtualService %s/%s", v.Namespace, v.Name)
			}
			if preview(v) {
				for host, token := range tokens {
					if hostOverlap(h, host) && !p.owned(v, token) {
						return fmt.Errorf("preview host conflict: %s overlaps VirtualService %s/%s", host, v.Namespace, v.Name)
					}
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
	// Copy before sorting because snapshots can be shared by callers.
	entries := append([]domain.RouteEntry(nil), snapshot.MeshEntries...)
	sort.Slice(entries, func(i, j int) bool { return entries[i].CompositionID < entries[j].CompositionID })
	aggregate := &networkingv1.VirtualService{ObjectMeta: metav1.ObjectMeta{Name: aggregateName, Namespace: namespace, Labels: map[string]string{installationLabel: p.installation, roleLabel: "aggregate"}, Annotations: map[string]string{ownershipAnnotation: aggregateToken(p.installation)}}, Spec: networking.VirtualService{Hosts: []string{baselineService}, Gateways: []string{"mesh"}}}
	for _, entry := range entries {
		aggregate.Spec.Http = append(aggregate.Spec.Http, &networking.HTTPRoute{Name: "composition-" + entry.CompositionID, Match: []*networking.HTTPMatchRequest{{Headers: map[string]*networking.StringMatch{"baggage": {MatchType: &networking.StringMatch_Regex{Regex: routing.BaggagePattern(entry.CompositionID)}}}}}, Route: []*networking.HTTPRouteDestination{route(entry.DestinationHost, entry.Port)}})
	}
	aggregate.Spec.Http = append(aggregate.Spec.Http, &networking.HTTPRoute{Name: "baseline", Route: []*networking.HTTPRouteDestination{route(baselineService, 8080)}})
	// Unpublish stale hosts before removing corresponding mesh entries.
	want := map[string]domain.RouteEntry{}
	for _, e := range snapshot.IngressEntries {
		want["envy-ingress-"+e.CompositionID] = e
	}
	list, err := p.client.NetworkingV1().VirtualServices(namespace).List(ctx, metav1.ListOptions{LabelSelector: installationLabel + "=" + p.installation + "," + roleLabel + "=ingress"})
	if err != nil {
		return domain.RouteObservation{}, err
	}
	for _, v := range list.Items {
		if _, ok := want[v.Name]; ok {
			continue
		}
		id := v.Labels[compositionLabel]
		token := snapshot.OwnedCompositions[id]
		if id == "" || v.Name != "envy-ingress-"+id || !p.owned(v, token) {
			return domain.RouteObservation{}, fmt.Errorf("stale ingress ownership is not established by persisted state: %s", v.Name)
		}
		if err = p.writable(ctx); err != nil {
			return domain.RouteObservation{}, err
		}
		uid := types.UID(v.UID)
		err = p.client.NetworkingV1().VirtualServices(namespace).Delete(ctx, v.Name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}})
		if err != nil && !apierrors.IsNotFound(err) {
			return domain.RouteObservation{}, err
		}
	}
	if err = p.ensure(ctx, aggregate); err != nil {
		return domain.RouteObservation{}, err
	}
	names := make([]string, 0, len(want))
	for name := range want {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		entry := want[name]
		v := &networkingv1.VirtualService{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: map[string]string{installationLabel: p.installation, compositionLabel: entry.CompositionID, roleLabel: "ingress"}, Annotations: map[string]string{ownershipAnnotation: entry.OwnershipToken}}, Spec: networking.VirtualService{Hosts: []string{entry.Host}, Gateways: []string{"envy-preview"}, Http: []*networking.HTTPRoute{{Name: "composition", Headers: &networking.Headers{Request: &networking.Headers_HeaderOperations{Set: map[string]string{"baggage": "composition=" + entry.CompositionID}}}, Route: []*networking.HTTPRouteDestination{route(baselineGateway, 8080)}}}}}
		if err = p.ensure(ctx, v); err != nil {
			return domain.RouteObservation{}, err
		}
	}
	return domain.RouteObservation{Ready: true, Message: "routing resources accepted; proxy convergence requires verification"}, nil
}
func (p *Provider) ensure(ctx context.Context, want *networkingv1.VirtualService) error {
	api := p.client.NetworkingV1().VirtualServices(namespace)
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
