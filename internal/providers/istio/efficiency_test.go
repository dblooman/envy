package istio

import (
	"context"
	"fmt"
	"testing"

	"github.com/dblooman/envy/internal/domain"
	networkingv1 "istio.io/client-go/pkg/apis/networking/v1"
	"istio.io/client-go/pkg/clientset/versioned/fake"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8stesting "k8s.io/client-go/testing"
)

func TestTwentyRoutesUseConstantReadsAndRepairDrift(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()
	p := New(client, "test", func(context.Context) error { return nil })
	snapshot := domain.RouteSnapshot{OwnedCompositions: map[string]string{}}
	for i := range 20 {
		e := entry(fmt.Sprintf("c%02d", i))
		snapshot.MeshEntries = append(snapshot.MeshEntries, e)
		snapshot.IngressEntries = append(snapshot.IngressEntries, e)
		snapshot.OwnedCompositions[e.CompositionID] = e.OwnershipToken
	}

	if _, err := p.Reconcile(ctx, snapshot); err != nil {
		t.Fatal(err)
	}

	client.ClearActions()
	if _, err := p.Reconcile(ctx, snapshot); err != nil {
		t.Fatal(err)
	}

	if actions := client.Actions(); len(actions) != 2 || actions[0].GetVerb() != "list" || actions[1].GetVerb() != "list" {
		t.Fatalf("unchanged twenty-route snapshot needs only two list reads: %v", actions)
	}

	api := client.NetworkingV1().VirtualServices(namespace)
	if err := api.Delete(ctx, "envy-ingress-c00", metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}

	v, err := api.Get(ctx, aggregateName, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}

	v.Spec.Http = v.Spec.Http[len(v.Spec.Http)-1:]
	if _, err = api.Update(ctx, v, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}

	if _, err = p.Reconcile(ctx, snapshot); err != nil {
		t.Fatal(err)
	}

	if _, err = api.Get(ctx, "envy-ingress-c00", metav1.GetOptions{}); err != nil {
		t.Fatal("deleted ingress was not recreated")
	}

	v, err = api.Get(ctx, aggregateName, metav1.GetOptions{})
	if err != nil || len(v.Spec.Http) != 21 {
		t.Fatal("aggregate drift was not repaired")
	}
}

func TestListObservationRetainsWritePreconditions(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()
	p := New(client, "test", func(context.Context) error { return nil })
	a := entry("a")
	snapshot := domain.RouteSnapshot{MeshEntries: []domain.RouteEntry{a}, IngressEntries: []domain.RouteEntry{a}, OwnedCompositions: map[string]string{"a": a.OwnershipToken}}
	if _, err := p.Reconcile(ctx, snapshot); err != nil {
		t.Fatal(err)
	}

	api := client.NetworkingV1().VirtualServices(namespace)
	v, _ := api.Get(ctx, aggregateName, metav1.GetOptions{})
	v.ResourceVersion = "42"
	v.UID = "observed-aggregate"
	v.Spec.Http = nil
	if _, err := api.Update(ctx, v, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}

	updates := 0
	client.PrependReactor("update", "virtualservices", func(action k8stesting.Action) (bool, runtime.Object, error) {
		updates++
		got := action.(k8stesting.UpdateAction).GetObject().(*networkingv1.VirtualService)
		if got.ResourceVersion != "42" || got.UID != "observed-aggregate" {
			t.Fatal("update discarded the identity/version obtained by list")
		}

		return true, nil, apierrors.NewConflict(schema.GroupResource{Group: "networking.istio.io", Resource: "virtualservices"}, got.Name, fmt.Errorf("concurrent change"))
	})
	if _, err := p.Reconcile(ctx, snapshot); !apierrors.IsConflict(err) || updates != 1 {
		t.Fatalf("concurrent change must be returned for a fresh retry: %v", err)
	}
}
