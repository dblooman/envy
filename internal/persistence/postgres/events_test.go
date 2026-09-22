package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dblooman/envy/internal/application"
	"github.com/dblooman/envy/internal/domain"
)

func TestLifecycleEventsAtomicOrderedAndDeduplicated(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	app := application.New(s, application.Config{})
	c, err := app.Create(ctx, request("events"), "event-key")
	if err != nil {
		t.Fatal(err)
	}

	if _, err = app.Create(ctx, request("events"), "event-key"); err != nil {
		t.Fatal(err)
	}

	page, err := s.Events(ctx, c.ID, "", 100)
	if err != nil || len(page.Items) != 1 || page.Items[0].Type != "create_requested" {
		t.Fatalf("idempotent create events %+v: %v", page, err)
	}

	c.Phase = domain.PhaseReady
	c.ObservedGeneration = 1
	c.LatestOperation.Status = "succeeded"
	if err = s.SaveObservation(ctx, c); err != nil {
		t.Fatal(err)
	}

	for range 3 {
		c.Runtime.Attempts++
		c.Runtime.NextAttemptAt = time.Now()
		if err = s.SaveObservation(ctx, c); err != nil {
			t.Fatal(err)
		}
	}

	page, err = s.Events(ctx, c.ID, "", 1)
	if err != nil || len(page.Items) != 1 || page.NextCursor == "" {
		t.Fatalf("pagination %+v: %v", page, err)
	}

	next, err := s.Events(ctx, c.ID, page.NextCursor, 1)
	if err != nil || len(next.Items) != 1 || next.NextCursor != "" || next.Items[0].Phase != domain.PhaseReady {
		t.Fatalf("duplicate polling events %+v: %v", next, err)
	}

	updated, err := app.Update(ctx, c.ID, domain.UpdateRequest{ExpectedGeneration: 1, Overrides: map[string]domain.ComponentOverride{"service-b": {Image: "v3"}}})
	if err != nil {
		t.Fatal(err)
	}

	if err = s.SaveObservation(ctx, c); !errors.Is(err, domain.ErrStaleObservation) {
		t.Fatal("stale write accepted")
	}

	deleted, err := app.Destroy(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}

	if _, err = app.Destroy(ctx, c.ID); err != nil {
		t.Fatal(err)
	}

	deleted.Phase = domain.PhaseDestroyed
	deleted.LatestOperation.Status = "succeeded"
	if err = s.SaveObservation(ctx, deleted); err != nil {
		t.Fatal(err)
	}

	page, err = s.Events(ctx, c.ID, "", 100)
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"create_requested", "observation_changed", "update_requested", "destroy_requested", "observation_changed"}
	if len(page.Items) != len(want) {
		t.Fatalf("unexpected events %+v", page)
	}

	var previous int64
	for i, event := range page.Items {
		n, err := domain.EventCursor(event.ID)
		if err != nil || n <= previous || event.Type != want[i] || event.Composition != c.ID || event.Project != "demo" || event.OccurredAt.IsZero() {
			t.Fatalf("bad event %+v", event)
		}

		previous = n
	}

	if page.Items[2].Operation.ID != updated.LatestOperation.ID {
		t.Fatal("event lost operation")
	}

	// A trigger insertion and state mutation must roll back together.
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}

	_, err = tx.Exec(ctx, "UPDATE compositions SET body=jsonb_set(body,'{phase}','\"failed\"') WHERE id=$1", c.ID)
	if err != nil {
		t.Fatal(err)
	}

	tx.Rollback(ctx)
	stable, err := s.Events(ctx, c.ID, "", 100)
	if err != nil || len(stable.Items) != len(want) {
		t.Fatal("rolled-back event leaked")
	}

	if _, err = s.Events(ctx, "unknown", "", 20); err == nil {
		t.Fatal("unknown composition returned history")
	}
}

func TestExpiryEventAndExistingSnapshotMigration(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	app := application.New(s, application.Config{})
	c, err := app.Create(ctx, request("expiry-events"), "")
	if err != nil {
		t.Fatal(err)
	}

	if err = s.Expire(ctx, c.ExpiresAt.Add(time.Second)); err != nil {
		t.Fatal(err)
	}

	page, err := s.Events(ctx, c.ID, "", 100)
	if err != nil || len(page.Items) != 2 || page.Items[1].Type != "expired" {
		t.Fatalf("expiry not distinguished %+v %v", page, err)
	}

	// Recreate the state immediately before migration 002, then verify backfill
	// records only an honest current snapshot and is repeatable.
	_, err = s.pool.Exec(ctx, "DROP TRIGGER envy_composition_events ON compositions; DROP FUNCTION envy_record_lifecycle_event(); DROP FUNCTION envy_event_state(jsonb); DROP TABLE lifecycle_events; DELETE FROM envy_schema_migrations WHERE name='002_lifecycle_events.sql'")
	if err != nil {
		t.Fatal(err)
	}

	if err = s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	if err = s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	page, err = s.Events(ctx, c.ID, "", 100)
	if err != nil || len(page.Items) != 1 || page.Items[0].Type != "snapshot" || page.Items[0].Phase != domain.PhaseDestroying {
		t.Fatalf("migration fabricated history %+v %v", page, err)
	}
}
