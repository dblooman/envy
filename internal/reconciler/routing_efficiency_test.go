package reconciler

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/dblooman/envy/internal/domain"
)

func TestStableCompositionsShareRoutingObservationWithinScan(t *testing.T) {
	r, store, runtime, routes, _, now := setup(t)
	store.records = map[string]domain.Composition{}
	for i := range 20 {
		c := fixture(*now)
		c.ID = fmt.Sprintf("c%02d", i)
		c.Runtime.OwnershipToken = "owner-" + c.ID
		c.Endpoints["public"] = domain.Endpoint{URL: "http://cmp-" + c.ID + ".envy.localhost:8080"}
		store.records[c.ID] = c
	}

	tick(t, r)
	if routes.calls != 20 || len(routes.last.IngressEntries) != 20 {
		t.Fatal("new route intentions must each be reconciled")
	}

	for range 2 {
		*now = now.Add(2 * time.Second)
		before := routes.calls
		tick(t, r)
		if routes.calls-before != 1 {
			t.Fatalf("stable scan made %d routing calls; want one fresh drift check", routes.calls-before)
		}
	}

	// A deletion committed while another composition is being reconciled must
	// invalidate reuse immediately, even though Tick started from older intent.
	ensures := 0
	runtime.onEnsure = func() {
		ensures++
		if ensures == 2 {
			c := store.records["c00"]
			c.DeletionRequested = true
			c.Generation++
			store.records[c.ID] = c
		}
	}
	before := routes.calls
	*now = now.Add(2 * time.Second)
	tick(t, r)
	if routes.calls-before != 2 {
		t.Fatalf("changed intent did not trigger a second route reconciliation: %d", routes.calls-before)
	}

	for _, e := range routes.last.IngressEntries {
		if e.CompositionID == "c00" {
			t.Fatal("concurrent deletion left hostname published")
		}
	}
}

type routingFunc func(context.Context, domain.RouteSnapshot) (domain.RouteObservation, error)

func (f routingFunc) Reconcile(ctx context.Context, snapshot domain.RouteSnapshot) (domain.RouteObservation, error) {
	return f(ctx, snapshot)
}

func TestPartialRoutingFailureInvalidatesEarlierObservation(t *testing.T) {
	r, store, _, _, _, _ := setup(t)
	tick(t, r)
	r.routeCycle = &routeCycle{}
	calls := 0
	r.routes = routingFunc(func(context.Context, domain.RouteSnapshot) (domain.RouteObservation, error) {
		calls++
		if calls == 2 {
			return domain.RouteObservation{}, errors.New("failed after partial route changes")
		}

		return domain.RouteObservation{Ready: true}, nil
	})
	ctx := context.Background()
	if err := r.syncRoutes(ctx); err != nil {
		t.Fatal(err)
	}

	original := clone(store.records["a"])
	changed := clone(original)
	changed.DeletionRequested = true
	store.records["a"] = changed
	if err := r.syncRoutes(ctx); err == nil {
		t.Fatal("ignored partial failure")
	}

	store.records["a"] = original
	if err := r.syncRoutes(ctx); err != nil {
		t.Fatal(err)
	}

	if calls != 3 {
		t.Fatal("reused an earlier snapshot after a partially applied change")
	}

	if err := r.syncRoutes(ctx); err != nil || calls != 3 {
		t.Fatal("successful identical snapshot was not reused")
	}

	r.guard = func(context.Context) error { return domain.ErrNotLeader }
	if err := r.syncRoutes(ctx); !errors.Is(err, domain.ErrNotLeader) || calls != 3 {
		t.Fatal("reused an observation after leadership was lost")
	}
}
