package application

import (
	"context"
	"errors"
	"github.com/dblooman/envy/internal/domain"
	"strings"
	"testing"
	"time"
)

type sourceFake struct {
	denied bool
	head   string
}

func (f *sourceFake) Check(context.Context, domain.SourceRepository) error {
	if f.denied {
		return domain.NotFound("installation access removed")
	}

	return nil
}
func (f *sourceFake) Resolve(_ context.Context, _ domain.SourceRepository, ref string) (domain.GitCommit, error) {
	if f.denied || ref == "missing" {
		return domain.GitCommit{}, domain.NotFound("commit missing")
	}

	if ref == "main" {
		ref = f.head
	}

	return domain.GitCommit{SHA: ref}, nil
}
func (f *sourceFake) Branches(context.Context, domain.SourceRepository, int) ([]domain.GitBranch, error) {
	return []domain.GitBranch{{Name: "main", SHA: f.head}}, nil
}
func (f *sourceFake) Commits(context.Context, domain.SourceRepository, string, int) ([]domain.GitCommit, error) {
	return []domain.GitCommit{{SHA: f.head}}, nil
}

type registryFake struct {
	err    error
	images []string
}

func (f *registryFake) Check(_ context.Context, image string) error {
	f.images = append(f.images, image)
	return f.err
}

type buildsFake struct {
	createRepository
	repo   domain.SourceRepository
	builds map[string]domain.Build
}

