package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/dblooman/envy/internal/domain"
	"github.com/dblooman/envy/internal/persistence/postgres/sqlc"
	"github.com/jackc/pgx/v5"
)

func decodeFrontend(data []byte, err error) (domain.FrontendBinding, error) {
	var b domain.FrontendBinding
	if errors.Is(err, pgx.ErrNoRows) {
		return b, domain.NotFound("frontend revision binding not found in project")
	}

	if err != nil {
		return b, unavailable("read frontend binding")
	}

	if json.Unmarshal(data, &b) != nil {
		return b, unavailable("decode frontend binding")
	}

	return b, nil
}

func (s *Store) FrontendBinding(ctx context.Context, k domain.FrontendKey) (domain.FrontendBinding, error) {
	return decodeFrontend(s.queries.GetFrontendBinding(ctx, sqlc.GetFrontendBindingParams{Project: k.Project, Frontend: k.Frontend, Revision: k.Revision}))
}

func (s *Store) FrontendBindings(ctx context.Context, id, after string, limit int) ([]domain.FrontendBinding, string, error) {
	rows, err := s.queries.ListFrontendBindings(ctx, sqlc.ListFrontendBindingsParams{Composition: id, After: after, Limit: int32(limit + 1)})
	if err != nil {
		return nil, "", unavailable("list frontend bindings")
	}

	return decodePage(rows, limit, func(b domain.FrontendBinding) string { return b.Frontend + ":" + b.Revision })
}

func frontendComposition(ctx context.Context, q *sqlc.Queries, project, id string) (domain.Composition, error) {
	row, err := q.GetCompositionForUpdate(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Composition{}, domain.NotFound("composition not found in project")
	}

	if err != nil {
		return domain.Composition{}, unavailable("lock bound composition")
	}

	c, err := decodeComposition(row.Body, row.Runtime, row.DeletionRequested)
	if err != nil {
		return c, err
	}

	if c.Project != project {
		return domain.Composition{}, domain.NotFound("composition not found in project")
	}

	return c, nil
}

func sameFrontend(b domain.FrontendBinding, req domain.BindFrontendRequest) error {
	if b.Composition != req.Composition || b.Repository != req.Repository {
		return &domain.Error{Code: "conflict", Message: "frontend revision is already bound; associations are immutable", Project: b.Project, Composition: b.Composition}
	}

	return nil
}

func (s *Store) BindFrontend(ctx context.Context, k domain.FrontendKey, req domain.BindFrontendRequest) (domain.FrontendBinding, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.FrontendBinding{}, unavailable("begin frontend binding")
	}
	defer tx.Rollback(ctx)
	q := s.queries.WithTx(tx)
	c, err := frontendComposition(ctx, q, k.Project, req.Composition)
	if err != nil {
		return domain.FrontendBinding{}, err
	}

	old, err := decodeFrontend(q.GetFrontendBinding(ctx, sqlc.GetFrontendBindingParams{Project: k.Project, Frontend: k.Frontend, Revision: k.Revision}))
	if err == nil {
		return old, sameFrontend(old, req)
	}

	var de *domain.Error
	if !errors.As(err, &de) || de.Code != "not_found" {
		return domain.FrontendBinding{}, err
	}

	if err = domain.FrontendCompositionAvailable(c, time.Now(), false); err != nil {
		return domain.FrontendBinding{}, err
	}

	n, err := q.CountFrontendBindings(ctx, c.ID)
	if err != nil {
		return domain.FrontendBinding{}, unavailable("count frontend bindings")
	}

	if n >= 100 {
		return domain.FrontendBinding{}, &domain.Error{Code: "capacity_exceeded", Message: "composition has 100 frontend bindings"}
	}

	now := time.Now().UTC()
	b := domain.FrontendBinding{Project: k.Project, Frontend: k.Frontend, Revision: k.Revision, Composition: c.ID, Repository: req.Repository, Version: 1, CreatedAt: now, UpdatedAt: now}
	body, _ := json.Marshal(b)
	inserted, err := q.InsertFrontendBinding(ctx, sqlc.InsertFrontendBindingParams{Project: k.Project, Frontend: k.Frontend, Revision: k.Revision, Composition: c.ID, Body: body})
	if err != nil {
		return b, unavailable("persist frontend binding")
	}

	if inserted == 0 {
		b, err = decodeFrontend(q.GetFrontendBinding(ctx, sqlc.GetFrontendBindingParams{Project: k.Project, Frontend: k.Frontend, Revision: k.Revision}))
		if err != nil {
			return b, err
		}

		if err = sameFrontend(b, req); err != nil {
			return b, err
		}
	} else {
		if err = insertActivity(ctx, tx, domain.Activity{Action: "frontend.bind", Outcome: "accepted", Project: k.Project, ResourceType: "frontend_binding", ResourceID: k.Frontend + ":" + k.Revision, Composition: c.ID, GenerationTo: c.Generation, Changes: activityChanges(map[string]any{"frontend": k.Frontend, "revision": k.Revision, "repository": req.Repository})}); err != nil {
			return b, err
		}
	}

	if err = tx.Commit(ctx); err != nil {
		return b, unavailable("commit frontend binding")
	}

	return b, nil
}

