package postgres

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/dblooman/envy/internal/application"
	"github.com/dblooman/envy/internal/domain"
)

type messagingValidator struct{ domain.MessagingProvider }

func (messagingValidator) Validate(context.Context, domain.Baseline) error { return nil }
func TestPersistIsolationRecipeAndAttachConsumer(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	b, err := s.Baseline(ctx, "demo", "staging")
	if err != nil {
		t.Fatal(err)
	}

	b.PubSub = map[string]domain.PubSubTopic{"events": {Topic: "projects/test-project/topics/events", Publishers: []string{"service-b"}, Consumers: map[string]domain.PubSubConsumer{"worker": {Subscription: "projects/test-project/subscriptions/worker", Component: "service-a", SubscriptionEnv: "EVENTS_SUB"}}}}
	body, _ := json.Marshal(b)
	// Fixture-only onboarding; broker validation is exercised by provider tests.
	if _, err = s.pool.Exec(ctx, `UPDATE baselines SET body=$1 WHERE project='demo' AND id='staging'`, body); err != nil {
		t.Fatal(err)
	}

	app := application.New(s, application.Config{Messaging: messagingValidator{}, Installation: "test"})
	req := request("isolated")
	req.MessageIsolation = true
	req.Overrides["service-b"] = domain.ComponentOverride{Image: "registry.example.com/team/producer@sha256:" + strings.Repeat("a", 64)}
	c, err := app.Create(ctx, req, "isolation")
	if err != nil {
		t.Fatal(err)
	}

	original := append([]domain.MessageSubscription(nil), c.Runtime.Plan.MessageSubscriptions...)
	loaded, err := s.Get(ctx, c.ID)
	if err != nil || !loaded.MessageIsolation || !reflect.DeepEqual(loaded.Runtime.Plan.MessageSubscriptions, original) {
		t.Fatal("isolation snapshot lost across storage")
	}

	replay, err := app.Create(ctx, req, "isolation")
	if err != nil || replay.ID != c.ID {
		t.Fatal("isolation replay failed")
	}

	off := req
	off.MessageIsolation = false
	if _, err = app.Create(ctx, off, "isolation"); err == nil {
		t.Fatal("idempotency key accepted changed isolation")
	}

	recipe, err := app.ExportRecipe(ctx, application.ExportRecipeRequest{Composition: c.ID})
	if err != nil || !recipe.MessageIsolation {
		t.Fatalf("recipe export: %v", err)
	}

	recreated, err := app.RecreateRecipe(ctx, application.RecreateRecipeRequest{Recipe: recipe, Name: "recreated", IdempotencyKey: "new-preview"})
	if err != nil || !recreated.Composition.MessageIsolation || recreated.Composition.MessageSubscriptions[0].Name == original[0].Name {
		t.Fatalf("recipe recreation: %v", err)
	}

	c.Phase = domain.PhaseReady
	c.ObservedGeneration = c.Generation
	c.LatestOperation.Status = "succeeded"
	c.MessageSubscriptions[0].Ready = true
	c.MessageSubscriptions[0].BacklogMayBeLost = true
	c.Runtime.MessagingObserved = map[string]bool{original[0].Name: true}
	if err = s.SaveObservation(ctx, c); err != nil {
		t.Fatal(err)
	}

	overrides := map[string]domain.ComponentOverride{"service-b": req.Overrides["service-b"], "service-a": {Image: "registry.example.com/team/worker@sha256:" + strings.Repeat("b", 64)}}
	attached, err := app.Update(ctx, c.ID, domain.UpdateRequest{ExpectedGeneration: 1, Overrides: overrides})
	if err != nil {
		t.Fatal(err)
	}

	if attached.Generation != 2 || !attached.MessageIsolation || !reflect.DeepEqual(attached.Runtime.Plan.MessageSubscriptions, original) || !attached.MessageSubscriptions[0].BacklogMayBeLost || !attached.Runtime.MessagingObserved[original[0].Name] {
		t.Fatal("attachment changed isolation, subscriptions or observations")
	}

	if _, err = app.Update(ctx, c.ID, domain.UpdateRequest{ExpectedGeneration: 1, Overrides: overrides}); err == nil {
		t.Fatal("stale consumer attachment accepted")
	}

	attached.Phase = domain.PhaseReady
	attached.ObservedGeneration = 2
	attached.LatestOperation.Status = "succeeded"
	if err = s.SaveObservation(ctx, attached); err != nil {
		t.Fatal(err)
	}

	removed, err := app.Update(ctx, c.ID, domain.UpdateRequest{ExpectedGeneration: 2, Overrides: req.Overrides})
	if err != nil || !reflect.DeepEqual(removed.Runtime.Plan.MessageSubscriptions, original) {
		t.Fatalf("consumer removal lost subscription: %v", err)
	}
}
