package reconciler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dblooman/envy/internal/domain"
)

type memoryMessaging struct {
	err              error
	ensures, deletes int
	onDelete         func()
	names            []string
}

func (m *memoryMessaging) Validate(context.Context, domain.Baseline) error { return m.err }
func (m *memoryMessaging) Ensure(_ context.Context, s domain.MessagingSpec) ([]domain.MessageSubscription, error) {
	m.ensures++
	out := append([]domain.MessageSubscription(nil), s.Subscriptions...)
	for i := range out {
		out[i].Ready = true
		m.names = append(m.names, out[i].Name)
	}

	return out, m.err
}

func (m *memoryMessaging) Delete(context.Context, domain.MessagingSpec) (bool, error) {
	m.deletes++
	if m.onDelete != nil {
		m.onDelete()
	}

	return m.err == nil, m.err
}

func TestMessagingPrecedesPublishersAndSurvivesConsumerAttachment(t *testing.T) {
	r, store, runtime, _, _, now := setup(t)
	m := &memoryMessaging{err: errors.New("unprotected subscription")}
	r.cfg.Messaging = m
	c := store.records["a"]
	c.MessageIsolation = true
	c.Runtime.Plan.MessageSubscriptions = []domain.MessageSubscription{{Name: "projects/test-project/subscriptions/preview", Component: "service-a", SubscriptionEnv: "SUB", Retention: "604800s"}}
	c.MessageSubscriptions = append([]domain.MessageSubscription(nil), c.Runtime.Plan.MessageSubscriptions...)
	store.records["a"] = c
	tick(t, r)
	if len(runtime.created) != 0 || store.records["a"].Runtime.RoutingActive {
		t.Fatal("publisher started before isolation")
	}

	m.err = nil
	*now = now.Add(time.Minute)
	runtime.onEnsure = func() {
		if m.ensures == 0 || !store.records["a"].MessageSubscriptions[0].Ready {
			t.Error("publisher started before durable subscription readiness")
		}
	}
	tick(t, r)
	c = store.records["a"]
	if c.Phase != domain.PhaseReady || !c.MessageSubscriptions[0].Ready {
		t.Fatalf("producer-only preview not ready: %+v", c)
	}

	if r.routeCycle != nil {
		t.Fatal("cycle leaked")
	}

	// Attach an approved worker to the same persisted composition.
	c.Overrides["service-a"] = domain.ComponentOverride{Image: "worker:v2"}
	if c.Runtime.Plan.Components == nil {
		c.Runtime.Plan.Components = c.Runtime.Plan.Profiles()
	}

	c.Runtime.Plan.Components["service-a"] = domain.Component{ID: "service-a", Port: 8080}
	c.Generation++
	c.LatestOperation = domain.Operation{Kind: "update", Status: "pending"}
	c.Runtime.NextAttemptAt = time.Time{}
	store.records[c.ID] = c
	restarted := New(store, runtime, r.routes, r.verifier, r.guard, r.log, r.cfg)
	restarted.now = r.now
	tick(t, restarted)
	c = store.records["a"]
	if c.Phase != domain.PhaseReady || c.MessageSubscriptions[0].Name != m.names[0] || m.deletes != 0 {
		t.Fatal("consumer attachment replaced subscription")
	}

	c.DeletionRequested = true
	c.Generation++
	c.Runtime.NextAttemptAt = time.Time{}
	store.records[c.ID] = c
	runtime.absent = true
	m.onDelete = func() {
		if runtime.deletes == 0 {
			t.Error("subscription deleted before workload")
		}
	}
	tick(t, restarted)
	*now = now.Add(time.Minute)
	tick(t, restarted)
	if m.deletes != 1 || store.records[c.ID].Phase != domain.PhaseDestroyed {
		t.Fatal("messaging cleanup incomplete")
	}
}

func TestMessagingCleanupRetriesWithoutBaselineFallback(t *testing.T) {
	r, store, runtime, _, _, now := setup(t)
	runtime.absent = true
	m := &memoryMessaging{err: errors.New("permission denied")}
	r.cfg.Messaging = m
	c := store.records["a"]
	c.MessageIsolation = true
	c.DeletionRequested = true
	store.records[c.ID] = c
	tick(t, r)
	if store.records[c.ID].Phase == domain.PhaseDestroyed {
		t.Fatal("destroyed before subscriptions removed")
	}

	m.err = nil
	*now = now.Add(time.Minute)
	tick(t, r)
	if store.records[c.ID].Phase != domain.PhaseDestroyed {
		t.Fatal("cleanup did not recover")
	}
}
