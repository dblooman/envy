package istio

import (
	"context"
	"errors"
	"testing"

	"github.com/dblooman/envy/internal/domain"
	networking "istio.io/api/networking/v1alpha3"
	networkingv1 "istio.io/client-go/pkg/apis/networking/v1"
	"istio.io/client-go/pkg/clientset/versioned/fake"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func entry(id string) domain.RouteEntry {
	return domain.RouteEntry{CompositionID: id, Host: "cmp-" + id + ".envy.localhost", DestinationHost: "service-b.envy-" + id + ".svc.cluster.local", Port: 8080, OwnershipToken: "token-" + id}
}
func TestAggregateSnapshotsPreserveOtherCompositionsAndDeletionDrain(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()
	p := New(client, "test", func(context.Context) error { return nil })
	a, b := entry("a"), entry("b")
	snapshot := domain.RouteSnapshot{MeshEntries: []domain.RouteEntry{b, a}, IngressEntries: []domain.RouteEntry{a, b}, OwnedCompositions: map[string]string{"a": a.OwnershipToken, "b": b.OwnershipToken}}
	if _, err := p.Reconcile(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	vs, err := client.NetworkingV1().VirtualServices(namespace).Get(ctx, aggregateName, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(vs.Spec.Http) != 3 || vs.Spec.Http[0].Name != "composition-a" || vs.Spec.Http[2].Name != "baseline" {
		t.Fatalf("bad aggregate: %v", vs.Spec.Http)
	}
	ingress, _ := client.NetworkingV1().VirtualServices(namespace).Get(ctx, "envy-ingress-a", metav1.GetOptions{})
	if ingress.Spec.Http[0].Headers.Request.Set["baggage"] != "composition=a" || ingress.Spec.Http[0].Route[0].Destination.Host != baselineGateway {
		t.Fatal("incorrect ingress context or destination")
	}
	client.ClearActions()
	if _, err = p.Reconcile(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	for _, action := range client.Actions() {
		if action.GetVerb() == "update" || action.GetVerb() == "create" {
			t.Fatal("unchanged snapshot mutated routing")
		}
	}
	snapshot.IngressEntries = []domain.RouteEntry{b}
	if _, err = p.Reconcile(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	if _, err = client.NetworkingV1().VirtualServices(namespace).Get(ctx, "envy-ingress-a", metav1.GetOptions{}); err == nil {
		t.Fatal("deleted host still published")
	}
	vs, _ = client.NetworkingV1().VirtualServices(namespace).Get(ctx, aggregateName, metav1.GetOptions{})
	if len(vs.Spec.Http) != 3 {
		t.Fatal("mesh entry removed before caller drain")
	}
	snapshot.MeshEntries = []domain.RouteEntry{b}
	if _, err = p.Reconcile(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	vs, _ = client.NetworkingV1().VirtualServices(namespace).Get(ctx, aggregateName, metav1.GetOptions{})
	if len(vs.Spec.Http) != 2 || vs.Spec.Http[0].Name != "composition-b" {
		t.Fatal("other composition route lost")
	}
}
func TestRejectConflictingHostOwnershipBeforeMutation(t *testing.T) {
	for _, tc := range []struct {
		name, host string
		gateways   []string
	}{{"fqdn", baselineService, []string{"mesh"}}, {"short", "service-b", nil}, {"wildcard", "*", []string{"mesh"}}, {"ingress", "*.envy.localhost", []string{"envy-preview"}}} {
		t.Run(tc.name, func(t *testing.T) {
			client := fake.NewSimpleClientset(&networkingv1.VirtualService{ObjectMeta: metav1.ObjectMeta{Name: "someone-elses", Namespace: namespace}, Spec: networking.VirtualService{Hosts: []string{tc.host}, Gateways: tc.gateways}})
			p := New(client, "test", func(context.Context) error { return nil })
			a := entry("a")
			_, err := p.Reconcile(context.Background(), domain.RouteSnapshot{MeshEntries: []domain.RouteEntry{a}, IngressEntries: []domain.RouteEntry{a}})
			if err == nil {
				t.Fatal("overwrote host conflict")
			}
			for _, action := range client.Actions() {
				if action.GetVerb() != "list" {
					t.Fatalf("action before conflict rejection: %s", action.GetVerb())
				}
			}
		})
	}
}
func TestLostLeadershipPreventsRoutingMutation(t *testing.T) {
	client := fake.NewSimpleClientset()
	p := New(client, "test", func(context.Context) error { return errors.New("lost leader") })
	if _, err := p.Reconcile(context.Background(), domain.RouteSnapshot{}); err == nil {
		t.Fatal("ignored leader guard")
	}
	for _, a := range client.Actions() {
		if a.GetVerb() == "create" || a.GetVerb() == "update" || a.GetVerb() == "delete" {
			t.Fatal("mutated after lock loss")
		}
	}
}

func TestStaleIngressRequiresPersistedOwnership(t *testing.T) {
	for _, token := range []string{"", "wrong-owner"} {
		t.Run(token, func(t *testing.T) {
			v := &networkingv1.VirtualService{ObjectMeta: metav1.ObjectMeta{
				Name: "envy-ingress-a", Namespace: namespace,
				Labels:      map[string]string{installationLabel: "test", roleLabel: "ingress", compositionLabel: "a"},
				Annotations: map[string]string{ownershipAnnotation: "token-a"},
			}}
			client := fake.NewSimpleClientset(v)
			p := New(client, "test", func(context.Context) error { return nil })
			_, err := p.Reconcile(context.Background(), domain.RouteSnapshot{OwnedCompositions: map[string]string{"a": token}})
			if err == nil {
				t.Fatal("accepted labels without matching persisted ownership")
			}
			for _, action := range client.Actions() {
				if action.GetVerb() == "delete" {
					t.Fatal("deleted an unowned routing object")
				}
			}
		})
	}
}
