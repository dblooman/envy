package kubernetes

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/watch"
	ktesting "k8s.io/client-go/testing"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
)

func TestObservationCacheAvoidsReadsButMutationsReadFresh(t *testing.T) {
	p, client, s := fixture()
	ctx := t.Context()
	if _, err := p.Ensure(ctx, s); err != nil {
		t.Fatal(err)
	}

	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{{Group: "networking.istio.io", Version: "v1", Resource: "virtualservices"}: "VirtualServiceList"})
	cache := NewObservations(client, dyn, "istio")
	startup, stop := context.WithTimeout(ctx, 3*time.Second)
	defer stop()
	if err := cache.Start(startup, func(string) {}); err != nil {
		t.Fatal(err)
	}

	p.WithObservations(cache)
	client.ClearActions()
	if _, err := p.Ensure(ctx, s); err != nil {
		t.Fatal(err)
	}

	if len(client.Actions()) != 0 {
		t.Fatalf("unchanged workload made API calls: %v", client.Actions())
	}

	s.Image = "envy/service-b:v3"
	if _, err := p.Ensure(ctx, s); err != nil {
		t.Fatal(err)
	}

	read := false
	write := false
	for _, a := range client.Actions() {
		if a.GetResource().Resource != "deployments" {
			continue
		}

		if a.GetVerb() == "get" {
			read = true
		}

		if a.GetVerb() == "patch" {
			if !read {
				t.Fatal("mutation used cached authorization")
			}

			write = true
		}
	}

	if !write {
		t.Fatal("image update did not reach API")
	}
}

func TestObservationWatchReconnect(t *testing.T) {
	_, client, _ := fixture()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	watches := make(chan *watch.RaceFreeFakeWatcher, 4)
	client.PrependWatchReactor("services", func(ktesting.Action) (bool, watch.Interface, error) {
		w := watch.NewRaceFreeFake()
		watches <- w
		return true, w, nil
	})
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{{Group: "networking.istio.io", Version: "v1", Resource: "virtualservices"}: "VirtualServiceList"})
	o := NewObservations(client, dyn, "istio")
	events := make(chan string, 16)
	if err := o.Start(ctx, func(ns string) { events <- ns }); err != nil {
		t.Fatal(err)
	}

	var first *watch.RaceFreeFakeWatcher
	select {
	case first = <-watches:
	case <-ctx.Done():
		t.Fatal("watch did not start")
	}

	first.Stop()
	var second *watch.RaceFreeFakeWatcher
	select {
	case second = <-watches:
	case <-ctx.Done():
		t.Fatal("watch did not reconnect")
	}

	second.Add(&corev1.Service{Name: "dependency", Namespace: "reconnected", ResourceVersion: "2"})
	for {
		select {
		case ns := <-events:
			if ns == "reconnected" {
				return
			}
		case <-ctx.Done():
			t.Fatal("reconnected watch lost Service event")
		}
	}
}
