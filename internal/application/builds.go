package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/dblooman/envy/internal/domain"
)

type BuildStore interface {
	SourceRepository(context.Context, string, string) (domain.SourceRepository, error)
	SourceRepositories(context.Context, string, string, int) ([]domain.SourceRepository, string, error)
	RegisterSourceRepository(context.Context, domain.SourceRepository) (domain.SourceRepository, error)
	EnableSourceRepository(context.Context, string, string, bool) (domain.SourceRepository, error)
	RecordBuild(context.Context, domain.Build) (domain.Build, error)
	Build(context.Context, string, string) (domain.Build, error)
	Builds(context.Context, string, string, string, string, string, int) ([]domain.Build, string, error)
}

var gitRepository = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*/[A-Za-z0-9_.-]+$`)
var commitSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)
var imageLocation = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]*(?::[0-9]+)?/[a-z0-9]+(?:[._-][a-z0-9]+)*(?:/[a-z0-9]+(?:[._-][a-z0-9]+)*)*$`)
var imageDigest = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
var runID = regexp.MustCompile(`^[1-9][0-9]{0,19}$`)

func (s *Service) buildStore() (BuildStore, error) {
	b, ok := s.store.(BuildStore)
	if !ok {
		return nil, &domain.Error{Code: "unavailable", Message: "build catalog is unavailable"}
	}

	return b, nil
}
func (s *Service) sourceReady() error {
	if s.cfg.SourceControl == nil {
		return &domain.Error{Code: "unavailable", Message: "GitHub App is not configured"}
	}

	return nil
}
func (s *Service) SourceRepositories(ctx context.Context, project, after string, limit int) ([]domain.SourceRepository, string, error) {
	if err := ValidatePage(after, limit); err != nil {
		return nil, "", err
	}

	b, err := s.buildStore()
	if err != nil {
		return nil, "", err
	}

	return b.SourceRepositories(ctx, project, after, limit)
}
func (s *Service) RegisterSourceRepository(ctx context.Context, r domain.SourceRepository) (domain.SourceRepository, error) {
	if !domain.ValidCatalogID(r.Project) || !domain.ValidCatalogID(r.ID) || len(r.GitHubRepository) > 200 || !gitRepository.MatchString(r.GitHubRepository) || r.InstallationID < 1 || len(r.Images) < 1 || len(r.Images) > 100 {
		return r, domain.Validation("repository requires project, ID, owner/repo, installation_id, and 1–100 component image mappings")
	}

	r.GitHubRepository = strings.ToLower(r.GitHubRepository)
	for component, image := range r.Images {
		if !domain.ValidCatalogID(component) || len(image) > 400 || !imageLocation.MatchString(image) || !strings.ContainsAny(strings.Split(image, "/")[0], ".:") {
			return r, domain.Validation("image mappings require component IDs and fully qualified registry/image locations without tags")
		}

		c, err := s.store.Component(ctx, r.Project, component)
		if err != nil {
			return r, err
		}

		if !c.Overridable {
			return r, domain.Validation("mapped component must allow overrides")
		}
	}

	if err := s.sourceReady(); err != nil {
		return r, err
	}

	if err := s.cfg.SourceControl.Check(ctx, r); err != nil {
		return r, err
	}

	b, err := s.buildStore()
	if err != nil {
		return r, err
	}

	return b.RegisterSourceRepository(ctx, r)
}
func (s *Service) EnableSourceRepository(ctx context.Context, project, id string, enabled bool) (domain.SourceRepository, error) {
	b, err := s.buildStore()
	if err != nil {
		return domain.SourceRepository{}, err
	}

	if enabled {
		r, e := b.SourceRepository(ctx, project, id)
		if e != nil {
			return r, e
		}

		if e = s.sourceReady(); e != nil {
			return r, e
		}

		if e = s.cfg.SourceControl.Check(ctx, r); e != nil {
			return r, e
		}
	}

	return b.EnableSourceRepository(ctx, project, id, enabled)
}
func (s *Service) eligibleRepository(ctx context.Context, project, id string) (domain.SourceRepository, error) {
	b, err := s.buildStore()
	if err != nil {
		return domain.SourceRepository{}, err
	}

	r, err := b.SourceRepository(ctx, project, id)
	if err != nil {
		return r, err
	}

	if !r.Enabled {
		return r, &domain.Error{Code: "conflict", Message: "repository is disabled"}
	}

	if err = s.sourceReady(); err != nil {
		return r, err
	}

	return r, s.cfg.SourceControl.Check(ctx, r)
}
func (s *Service) SourceBranches(ctx context.Context, project, id string, page int) ([]domain.GitBranch, error) {
	if page < 1 || page > 10000 {
		return nil, domain.Validation("page must be between 1 and 10000")
	}

	r, err := s.eligibleRepository(ctx, project, id)
	if err != nil {
		return nil, err
	}

	return s.cfg.SourceControl.Branches(ctx, r, page)
}
func (s *Service) SourceCommits(ctx context.Context, project, id, branch string, page int) ([]domain.GitCommit, error) {
	if page < 1 || page > 10000 || strings.TrimSpace(branch) == "" || len(branch) > 256 {
		return nil, domain.Validation("branch and valid page are required")
	}

	r, err := s.eligibleRepository(ctx, project, id)
	if err != nil {
		return nil, err
	}

	return s.cfg.SourceControl.Commits(ctx, r, branch, page)
}
func (s *Service) ResolveRevision(ctx context.Context, project, id, component, ref, after string, limit int) (domain.RevisionResolution, error) {
	var out domain.RevisionResolution
	if err := ValidatePage(after, limit); err != nil {
		return out, err
	}

	if strings.TrimSpace(ref) == "" || len(ref) > 256 {
		return out, domain.Validation("a branch or full Git commit SHA is required")
	}

	r, err := s.eligibleRepository(ctx, project, id)
	if err != nil {
		return out, err
	}

	if _, ok := r.Images[component]; !ok {
		return out, domain.Validation("component is not mapped to this repository")
	}

	commit, err := s.cfg.SourceControl.Resolve(ctx, r, ref)
	if err != nil {
		return out, err
	}

	b, _ := s.buildStore()
	builds, next, err := b.Builds(ctx, project, id, component, commit.SHA, after, limit)
	return domain.RevisionResolution{Repository: r, Commit: commit, Builds: builds, NextCursor: next, CIURL: "https://github.com/" + r.GitHubRepository + "/actions"}, err
}
func (s *Service) RecordBuild(ctx context.Context, project, id string, report domain.BuildReport) (domain.Build, error) {
	var out domain.Build
	if !commitSHA.MatchString(report.Revision) || !runID.MatchString(report.RunID) || report.Attempt < 1 || report.Attempt > 100000 || report.BuiltAt.IsZero() || report.BuiltAt.After(time.Now().Add(5*time.Minute)) {
		return out, domain.Validation("build requires a full lowercase commit SHA, numeric run_id, positive attempt, and valid built_at")
	}

	r, err := s.eligibleRepository(ctx, project, id)
	if err != nil {
		return out, err
	}

	location, ok := r.Images[report.Component]
	parts := strings.Split(report.Image, "@")
	if !ok || len(parts) != 2 || parts[0] != location || !imageDigest.MatchString(parts[1]) {
		return out, domain.Validation("image must use the component's approved registry location and sha256 digest")
	}

	commit, err := s.cfg.SourceControl.Resolve(ctx, r, report.Revision)
	if err != nil {
		return out, err
	}

	if commit.SHA != report.Revision {
		return out, domain.Validation("commit resolution did not match the reported revision")
	}

	if err = s.checkImage(ctx, report.Image); err != nil {
		return out, err
	}

	report.BuiltAt = report.BuiltAt.UTC()
	identity, _ := json.Marshal([]any{project, id, report.Component, report.RunID, report.Attempt})
	hash := sha256.Sum256(identity)
	out = domain.Build{BuildReport: report, ID: hex.EncodeToString(hash[:]), Project: project, Repository: id, GitHubRepository: r.GitHubRepository, RunURL: "https://github.com/" + r.GitHubRepository + "/actions/runs/" + report.RunID + "/attempts/" + strconv.Itoa(report.Attempt)}
	b, _ := s.buildStore()
	return b.RecordBuild(ctx, out)
}
func (s *Service) checkImage(ctx context.Context, image string) error {
	if s.cfg.ImageRegistry == nil {
		return &domain.Error{Code: "unavailable", Message: "image registry lookup is not configured"}
	}

	return s.cfg.ImageRegistry.Check(ctx, image)
}
func (s *Service) resolveOverrides(ctx context.Context, project string, in map[string]domain.ComponentOverride) (map[string]domain.ComponentOverride, error) {
	out := make(map[string]domain.ComponentOverride, len(in))
	for component, o := range in {
		if o.BuildID == "" {
			out[component] = o
			continue
		}

		b, err := s.buildStore()
		if err != nil {
			return nil, err
		}

		build, err := b.Build(ctx, project, o.BuildID)
		if err != nil {
			return nil, err
		}

		if build.Component != component {
			return nil, domain.Validation("build does not belong to the selected component")
		}

		r, err := s.eligibleRepository(ctx, project, build.Repository)
		if err != nil {
			return nil, err
		}

		if !strings.HasPrefix(build.Image, r.Images[component]+"@sha256:") {
			return nil, domain.Validation("build no longer matches repository image mapping")
		}

		if err = s.checkImage(ctx, build.Image); err != nil {
			return nil, err
		}

		out[component] = domain.ComponentOverride{Image: build.Image, BuildID: build.ID, Source: &build}
	}

	return out, nil
}
