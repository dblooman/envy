package application

import (
	"context"
	"errors"
	"testing"

	"github.com/dblooman/envy/internal/domain"
)

type messagingRepository struct{ createRepository }

func (r *messagingRepository) Baseline(ctx context.Context, p, b string) (domain.Baseline, error) {
	baseline, _ := r.createRepository.Baseline(ctx, p, b)
	baseline.PubSub = map[string]domain.PubSubTopic{"orders": {Topic: "projects/test-project/topics/orders", Publishers: []string{"service-b"}, Consumers: map[string]domain.PubSubConsumer{"worker": {Subscription: "projects/test-project/subscriptions/worker", Component: "service-a", SubscriptionEnv: "ORDERS_SUB"}}}}
	return baseline, nil
}

type messagingCheck struct {
	domain.MessagingProvider
	calls int
	err   error
}

func (m *messagingCheck) Validate(context.Context, domain.Baseline) error { m.calls++; return m.err }

func TestCreateIsolationSnapshotAndHash(t *testing.T) {
	ctx := context.Background()
	repo := &messagingRepository{}
	provider := &messagingCheck{}
	s := New(repo, Config{Messaging: provider, Installation: "test"})
	req := validRequest()
	off, err := s.Create(ctx, req, "off")
	if err != nil {
		t.Fatal(err)
	}

	offHash := repo.hash
	req.MessageIsolation = true
	on, err := s.Create(ctx, req, "on")
	if err != nil {
		t.Fatal(err)
	}

	if off.MessageIsolation || !on.MessageIsolation || offHash == repo.hash || provider.calls != 1 {
		t.Fatal("mode not preserved or hashed")
	}

	if len(on.Overrides) != 1 || len(on.MessageSubscriptions) != 1 || on.MessageSubscriptions[0].Component != "service-a" {
		t.Fatal("subscription required a deployed consumer")
	}

	if len(on.Runtime.Plan.MessageSubscriptions) != 1 {
		t.Fatal("no durable messaging plan")
	}

	provider.err = errors.New("unprotected baseline")
	if _, err = s.Create(ctx, req, "bad"); err == nil {
		t.Fatal("unsafe isolation accepted")
	}

	s.cfg.Messaging = nil
	if _, err = s.Create(ctx, req, "disabled"); err == nil {
		t.Fatal("missing provider accepted")
	}
}
