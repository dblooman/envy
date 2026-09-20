package reconciler

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/dblooman/envy/internal/domain"
)

type queuedStore struct {
	mu       sync.Mutex
	records  map[string]domain.Composition
	messages chan string
}

func (s *queuedStore) Active(context.Context) ([]domain.Composition, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []domain.Composition
	for _, c := range s.records {
		if c.Phase != domain.PhaseDestroyed {
			out = append(out, clone(c))
		}
	}

	return out, nil
}

func (s *queuedStore) Get(_ context.Context, id string) (domain.Composition, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return clone(s.records[id]), nil
}

func (s *queuedStore) ActiveDomain(ctx context.Context, p, b string) ([]domain.Composition, error) {
	rows, _ := s.Active(ctx)
	var out []domain.Composition
	for _, c := range rows {
		if c.Project == p && c.Baseline == b {
			out = append(out, c)
		}
	}

	return out, nil
}

func (s *queuedStore) SaveObservation(_ context.Context, c domain.Composition) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	old := s.records[c.ID]
	if old.Generation != c.Generation || old.DeletionRequested != c.DeletionRequested {
		return domain.ErrStaleObservation
	}

	s.records[c.ID] = clone(c)
	return nil
}
func (s *queuedStore) Expire(context.Context, time.Time) error { return nil }
func (s *queuedStore) ListenChanges(ctx context.Context, f func(string)) {
	for {
		select {
		case <-ctx.Done():
			return
		case id := <-s.messages:
			f(id)
		}
	}
}

type queueRuntime struct {
	memoryRuntime
	entered chan struct{}
	once    sync.Once
}

func (m *queueRuntime) Ensure(ctx context.Context, s domain.WorkloadSpec) (domain.WorkloadRef, error) {
	if s.CompositionID == "slow" {
		m.once.Do(func() { close(m.entered) })
		<-ctx.Done()
		return domain.WorkloadRef{}, ctx.Err()
	}

	return domain.WorkloadRef{Namespace: domain.NamespaceForID(s.CompositionID), Deployment: s.ComponentID, Service: s.ComponentID, OwnershipToken: s.OwnershipToken}, nil
}

func TestQueueIndependentDomainsAndShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	slow := fixture(time.Now())
	slow.ID = "slow"
	slow.Baseline = "slow"
	slow.Runtime.Plan.Baseline.ID = "slow"
	fast := fixture(time.Now())
	fast.ID = "fast"
	fast.Baseline = "fast"
	fast.Runtime.Plan.Baseline.ID = "fast"
	slow.Endpoints["public"] = domain.Endpoint{URL: "http://slow.envy.localhost"}
	fast.Endpoints["public"] = domain.Endpoint{URL: "http://fast.envy.localhost"}
	store := &queuedStore{records: map[string]domain.Composition{"slow": slow, "fast": fast}, messages: make(chan string, 10)}
	runtime := &queueRuntime{memoryRuntime: memoryRuntime{ready: true}, entered: make(chan struct{})}
	r := New(store, runtime, &memoryRoutes{}, &memoryVerifier{}, func(context.Context) error { return ctx.Err() }, nil, Config{Interval: 10 * time.Millisecond})
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()
	select {
	case <-runtime.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("slow domain did not start")
	}

	deadline := time.Now().Add(3 * time.Second)
	for {
		c, _ := store.Get(ctx, "fast")
		if c.Phase == domain.PhaseReady {
			break
		}

		if time.Now().After(deadline) {
			t.Fatalf("fast domain blocked: %+v", c.LastError)
		}

		time.Sleep(10 * time.Millisecond)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("workers did not stop")
	}
}

func TestQueueObservationDoesNotBypassFailureBackoff(t *testing.T) {
	r, store, runtime, _, _, now := setup(t)
	c := store.records["a"]
	c.Runtime.Attempts = 2
	c.Runtime.NextAttemptAt = now.Add(time.Minute)
	if err := r.reconcile(context.Background(), &c, true); err != nil {
		t.Fatal(err)
	}

	if len(runtime.created) != 0 {
		t.Fatal("event bypassed persisted failure backoff")
	}

	c.Generation++
	c.Runtime.Attempts = 0
	c.Runtime.NextAttemptAt = time.Time{}
	if err := r.reconcile(context.Background(), &c, true); err != nil {
		t.Fatal(err)
	}

	if len(runtime.created) == 0 {
		t.Fatal("new intent not processed")
	}
}

func TestQueueRecoveryFindsUnnotifiedIntent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := &queuedStore{records: map[string]domain.Composition{}, messages: make(chan string)}
	started := make(chan struct{})
	runtime := &queueRuntime{memoryRuntime: memoryRuntime{ready: true}, entered: make(chan struct{})}
	r := New(store, runtime, &memoryRoutes{}, &memoryVerifier{}, func(context.Context) error { return ctx.Err() }, nil, Config{Interval: 10 * time.Millisecond, StartWatch: func(context.Context, func(string)) error { close(started); return nil }})
	r.recoveryInterval = 30 * time.Millisecond
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()
	<-started
	c := fixture(time.Now())
	c.ID = "unnotified"
	store.mu.Lock()
	store.records[c.ID] = c
	store.mu.Unlock()
	deadline := time.Now().Add(2 * time.Second)
	for {
		c, _ = store.Get(ctx, c.ID)
		if c.Phase == domain.PhaseReady {
			break
		}

		if time.Now().After(deadline) {
			t.Fatal("missed notification was not recovered")
		}

		time.Sleep(10 * time.Millisecond)
	}

	cancel()
	<-done
}
