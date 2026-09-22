package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dblooman/envy/internal/application"
	"github.com/dblooman/envy/internal/domain"
)

func TestVerificationAtomicCoalescingAndRetention(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	app := application.New(s, application.Config{})
	c, err := app.Create(ctx, request("evidence"), "evidence-key")
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC().Truncate(time.Microsecond)
	c.Phase = domain.PhaseReady
	c.LatestOperation.Status = "succeeded"
	c.PendingVerification = &domain.VerificationEvidence{Composition: c.ID, Generation: c.Generation, Kind: "http", Outcome: "passed", FirstCheckedAt: now, LastCheckedAt: now, Probes: []domain.VerificationProbe{{Target: "preview", ExpectedStatus: 200, ObservedStatus: 200}}, Hops: []domain.VerificationHop{}}
	if err = s.SaveObservation(ctx, c); err != nil {
		t.Fatal(err)
	}

	c.PendingVerification.LastCheckedAt = now.Add(time.Minute)
	if err = s.SaveObservation(ctx, c); err != nil {
		t.Fatal(err)
	}

	page, err := s.Verification(ctx, c.ID, "", 100)
	if err != nil || len(page.Items) != 1 || !page.Items[0].FirstCheckedAt.Equal(now) || !page.Items[0].LastCheckedAt.Equal(now.Add(time.Minute)) {
		t.Fatalf("coalescing: %+v %v", page, err)
	}

	c.PendingVerification.Outcome = "failed"
	c.PendingVerification.Error = &domain.Error{Code: "verification_failed", Message: "HTTP 502"}
	if err = s.SaveObservation(ctx, c); err != nil {
		t.Fatal(err)
	}

	page, err = s.Verification(ctx, c.ID, "", 1)
	if err != nil || len(page.Items) != 1 || page.NextCursor == "" || page.Items[0].Outcome != "failed" {
		t.Fatalf("failure transition: %+v %v", page, err)
	}

	older, err := s.Verification(ctx, c.ID, page.NextCursor, 1)
	if err != nil || len(older.Items) != 1 || older.Items[0].Outcome != "passed" {
		t.Fatalf("pagination: %+v %v", older, err)
	}

	if _, err = app.Update(ctx, c.ID, domain.UpdateRequest{ExpectedGeneration: 1, Overrides: map[string]domain.ComponentOverride{"service-b": {Image: "v3"}}}); err != nil {
		t.Fatal(err)
	}

	c.PendingVerification.Outcome = "passed"
	if err = s.SaveObservation(ctx, c); !errors.Is(err, domain.ErrStaleObservation) {
		t.Fatal("stale evidence accepted", err)
	}

	destroyed, err := app.Destroy(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}

	destroyed.Phase = domain.PhaseDestroyed
	if err = s.SaveObservation(ctx, destroyed); err != nil {
		t.Fatal(err)
	}

	page, err = s.Verification(ctx, c.ID, "", 100)
	if err != nil || len(page.Items) != 2 || page.Items[0].Outcome != "failed" {
		t.Fatalf("retention/stale write: %+v %v", page, err)
	}
}
