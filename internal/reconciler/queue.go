package reconciler

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/dblooman/envy/internal/domain"
	"k8s.io/client-go/util/workqueue"
)

type eventStore interface {
	Store
	Get(context.Context, string) (domain.Composition, error)
	ActiveDomain(context.Context, string, string) ([]domain.Composition, error)
	ListenChanges(context.Context, func(string))
}
type (
	routingKey   struct{ Project, Baseline string }
	queueSession struct {
		ctx         context.Context
		reconciler  *Reconciler
		store       eventStore
		queue       workqueue.TypedRateLimitingInterface[routingKey]
		mu          sync.RWMutex
		byNamespace map[string]map[routingKey]bool
	}
)

func (r *Reconciler) runQueue(parent context.Context, store eventStore) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	if r.guard == nil {
		return fmt.Errorf("reconciler requires a leadership guard")
	}

	if err := r.guard(ctx); err != nil {
		return err
	}

	if err := store.Expire(ctx, r.now()); err != nil {
		return err
	}

	session := &queueSession{ctx: ctx, reconciler: r, store: store, queue: workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[routingKey]()), byNamespace: map[string]map[routingKey]bool{}}
	defer session.queue.ShutDown()
	if err := session.sweep(); err != nil {
		return err
	}

	if r.cfg.StartWatch != nil {
		if err := r.cfg.StartWatch(ctx, session.namespaceChanged); err != nil {
			return err
		}
	}

	// Expiration may have advanced while the informer caches synchronized.
	if err := store.Expire(ctx, r.now()); err != nil {
		return err
	}

	var wg sync.WaitGroup
	wg.Go(func() { store.ListenChanges(ctx, session.desiredChanged) })
	for range 4 {
		wg.Go(session.work)
	}

	defer func() { cancel(); session.queue.ShutDown(); wg.Wait() }()
	return session.timers()
}

func (s *queueSession) index(c domain.Composition) {
	key := routingKey{c.Project, c.Baseline}
	namespaces := []string{domain.NamespaceForID(c.ID)}
	if c.Runtime.Plan != nil {
		namespaces = append(namespaces, c.Runtime.Plan.Baseline.Routing.Namespace, c.Runtime.Plan.Baseline.Routing.GatewayNamespace)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for _, ns := range namespaces {
		if s.byNamespace[ns] == nil {
			s.byNamespace[ns] = map[routingKey]bool{}
		}

		s.byNamespace[ns][key] = true
	}
}

func (s *queueSession) sweep() error {
	all, err := s.store.Active(s.ctx)
	if err != nil {
		return err
	}

	s.mu.Lock()
	s.byNamespace = map[string]map[routingKey]bool{}
	s.mu.Unlock()
	for _, c := range all {
		s.index(c)
		s.queue.Add(routingKey{c.Project, c.Baseline})
	}

	return nil
}

func (s *queueSession) namespaceChanged(namespace string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if namespace != "" {
		for key := range s.byNamespace[namespace] {
			s.queue.Add(key)
		}

		return
	}

	for _, keys := range s.byNamespace {
		for key := range keys {
			s.queue.Add(key)
		}
	}
}

func (s *queueSession) desiredChanged(id string) {
	if id == "" {
		if err := s.sweep(); err != nil {
			s.reconciler.log.Warn("recovery scan failed", "error", err)
		}

		return
	}

	c, err := s.store.Get(s.ctx, id)
	if err != nil {
		return
	}

	s.index(c)
	s.queue.Add(routingKey{c.Project, c.Baseline})
}

func (s *queueSession) work() {
	worker := *s.reconciler
	worker.routeCycle = nil
	for {
		key, shutdown := s.queue.Get()
		if shutdown {
			return
		}

		s.process(&worker, key)
	}
}

func (s *queueSession) process(worker *Reconciler, key routingKey) {
	defer s.queue.Done(key)
	if err := s.reconcileDomain(worker, key); err != nil {
		worker.log.Warn("routing domain reconcile failed", "project", key.Project, "baseline", key.Baseline, "error", err)
		s.queue.AddRateLimited(key)
		return
	}

	s.queue.Forget(key)
}

func (s *queueSession) reconcileDomain(worker *Reconciler, key routingKey) error {
	rows, err := s.store.ActiveDomain(s.ctx, key.Project, key.Baseline)
	if err != nil {
		return err
	}

	for i := range rows {
		if err := worker.reconcile(s.ctx, &rows[i], true); err != nil {
			return err
		}
	}

	// Read committed backoff and concurrent intent, not the worker's old snapshot.
	rows, err = s.store.ActiveDomain(s.ctx, key.Project, key.Baseline)
	if err != nil {
		return err
	}

	delay := 30 * time.Second
	for _, c := range rows {
		d := max(time.Until(c.Runtime.NextAttemptAt), worker.cfg.Interval)

		if d < delay {
			delay = d
		}
	}

	if len(rows) > 0 {
		s.queue.AddAfter(key, delay)
	}

	return nil
}

func (s *queueSession) timers() error {
	r := s.reconciler
	recovery := time.NewTicker(r.recoveryInterval)
	defer recovery.Stop()
	expiry := time.NewTicker(time.Second)
	defer expiry.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return s.ctx.Err()
		case <-recovery.C:
			if err := s.sweep(); err != nil {
				return err
			}
		case <-expiry.C:
			if err := r.guard(s.ctx); err != nil {
				return err
			}

			if err := s.store.Expire(s.ctx, r.now()); err != nil {
				return err
			}
		}
	}
}
