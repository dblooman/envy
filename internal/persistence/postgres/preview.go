package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/dblooman/envy/internal/domain"
	"github.com/jackc/pgx/v5"
)

func (s *Store) PreviewProfile(ctx context.Context, project, baseline, component string) (*domain.PreviewProfile, error) {
	var body []byte
	err := s.pool.QueryRow(ctx, `SELECT body FROM preview_profiles WHERE project=$1 AND baseline=$2 AND component=$3 ORDER BY revision DESC LIMIT 1`, project, baseline, component).Scan(&body)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}

	if err != nil {
		return nil, unavailable("read preview profile")
	}

	var p domain.PreviewProfile
	if json.Unmarshal(body, &p) != nil {
		return nil, unavailable("decode preview profile")
	}

	return &p, nil
}
func (s *Store) ApprovePreview(ctx context.Context, p domain.PreviewProfile, expected int64) (domain.PreviewProfile, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return p, unavailable("begin preview approval")
	}
	defer tx.Rollback(ctx)
	// Serialize approvals on the existing baseline, including the first revision.
	var id string
	if err = tx.QueryRow(ctx, `SELECT id FROM baselines WHERE project=$1 AND id=$2 FOR UPDATE`, p.Project, p.Baseline).Scan(&id); err != nil {
		return p, catalogError(err)
	}

	var revision int64
	if err = tx.QueryRow(ctx, `SELECT COALESCE(MAX(revision),0) FROM preview_profiles WHERE project=$1 AND baseline=$2 AND component=$3`, p.Project, p.Baseline, p.Component).Scan(&revision); err != nil {
		return p, unavailable("read preview revision")
	}

	if revision != expected {
		return p, &domain.Error{Code: "conflict", Message: "preview profile changed; inspect before approving"}
	}

	p.Revision = revision + 1
	body, err := json.Marshal(p)
	if err != nil {
		return p, domain.Validation("invalid preview profile")
	}

	if _, err = tx.Exec(ctx, `INSERT INTO preview_profiles(project,baseline,component,revision,body) VALUES($1,$2,$3,$4,$5)`, p.Project, p.Baseline, p.Component, p.Revision, body); err != nil {
		return p, catalogError(err)
	}

	if err = insertActivity(ctx, tx, domain.Activity{Action: "catalog.preview.approve", Outcome: "accepted", Project: p.Project, ResourceType: "preview_profile", ResourceID: p.Baseline + "/" + p.Component, Changes: activityChanges(map[string]any{"revision": p.Revision, "source_uid": p.SourceUID, "contract": p.Contract})}); err != nil {
		return p, err
	}

	if err = tx.Commit(ctx); err != nil {
		return p, unavailable("commit preview approval")
	}

	return p, nil
}

func (s *Store) ReplayCreate(ctx context.Context, key, hash string) (*domain.Composition, error) {
	row, err := s.queries.GetIdempotencyKey(ctx, key)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}

	if err != nil {
		return nil, unavailable("read create retry")
	}

	if row.RequestHash != hash {
		return nil, &domain.Error{Code: "conflict", Message: "idempotency key was already used with a different request"}
	}

	c, err := s.Get(ctx, row.CompositionID)
	return &c, err
}