// Lock composition before binding in every mutation. This serializes expiry,
// generation changes and publications without making metadata part of reconciler
// observations, which would otherwise overwrite concurrent binding writes.
func (s *Store) updateFrontend(ctx context.Context, k domain.FrontendKey, expected int64, action string, mutate func(*domain.FrontendBinding, domain.Composition) (bool, error)) (domain.FrontendBinding, error) {
	b, err := s.FrontendBinding(ctx, k)
	if err != nil {
		return b, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return b, unavailable("begin frontend update")
	}
	defer tx.Rollback(ctx)
	q := s.queries.WithTx(tx)
	c, err := frontendComposition(ctx, q, k.Project, b.Composition)
	if err != nil {
		return b, err
	}

	if err = domain.FrontendCompositionAvailable(c, time.Now(), false); err != nil {
		return b, err
	}

	b, err = decodeFrontend(q.GetFrontendBindingForUpdate(ctx, sqlc.GetFrontendBindingForUpdateParams{Project: k.Project, Frontend: k.Frontend, Revision: k.Revision}))
	if err != nil {
		return b, err
	}

	version := b.Version
	changed, err := mutate(&b, c)
	if err != nil {
		return b, err
	}

	if !changed {
		return b, nil
	}

	if expected != version {
		return domain.FrontendBinding{}, &domain.Error{Code: "conflict", Message: "expected_version does not match current frontend binding version", Project: k.Project, Composition: c.ID}
	}

	b.Version++
	b.UpdatedAt = time.Now().UTC()
	body, _ := json.Marshal(b)
	if err = q.UpdateFrontendBinding(ctx, sqlc.UpdateFrontendBindingParams{Project: k.Project, Frontend: k.Frontend, Revision: k.Revision, Body: body}); err != nil {
		return b, unavailable("persist frontend update")
	}

	if err = insertActivity(ctx, tx, domain.Activity{Action: action, Outcome: "accepted", Project: k.Project, ResourceType: "frontend_binding", ResourceID: k.Frontend + ":" + k.Revision, Composition: c.ID, GenerationTo: c.Generation, Changes: activityChanges(map[string]any{"version": b.Version, "url_reported": b.URL != "", "check_status": func() string {
		if b.Check == nil {
			return ""
		}

		return b.Check.Status
	}()})}); err != nil {
		return b, err
	}

	if err = tx.Commit(ctx); err != nil {
		return b, unavailable("commit frontend update")
	}

	return b, nil
}

func (s *Store) PublishFrontend(ctx context.Context, k domain.FrontendKey, req domain.PublishFrontendRequest) (domain.FrontendBinding, error) {
	return s.updateFrontend(ctx, k, req.ExpectedVersion, "frontend.publish", func(b *domain.FrontendBinding, _ domain.Composition) (bool, error) {
		if b.URL == req.URL {
			return false, nil
		}

		b.URL = req.URL
		b.Check = nil
		return true, nil
	})
}

func (s *Store) CheckFrontend(ctx context.Context, k domain.FrontendKey, req domain.FrontendCheckRequest) (domain.FrontendBinding, error) {
	return s.updateFrontend(ctx, k, req.ExpectedVersion, "frontend.check", func(b *domain.FrontendBinding, c domain.Composition) (bool, error) {
		if err := domain.FrontendCompositionAvailable(c, time.Now(), true); err != nil {
			return false, err
		}

		if b.URL == "" || req.CompositionGeneration != c.Generation {
			return false, &domain.Error{Code: "conflict", Message: "browser check requires a reported frontend URL and current composition generation"}
		}

		if old := b.Check; old != nil && old.CompositionGeneration == req.CompositionGeneration && old.Status == req.Status && old.Message == req.Message {
			return false, nil
		}

		b.Check = &domain.FrontendCheck{CompositionGeneration: req.CompositionGeneration, Status: req.Status, Message: req.Message, ReportedAt: time.Now().UTC()}
		return true, nil
	})
}
