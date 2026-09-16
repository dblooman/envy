package postgres

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/dblooman/envy/internal/application"
	"github.com/dblooman/envy/internal/domain"
)

func persistedPreview(t *testing.T) (*Store, *application.Service, domain.PRPreview) {
	t.Helper()
	s := testStore(t)
	ctx := context.Background()
	app := application.New(s, application.Config{SourceControl: sourceChecks{}, ImageRegistry: registryChecks{}})
	r := domain.SourceRepository{Project: "demo", ID: "backend", Enabled: true, InstallationID: 42, GitHubRepository: "acme/backend", Images: map[string]string{"service-b": "registry.example.com/service-b"}}
	if _, e := app.RegisterSourceRepository(ctx, r); e != nil {
		t.Fatal(e)
	}

	policy := domain.PRPreviewPolicy{Project: "demo", Repository: "backend", GitHubRepositoryID: 100, Enabled: true, Baseline: "staging", Components: []string{"service-b"}, TTL: "1h", WorkflowID: 123}
	if e := s.SavePreviewPolicy(ctx, policy); e != nil {
		t.Fatal(e)
	}

	p, e := s.SavePRPreview(ctx, domain.PRPreview{ID: "preview-1", Number: 7, Policy: policy, Lifecycle: 1, Requested: true, Status: "waiting_build"})
	if e != nil {
		t.Fatal(e)
	}

	return s, app, p
}

func TestPRPreviewAtomicOwnershipAndStopFence(t *testing.T) {
	s, app, p := persistedPreview(t)
	ctx := context.Background()
	claim := domain.WithPRPreviewClaim(ctx, p)
	c, e := app.Create(claim, request("owned"), "owned-key")
	if e != nil {
		t.Fatal(e)
	}

	loaded, e := s.PRPreview(ctx, p.ID)
	if e != nil || loaded.CompositionID != c.ID || c.PRPreviewID != p.ID {
		t.Fatalf("ownership not atomic: %+v %v", loaded, e)
	}

	replay, e := app.Create(claim, request("owned"), "owned-key")
	if e != nil || replay.ID != c.ID {
		t.Fatalf("retry created another environment: %v", e)
	}

	_, e = app.Create(claim, request("duplicate"), "another-key")
	checkCode(t, e, "conflict")
	_, e = s.Update(ctx, c.ID, domain.UpdateRequest{ExpectedGeneration: 1, Overrides: c.Overrides}, "manual-update")
	checkCode(t, e, "conflict")
	if _, e = app.Destroy(ctx, c.ID); e != nil {
		t.Fatal(e)
	}

	loaded, e = s.PRPreview(ctx, p.ID)
	if e != nil || !loaded.Terminal || loaded.Status != "stopped" {
		t.Fatal("manual deletion did not persist stop", e)
	}

	_, e = app.Create(claim, request("late"), "late-key")
	checkCode(t, e, "conflict")
	_, e = s.Update(claim, c.ID, domain.UpdateRequest{ExpectedGeneration: 1, Overrides: c.Overrides}, "late-update")
	checkCode(t, e, "conflict")
	if _, e = app.Create(ctx, request("independent"), "independent-key"); e != nil {
		t.Fatal("independent create affected", e)
	}
}

func TestPRPreviewConcurrentCASAndWebhookDedupe(t *testing.T) {
	s, _, p := persistedPreview(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			copy := p
			copy.Terminal = true
			_, e := s.SavePRPreview(ctx, copy)
			results <- e
		}()
	}

	wg.Wait()
	close(results)
	wins := 0
	for e := range results {
		if e == nil {
			wins++
		} else {
			var de *domain.Error
			if !errors.As(e, &de) || de.Code != "conflict" {
				t.Fatal(e)
			}
		}
	}

	if wins != 1 {
		t.Fatalf("expected one CAS winner, got %d", wins)
	}

	for range 2 {
		if e := s.ReceiveGitHubWebhook(ctx, "delivery", "pull_request", []byte(`{"action":"opened"}`)); e != nil {
			t.Fatal(e)
		}
	}

	events, e := s.PendingGitHubEvents(ctx)
	if e != nil || len(events) != 1 {
		t.Fatal("delivery dedupe", e)
	}

	if e = s.FinishGitHubEvent(ctx, "delivery", ""); e != nil {
		t.Fatal(e)
	}

	events, e = s.PendingGitHubEvents(ctx)
	if e != nil || len(events) != 0 {
		t.Fatal("delivery not completed", e)
	}
}

func TestPRPreviewPolicyUniqueEnabledRepository(t *testing.T) {
	s, _, p := persistedPreview(t)
	ctx := context.Background()
	other := p.Policy
	other.Repository = "other"
	r := domain.SourceRepository{Project: "demo", ID: "other", Enabled: true, InstallationID: 42, GitHubRepository: "acme/other", Images: map[string]string{"service-a": "registry.example.com/service-a"}}
	if _, e := s.RegisterSourceRepository(ctx, r); e != nil {
		t.Fatal(e)
	}

	if e := s.SavePreviewPolicy(ctx, other); e == nil {
		t.Fatal("duplicate enabled owner accepted")
	}

	other.Enabled = false
	if e := s.SavePreviewPolicy(ctx, other); e != nil {
		t.Fatal(e)
	}
}
