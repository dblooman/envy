package postgres

import (
	"context"
	"github.com/dblooman/envy/internal/domain"
	"sync"
	"testing"
)

func TestPreviewApprovalIsVersionedAuditedAndSerialized(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	profile := domain.PreviewProfile{Project: "demo", Baseline: "staging", Component: "service-b", SourceUID: "deployment", Contract: "contract"}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Go(func() { _, err := s.ApprovePreview(ctx, profile, 0); results <- err })
	}
	wg.Wait()
	close(results)
	success, conflicts := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else {
			checkCode(t, err, "conflict")
			conflicts++
		}
	}
	if success != 1 || conflicts != 1 {
		t.Fatal("concurrent first approvals were not serialized")
	}
	latest, err := s.PreviewProfile(ctx, "demo", "staging", "service-b")
	if err != nil || latest.Revision != 1 {
		t.Fatal(err)
	}
	profile.Contract = "new-contract"
	if _, err = s.ApprovePreview(ctx, profile, 1); err != nil {
		t.Fatal(err)
	}
	var revisions, events int
	if err = s.pool.QueryRow(ctx, `SELECT count(*) FROM preview_profiles`).Scan(&revisions); err != nil {
		t.Fatal(err)
	}
	if err = s.pool.QueryRow(ctx, `SELECT count(*) FROM activity_events WHERE action='catalog.preview.approve'`).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if revisions != 2 || events != 2 {
		t.Fatalf("revisions %d events %d", revisions, events)
	}
}