func (f *buildsFake) SourceRepository(context.Context, string, string) (domain.SourceRepository, error) {
	return f.repo, nil
}
func (f *buildsFake) SourceRepositories(context.Context, string, string, int) ([]domain.SourceRepository, string, error) {
	return []domain.SourceRepository{f.repo}, "", nil
}
func (f *buildsFake) RegisterSourceRepository(_ context.Context, r domain.SourceRepository) (domain.SourceRepository, error) {
	f.repo = r
	return r, nil
}
func (f *buildsFake) EnableSourceRepository(_ context.Context, _, _ string, enabled bool) (domain.SourceRepository, error) {
	f.repo.Enabled = enabled
	return f.repo, nil
}
func (f *buildsFake) RecordBuild(_ context.Context, b domain.Build) (domain.Build, error) {
	f.builds[b.ID] = b
	return b, nil
}
func (f *buildsFake) Build(_ context.Context, project, id string) (domain.Build, error) {
	b, ok := f.builds[id]
	if !ok || b.Project != project {
		return b, domain.NotFound("build")
	}

	return b, nil
}
func (f *buildsFake) Builds(_ context.Context, _, _, component, revision, _ string, _ int) ([]domain.Build, string, error) {
	out := []domain.Build{}
	for _, b := range f.builds {
		if b.Component == component && b.Revision == revision {
			out = append(out, b)
		}
	}

	return out, "", nil
}
func buildFixture() (*Service, *buildsFake, *sourceFake, *registryFake, domain.BuildReport) {
	f := &buildsFake{repo: domain.SourceRepository{Project: "demo", ID: "backend", Enabled: true, GitHubRepository: "acme/backend", InstallationID: 42, Images: map[string]string{"service-b": "registry.example.com/team/service-b", "service-a": "registry.example.com/team/service-a"}}, builds: map[string]domain.Build{}}
	gh := &sourceFake{head: strings.Repeat("a", 40)}
	reg := &registryFake{}
	report := domain.BuildReport{Component: "service-b", Revision: gh.head, Image: "registry.example.com/team/service-b@sha256:" + strings.Repeat("b", 64), RunID: "123", Attempt: 1, BuiltAt: time.Now().UTC()}
	return New(f, Config{SourceControl: gh, ImageRegistry: reg}), f, gh, reg, report
}
func TestBuildResolutionPinsArtifactAcrossBranchMovement(t *testing.T) {
	s, f, gh, reg, report := buildFixture()
	ctx := context.Background()
	b, err := s.RecordBuild(ctx, "demo", "backend", report)
	if err != nil {
		t.Fatal(err)
	}

	resolved, err := s.ResolveRevision(ctx, "demo", "backend", "service-b", "main", "", 20)
	if err != nil || len(resolved.Builds) != 1 {
		t.Fatalf("lookup %+v %v", resolved, err)
	}

	gh.head = strings.Repeat("c", 40)
	req := validRequest()
	req.Overrides["service-b"] = domain.ComponentOverride{BuildID: b.ID}
	c, err := s.Create(ctx, req, "retry")
	if err != nil {
		t.Fatal(err)
	}

	o := c.Overrides["service-b"]
	if o.Image != report.Image || o.Source == nil || o.Source.Revision != report.Revision || o.BuildID != b.ID {
		t.Fatalf("unpinned %+v", o)
	}

	if len(reg.images) != 2 {
		t.Fatalf("expected report and deployment registry checks: %v", reg.images)
	}

	if f.received.Runtime.Plan.Components["service-b"].ID != "service-b" {
		t.Fatal("approved profile lost")
	}

	historical, err := s.ResolveRevision(ctx, "demo", "backend", "service-b", report.Revision, "", 20)
	if err != nil || len(historical.Builds) != 1 {
		t.Fatal("historical lookup failed")
	}

	current, err := s.ResolveRevision(ctx, "demo", "backend", "service-b", "main", "", 20)
	if err != nil || len(current.Builds) != 0 {
		t.Fatal("no-build commit should resolve without substituting an old build")
	}
}
func TestBuildValidationAndEligibility(t *testing.T) {
	for _, scenario := range []string{"disabled", "app removed", "missing image", "wrong component", "cross project", "both identities", "forged provenance", "unknown build"} {
		t.Run(scenario, func(t *testing.T) {
			s, f, gh, reg, report := buildFixture()
			ctx := context.Background()
			b, err := s.RecordBuild(ctx, "demo", "backend", report)
			if err != nil {
				t.Fatal(err)
			}

			req := validRequest()
			req.Overrides["service-b"] = domain.ComponentOverride{BuildID: b.ID}
			switch scenario {
			case "disabled":
				f.repo.Enabled = false
			case "app removed":
				gh.denied = true
			case "missing image":
				reg.err = errors.New("removed")
			case "wrong component":
				req.Overrides = map[string]domain.ComponentOverride{"service-a": {BuildID: b.ID}}
			case "cross project":
				req.Project = "other"
			case "both identities":
				req.Overrides["service-b"] = domain.ComponentOverride{BuildID: b.ID, Image: report.Image}
			case "forged provenance":
				req.Overrides["service-b"] = domain.ComponentOverride{Image: report.Image, Source: &b}
			case "unknown build":
				req.Overrides["service-b"] = domain.ComponentOverride{BuildID: strings.Repeat("0", 64)}
			}

			if _, err = s.Create(ctx, req, ""); err == nil {
				t.Fatal("invalid build selection accepted")
			}

			if f.received.ID != "" {
				t.Fatal("invalid selection persisted")
			}
		})
	}
}
func TestReportRejectsIncorrectMappingsAndRetainsRebuilds(t *testing.T) {
	s, _, _, _, report := buildFixture()
	ctx := context.Background()
	for _, change := range []func(*domain.BuildReport){func(r *domain.BuildReport) { r.Revision = "abc" }, func(r *domain.BuildReport) { r.Image = "registry.example.com/team/service-b:latest" }, func(r *domain.BuildReport) { r.Image = "evil.example.com/x@sha256:" + strings.Repeat("b", 64) }, func(r *domain.BuildReport) { r.Attempt = 0 }, func(r *domain.BuildReport) { r.BuiltAt = time.Time{} }, func(r *domain.BuildReport) { r.RunID = "../../x" }} {
		bad := report
		change(&bad)
		if _, err := s.RecordBuild(ctx, "demo", "backend", bad); err == nil {
			t.Fatal("invalid report accepted")
		}
	}

	first, err := s.RecordBuild(ctx, "demo", "backend", report)
	if err != nil {
		t.Fatal(err)
	}

	report.Attempt = 2
	report.Image = "registry.example.com/team/service-b@sha256:" + strings.Repeat("d", 64)
	second, err := s.RecordBuild(ctx, "demo", "backend", report)
	if err != nil || second.ID == first.ID {
		t.Fatal("rebuild overwritten")
	}

	out, err := s.ResolveRevision(ctx, "demo", "backend", "service-b", report.Revision, "", 20)
	if err != nil || len(out.Builds) != 2 {
		t.Fatal("rebuild choices lost")
	}
}
