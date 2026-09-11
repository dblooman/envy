package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/dblooman/envy/internal/domain"
	"github.com/jackc/pgx/v5"
)

func activityIdentity(ctx context.Context) domain.RequestIdentity {
	identity := domain.RequestIdentityFromContext(ctx)
	if identity.Principal.ID == "" {
		identity.Principal = domain.Principal{Kind: "unknown", ID: "unknown"}
	}
	if identity.Channel == "" {
		identity.Channel = "unknown"
	}
	return identity
}

func insertActivity(ctx context.Context, tx pgx.Tx, event domain.Activity) error {
	identity := activityIdentity(ctx)
	event.Actor, event.Channel, event.Task = identity.Principal, identity.Channel, identity.Task
	event.OccurredAt = time.Now().UTC()
	body, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO activity_events(project, actor_id, action, outcome, resource_type, resource_id, operation_id, body) VALUES ($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT (operation_id,outcome) WHERE operation_id <> '' DO NOTHING`, event.Project, event.Actor.ID, event.Action, event.Outcome, event.ResourceType, event.ResourceID, event.Operation, body); err != nil {
		return unavailable("persist activity")
	}
	return nil
}

func activityChanges(value any) json.RawMessage { body, _ := json.Marshal(value); return body }

func insertRevision(ctx context.Context, tx pgx.Tx, c domain.Composition) error {
	identity := activityIdentity(ctx)
	revision := domain.CompositionRevision{Composition: c.ID, Project: c.Project, Generation: c.Generation, Baseline: c.Baseline, BaselineRevision: c.BaselineRevision, Overrides: c.Overrides, CreatedAt: time.Now().UTC(), Actor: identity.Principal, Channel: identity.Channel, Operation: c.LatestOperation.ID}
	body, err := json.Marshal(revision)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO composition_revisions(composition_id,generation,project,body) VALUES ($1,$2,$3,$4) ON CONFLICT DO NOTHING`, c.ID, c.Generation, c.Project, body); err != nil {
		return unavailable("persist composition revision")
	}
	return nil
}

func parseActivityCursor(value string) (int64, error) {
	if value == "" {
		return 0, nil
	}
	v, err := strconv.ParseInt(value, 10, 64)
	if err != nil || v < 0 {
		return 0, domain.Validation("invalid activity cursor")
	}
	return v, nil
}

func (s *Store) Activity(ctx context.Context, filter domain.ActivityFilter) (domain.ActivityPage, error) {
	page := domain.ActivityPage{Items: []domain.Activity{}}
	after, err := parseActivityCursor(filter.After)
	if err != nil {
		return page, err
	}
	if filter.Limit < 1 || filter.Limit > 100 {
		return page, domain.Validation("limit must be between 1 and 100")
	}
	from, to := filter.From, filter.To
	if from.IsZero() {
		from = time.Unix(0, 0).UTC()
	}
	if to.IsZero() {
		to = time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	rows, err := s.pool.Query(ctx, `SELECT id, occurred_at, body FROM activity_events WHERE ($1=0 OR id < $1) AND ($2='' OR project=$2) AND ($3='' OR actor_id=$3) AND ($4='' OR action=$4) AND ($5='' OR outcome=$5) AND ($6='' OR resource_type=$6) AND ($7='' OR resource_id=$7) AND occurred_at >= $8 AND occurred_at <= $9 ORDER BY id DESC LIMIT $10`, after, filter.Project, filter.Actor, filter.Action, filter.Outcome, filter.ResourceType, filter.ResourceID, from, to, filter.Limit+1)
	if err != nil {
		return page, unavailable("list activity")
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var occurred time.Time
		var body []byte
		var event domain.Activity
		if err = rows.Scan(&id, &occurred, &body); err != nil || json.Unmarshal(body, &event) != nil {
			return page, unavailable("decode activity")
		}
		event.ID, event.OccurredAt = strconv.FormatInt(id, 10), occurred
		page.Items = append(page.Items, event)
	}
	if err = rows.Err(); err != nil {
		return page, unavailable("list activity")
	}
	if len(page.Items) > filter.Limit {
		page.Items = page.Items[:filter.Limit]
		page.NextCursor = page.Items[len(page.Items)-1].ID
	}
	return page, nil
}

func (s *Store) RecordRejectedActivity(ctx context.Context, event domain.Activity) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return unavailable("begin rejected activity")
	}
	defer tx.Rollback(ctx)
	if err = insertActivity(ctx, tx, event); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return unavailable("commit rejected activity")
	}
	return nil
}
func (s *Store) PruneActivity(ctx context.Context, before time.Time) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return unavailable("begin activity retention")
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `DELETE FROM activity_events WHERE occurred_at < $1`, before); err != nil {
		return unavailable("prune activity")
	}
	if _, err = tx.Exec(ctx, `DELETE FROM composition_revisions WHERE created_at < $1`, before); err != nil {
		return unavailable("prune revisions")
	}
	if err = tx.Commit(ctx); err != nil {
		return unavailable("commit activity retention")
	}
	return nil
}

func (s *Store) Revisions(ctx context.Context, id, after string, limit int) (domain.RevisionsPage, error) {
	page := domain.RevisionsPage{Items: []domain.CompositionRevision{}}
	cursor, err := parseActivityCursor(after)
	if err != nil {
		return page, err
	}
	if limit < 1 || limit > 100 {
		return page, domain.Validation("limit must be between 1 and 100")
	}
	if _, err = s.Get(ctx, id); err != nil {
		return page, err
	}
	rows, err := s.pool.Query(ctx, `SELECT generation, created_at, body FROM composition_revisions WHERE composition_id=$1 AND generation > $2 ORDER BY generation LIMIT $3`, id, cursor, limit+1)
	if err != nil {
		return page, unavailable("list composition revisions")
	}
	defer rows.Close()
	for rows.Next() {
		var generation int64
		var created time.Time
		var body []byte
		var revision domain.CompositionRevision
		if err = rows.Scan(&generation, &created, &body); err != nil || json.Unmarshal(body, &revision) != nil {
			return page, unavailable("decode composition revision")
		}
		revision.Generation, revision.CreatedAt = generation, created
		page.Items = append(page.Items, revision)
	}
	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
		page.NextCursor = strconv.FormatInt(page.Items[len(page.Items)-1].Generation, 10)
	}
	return page, nil
}

func (s *Store) Revision(ctx context.Context, id string, generation int64) (domain.CompositionRevision, error) {
	var revision domain.CompositionRevision
	var created time.Time
	var body []byte
	err := s.pool.QueryRow(ctx, `SELECT created_at, body FROM composition_revisions WHERE composition_id=$1 AND generation=$2`, id, generation).Scan(&created, &body)
	if errors.Is(err, pgx.ErrNoRows) {
		return revision, domain.NotFound("composition revision not found")
	}
	if err != nil || json.Unmarshal(body, &revision) != nil {
		return revision, unavailable("read composition revision")
	}
	revision.CreatedAt = created
	return revision, nil
}
