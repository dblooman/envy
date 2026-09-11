package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"

	"github.com/dblooman/envy/internal/domain"
	"github.com/dblooman/envy/internal/persistence/postgres/sqlc"
	"github.com/jackc/pgx/v5"
)

func decodeSource(data []byte, err error) (domain.SourceRepository, error) {
	var r domain.SourceRepository
	if errors.Is(err, pgx.ErrNoRows) {
		return r, domain.NotFound("repository not registered in project")
	}
	if err != nil || json.Unmarshal(data, &r) != nil {
		return r, unavailable("read source repository")
	}
	return r, nil
}
func (s *Store) SourceRepository(ctx context.Context, project, id string) (domain.SourceRepository, error) {
	return decodeSource(s.queries.GetSourceRepository(ctx, sqlc.GetSourceRepositoryParams{Project: project, ID: id}))
}
func (s *Store) SourceRepositories(ctx context.Context, project, after string, limit int) ([]domain.SourceRepository, string, error) {
	rows, err := s.queries.ListSourceRepositories(ctx, sqlc.ListSourceRepositoriesParams{Project: project, After: after, Limit: int32(limit + 1)})
	if err != nil {
		return nil, "", unavailable("list source repositories")
	}
	return decodePage(rows, limit, func(r domain.SourceRepository) string { return r.ID })
}
func (s *Store) RegisterSourceRepository(ctx context.Context, r domain.SourceRepository) (domain.SourceRepository, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return r, unavailable("begin repository registration")
	}
	defer tx.Rollback(ctx)
	q := s.queries.WithTx(tx)
	body, _ := json.Marshal(r)
	n, err := q.InsertSourceRepository(ctx, sqlc.InsertSourceRepositoryParams{Project: r.Project, ID: r.ID, Body: body})
	if err != nil {
		return r, unavailable("register source repository")
	}
	if n == 0 {
		old, e := decodeSource(q.GetSourceRepository(ctx, sqlc.GetSourceRepositoryParams{Project: r.Project, ID: r.ID}))
		if e != nil || !reflect.DeepEqual(old, r) {
			return r, &domain.Error{Code: "conflict", Message: "repository identity and component mappings are immutable; use enable/disable to change availability"}
		}
		return old, nil
	}
	for component := range r.Images {
		if err = q.ClaimSourceComponent(ctx, sqlc.ClaimSourceComponentParams{Project: r.Project, Component: component, Repository: r.ID}); err != nil {
			return r, &domain.Error{Code: "conflict", Message: "component is missing or already mapped to a repository"}
		}
	}
	if err = insertActivity(ctx, tx, domain.Activity{Action: "source.register", Outcome: "accepted", Project: r.Project, ResourceType: "source_repository", ResourceID: r.ID, Changes: body}); err != nil {
		return r, err
	}
	if err = tx.Commit(ctx); err != nil {
		return r, unavailable("commit source repository")
	}
	return r, nil
}
func (s *Store) EnableSourceRepository(ctx context.Context, project, id string, enabled bool) (domain.SourceRepository, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.SourceRepository{}, unavailable("begin repository update")
	}
	defer tx.Rollback(ctx)
	q := s.queries.WithTx(tx)
	r, err := decodeSource(q.EnableSourceRepository(ctx, sqlc.EnableSourceRepositoryParams{Project: project, ID: id, Enabled: enabled}))
	if err != nil {
		return r, err
	}
	body, _ := json.Marshal(map[string]bool{"enabled": enabled})
	if err = insertActivity(ctx, tx, domain.Activity{Action: "source.enable", Outcome: "accepted", Project: project, ResourceType: "source_repository", ResourceID: id, Changes: body}); err != nil {
		return r, err
	}
	if err = tx.Commit(ctx); err != nil {
		return r, unavailable("commit repository update")
	}
	return r, nil
}
func decodeBuild(data []byte, err error) (domain.Build, error) {
	var b domain.Build
	if errors.Is(err, pgx.ErrNoRows) {
		return b, domain.NotFound("build not found in project")
	}
	if err != nil || json.Unmarshal(data, &b) != nil {
		return b, unavailable("read build")
	}
	return b, nil
}
func (s *Store) Build(ctx context.Context, project, id string) (domain.Build, error) {
	return decodeBuild(s.queries.GetBuild(ctx, sqlc.GetBuildParams{Project: project, ID: id}))
}
func (s *Store) Builds(ctx context.Context, project, repository, component, revision, after string, limit int) ([]domain.Build, string, error) {
	rows, err := s.queries.ListBuilds(ctx, sqlc.ListBuildsParams{Project: project, Repository: repository, Component: component, Revision: revision, After: after, Limit: int32(limit + 1)})
	if err != nil {
		return nil, "", unavailable("list builds")
	}
	return decodePage(rows, limit, func(b domain.Build) string { return b.ID })
}
func (s *Store) RecordBuild(ctx context.Context, b domain.Build) (domain.Build, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return b, unavailable("begin build report")
	}
	defer tx.Rollback(ctx)
	q := s.queries.WithTx(tx)
	if err = checkBuildRepository(ctx, q, b); err != nil {
		return b, err
	}
	body, _ := json.Marshal(b)
	n, err := q.InsertBuild(ctx, sqlc.InsertBuildParams{ID: b.ID, Project: b.Project, Repository: b.Repository, Component: b.Component, Revision: b.Revision, Body: body})
	if err != nil {
		return b, unavailable("record build")
	}
	if n == 0 {
		old, e := decodeBuild(q.GetBuild(ctx, sqlc.GetBuildParams{Project: b.Project, ID: b.ID}))
		if e != nil {
			return b, e
		}
		oldJSON, _ := json.Marshal(old)
		if string(oldJSON) != string(body) {
			return b, &domain.Error{Code: "conflict", Message: "CI run attempt already reported a different build for this component"}
		}
		return old, nil
	}
	if err = insertActivity(ctx, tx, domain.Activity{Action: "build.report", Outcome: "accepted", Project: b.Project, ResourceType: "build", ResourceID: b.ID, Changes: body}); err != nil {
		return b, err
	}
	if err = tx.Commit(ctx); err != nil {
		return b, unavailable("commit build")
	}
	return b, nil
}
func checkBuildRepository(ctx context.Context, q *sqlc.Queries, b domain.Build) error {
	r, err := decodeSource(q.LockSourceRepository(ctx, sqlc.LockSourceRepositoryParams{Project: b.Project, ID: b.Repository}))
	if err != nil {
		return err
	}
	if !r.Enabled {
		return &domain.Error{Code: "conflict", Message: "repository is disabled"}
	}
	if r.GitHubRepository != b.GitHubRepository || strings.Split(b.Image, "@")[0] != r.Images[b.Component] {
		return domain.Validation("build does not match approved repository mapping")
	}
	return nil
}
func validateBuildOverrides(ctx context.Context, q *sqlc.Queries, project string, overrides map[string]domain.ComponentOverride) error {
	for _, component := range domain.OverrideNames(overrides) {
		o := overrides[component]
		if o.BuildID == "" {
			continue
		}
		b, err := decodeBuild(q.GetBuild(ctx, sqlc.GetBuildParams{Project: project, ID: o.BuildID}))
		if err != nil {
			return err
		}
		if b.Component != component || b.Image != o.Image || o.Source == nil {
			return domain.Validation("resolved build does not match override")
		}
		if err = checkBuildRepository(ctx, q, b); err != nil {
			return err
		}
	}
	return nil
}
