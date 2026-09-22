package postgres

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dblooman/envy/internal/application"
	"github.com/dblooman/envy/internal/domain"
)

type sourceChecks struct{}

func (sourceChecks) Check(context.Context, domain.SourceRepository) error { return nil }
func (sourceChecks) Resolve(_ context.Context, _ domain.SourceRepository, ref string) (domain.GitCommit, error) {
	return domain.GitCommit{SHA: ref}, nil
}

func (sourceChecks) Branches(context.Context, domain.SourceRepository, int) ([]domain.GitBranch, error) {
	return nil, nil
}

func (sourceChecks) Commits(context.Context, domain.SourceRepository, string, int) ([]domain.GitCommit, error) {
	return nil, nil
}

type registryChecks struct{}

func (registryChecks) Check(context.Context, string) error { return nil }
func TestBuildCatalogPersistenceAndPinnedComposition(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	app := application.New(s, application.Config{SourceControl: sourceChecks{}, ImageRegistry: registryChecks{}})
	r := domain.SourceRepository{Project: "demo", ID: "backend", Enabled: true, InstallationID: 42, GitHubRepository: "acme/backend", Images: map[string]string{"service-b": "registry.example.com/service-b", "service-a": "registry.example.com/service-a"}}
	if _, err := app.RegisterSourceRepository(ctx, r); err != nil {
		t.Fatal(err)
	}

	if _, err := app.RegisterSourceRepository(ctx, r); err != nil {
		t.Fatal("registration retry", err)
	}

	report := domain.BuildReport{Component: "service-b", Revision: strings.Repeat("a", 40), Image: "registry.example.com/service-b@sha256:" + strings.Repeat("b", 64), RunID: "123", Attempt: 1, BuiltAt: time.Now().UTC()}
	var wg sync.WaitGroup
	errs := make(chan error, 5)
	for range 5 {
		wg.Go(func() { _, err := app.RecordBuild(ctx, "demo", "backend", report); errs <- err })
	}

	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal("concurrent retry", err)
		}
	}

	resolved, err := app.ResolveRevision(ctx, "demo", "backend", "service-b", report.Revision, "", 20)
	if err != nil || len(resolved.Builds) != 1 {
		t.Fatalf("lookup %+v %v", resolved, err)
	}

	first := resolved.Builds[0]
	bad := report
	bad.Image = "registry.example.com/service-b@sha256:" + strings.Repeat("c", 64)
	_, err = app.RecordBuild(ctx, "demo", "backend", bad)
	checkCode(t, err, "conflict")
	report.Attempt = 2
	report.Image = bad.Image
	second, err := app.RecordBuild(ctx, "demo", "backend", report)
	if err != nil {
		t.Fatal(err)
	}

	items, next, err := s.Builds(ctx, "demo", "backend", "service-b", report.Revision, "", 1)
	if err != nil || len(items) != 1 || next == "" {
		t.Fatal("pagination first page")
	}

	items, next, err = s.Builds(ctx, "demo", "backend", "service-b", report.Revision, next, 1)
	if err != nil || len(items) != 1 || next != "" {
		t.Fatal("pagination next page")
	}

	req := request("pinned")
	req.Overrides["service-b"] = domain.ComponentOverride{BuildID: first.ID}
	c, err := app.Create(ctx, req, "build-idempotency")
	if err != nil {
		t.Fatal(err)
	}

	again, err := app.Create(ctx, req, "build-idempotency")
	if err != nil || again.ID != c.ID {
		t.Fatal("composition retry", err)
	}

	got, err := s.Get(ctx, c.ID)
	if err != nil || got.Overrides["service-b"].Source.Revision != report.Revision || got.Overrides["service-b"].Image != first.Image {
		t.Fatal("provenance not persisted")
	}

	// Use a failed phase as a legal update starting state without a Kubernetes cluster.
	if _, err = s.pool.Exec(ctx, "UPDATE compositions SET phase='failed', body=jsonb_set(body,'{phase}','\"failed\"') WHERE id=$1", c.ID); err != nil {
		t.Fatal(err)
	}

	update := domain.UpdateRequest{ExpectedGeneration: 1, Overrides: map[string]domain.ComponentOverride{"service-b": {BuildID: second.ID}}}
	updated, err := app.Update(ctx, c.ID, update)
	if err != nil {
		t.Fatal(err)
	}

	if updated.Overrides["service-b"].Image != second.Image || updated.Generation != 2 || updated.Endpoints["public"].URL != c.Endpoints["public"].URL || !updated.ExpiresAt.Equal(c.ExpiresAt) {
		t.Fatal("update changed identity or lost artifact")
	}

	_, err = app.Update(ctx, c.ID, update)
	checkCode(t, err, "conflict")
	if _, err = app.EnableSourceRepository(ctx, "demo", "backend", false); err != nil {
		t.Fatal(err)
	}

	req.Name = "disabled"
	_, err = app.Create(ctx, req, "")
	checkCode(t, err, "conflict")
	// Persistence independently enforces the disable even with an already resolved plan.
	got.ID = "new-disabled-composition"
	_, err = s.Create(ctx, got, "", "", 20)
	checkCode(t, err, "conflict")
	if _, err = s.Get(ctx, c.ID); err != nil {
		t.Fatal("disable removed existing composition")
	}

	_, err = s.RecordBuild(ctx, first)
	checkCode(t, err, "conflict")
	r.ID = "other"
	r.GitHubRepository = "acme/other"
	_, err = app.RegisterSourceRepository(ctx, r)
	checkCode(t, err, "conflict")
	if _, err = s.SourceRepository(ctx, "demo", "other"); err == nil {
		t.Fatal("failed component claim leaked repository")
	}
}
