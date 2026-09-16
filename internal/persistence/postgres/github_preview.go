package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/dblooman/envy/internal/domain"
	"github.com/jackc/pgx/v5"
)

func (s *Store) PreviewPolicies(ctx context.Context) ([]domain.PRPreviewPolicy, error) {
	rows, e := s.pool.Query(ctx, "SELECT body FROM github_preview_policies ORDER BY project,repository")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []domain.PRPreviewPolicy{}
	for rows.Next() {
		var b []byte
		var p domain.PRPreviewPolicy
		if e = rows.Scan(&b); e != nil {
			return nil, e
		}

		if e = json.Unmarshal(b, &p); e != nil {
			return nil, e
		}

		out = append(out, p)
	}

	return out, rows.Err()
}

func (s *Store) SavePreviewPolicy(ctx context.Context, p domain.PRPreviewPolicy) error {
	b, e := json.Marshal(p)
	if e != nil {
		return e
	}

	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}

	defer func() { _ = tx.Rollback(ctx) }()
	_, e = tx.Exec(ctx, `INSERT INTO github_preview_policies(project,repository,github_repository_id,enabled,body) VALUES($1,$2,$3,$4,$5) ON CONFLICT(project,repository) DO UPDATE SET github_repository_id=$3,enabled=$4,body=$5`, p.Project, p.Repository, p.GitHubRepositoryID, p.Enabled, b)
	if e != nil {
		return &domain.Error{Code: "conflict", Message: "could not save policy; repository must be registered and only one enabled policy may own a GitHub repository"}
	}

	if e = insertActivity(ctx, tx, domain.Activity{Action: "github.policy.save", Outcome: "accepted", Project: p.Project, ResourceType: "source_repository", ResourceID: p.Repository, Changes: b}); e != nil {
		return e
	}

	return tx.Commit(ctx)
}

func (s *Store) PRPreviews(ctx context.Context, project, after string, limit int) ([]domain.PRPreview, string, error) {
	rows, e := s.pool.Query(ctx, `SELECT body,version,COALESCE(composition_id,'') FROM github_pr_previews WHERE ($1='' OR project=$1) AND id>$2 ORDER BY id LIMIT $3`, project, after, limit+1)
	if e != nil {
		return nil, "", e
	}
	defer rows.Close()
	out := []domain.PRPreview{}
	for rows.Next() {
		var b []byte
		var p domain.PRPreview
		var v int64
		var c string
		if e = rows.Scan(&b, &v, &c); e != nil {
			return nil, "", e
		}

		if e = json.Unmarshal(b, &p); e != nil {
			return nil, "", e
		}

		p.Version = v
		p.CompositionID = c
		out = append(out, p)
	}

	next := ""
	if len(out) > limit {
		out = out[:limit]
		next = out[len(out)-1].ID
	}

	return out, next, rows.Err()
}

func (s *Store) PRPreview(ctx context.Context, id string) (domain.PRPreview, error) {
	var b []byte
	var p domain.PRPreview
	var v int64
	var c string
	e := s.pool.QueryRow(ctx, "SELECT body,version,COALESCE(composition_id,'') FROM github_pr_previews WHERE id=$1", id).Scan(&b, &v, &c)
	if errors.Is(e, pgx.ErrNoRows) {
		return p, domain.NotFound("PR preview not found")
	}

	if e != nil {
		return p, e
	}

	e = json.Unmarshal(b, &p)
	p.Version = v
	p.CompositionID = c
	return p, e
}

func (s *Store) SavePRPreview(ctx context.Context, p domain.PRPreview) (domain.PRPreview, error) {
	p.UpdatedAt = time.Now().UTC()
	b, e := json.Marshal(p)
	if e != nil {
		return p, e
	}

	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return p, e
	}

	defer func() { _ = tx.Rollback(ctx) }()
	if p.Version == 0 {
		e = tx.QueryRow(ctx, `INSERT INTO github_pr_previews(id,project,repository,number,body) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING RETURNING version`, p.ID, p.Policy.Project, p.Policy.Repository, p.Number, b).Scan(&p.Version)
	} else {
		var previous []byte
		var composition string
		e = tx.QueryRow(ctx, "SELECT body,COALESCE(composition_id,'') FROM github_pr_previews WHERE id=$1 AND version=$2 FOR UPDATE", p.ID, p.Version).Scan(&previous, &composition)
		if errors.Is(e, pgx.ErrNoRows) {
			return p, &domain.Error{Code: "conflict", Message: "PR preview changed; reload before retrying"}
		}

		if e != nil {
			return p, e
		}

		var old domain.PRPreview
		if e = json.Unmarshal(previous, &old); e != nil {
			return p, e
		}

		updated := old.UpdatedAt
		old.Version = p.Version
		old.CompositionID = composition
		old.UpdatedAt = p.UpdatedAt
		if reflect.DeepEqual(old, p) {
			p.UpdatedAt = updated
			return p, tx.Commit(ctx)
		}

		e = tx.QueryRow(ctx, `UPDATE github_pr_previews SET body=$3,version=version+1,composition_id=NULLIF($4,'') WHERE id=$1 AND version=$2 RETURNING version`, p.ID, p.Version, b, p.CompositionID).Scan(&p.Version)
	}

	if errors.Is(e, pgx.ErrNoRows) {
		return p, &domain.Error{Code: "conflict", Message: "PR preview changed; reload before retrying"}
	}

	if e != nil {
		return p, e
	}

	_, e = tx.Exec(ctx, `INSERT INTO github_preview_history(preview_id,lifecycle,body) VALUES($1,$2,$3) ON CONFLICT(preview_id,lifecycle) DO UPDATE SET body=$3`, p.ID, p.Lifecycle, b)
	if e != nil {
		return p, e
	}

	if e = insertActivity(ctx, tx, domain.Activity{Action: "github.preview.reconcile", Outcome: "accepted", Project: p.Policy.Project, ResourceType: "pr_preview", ResourceID: p.ID, Composition: p.CompositionID, Changes: b}); e != nil {
		return p, e
	}

	return p, tx.Commit(ctx)
}

func (s *Store) ReceiveGitHubWebhook(ctx context.Context, id, event string, body []byte) error {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}

	defer func() { _ = tx.Rollback(ctx) }()
	result, e := tx.Exec(ctx, `INSERT INTO github_webhook_deliveries(id,event,body) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, id, event, body)
	if e != nil {
		return e
	}

	if result.RowsAffected() > 0 {
		var payload struct {
			Sender struct {
				Login string `json:"login"`
				ID    int64  `json:"id"`
			} `json:"sender"`
			Action string `json:"action"`
		}
		if e = json.Unmarshal(body, &payload); e != nil {
			return e
		}

		identity := domain.RequestIdentity{Principal: domain.Principal{Kind: "github", ID: fmt.Sprint(payload.Sender.ID), DisplayName: payload.Sender.Login}, Channel: "github"}
		if e = insertActivity(domain.WithRequestIdentity(ctx, identity), tx, domain.Activity{Action: "github.webhook.receive", Outcome: "accepted", ResourceType: "github_delivery", ResourceID: id, Changes: activityChanges(map[string]string{"event": event, "action": payload.Action})}); e != nil {
			return e
		}
	}

	return tx.Commit(ctx)
}

func (s *Store) PendingGitHubEvents(ctx context.Context) (map[string]json.RawMessage, error) {
	rows, e := s.pool.Query(ctx, "SELECT id,body || jsonb_build_object('_envy_event',event) FROM github_webhook_deliveries WHERE processed_at IS NULL ORDER BY received_at LIMIT 100")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := map[string]json.RawMessage{}
	for rows.Next() {
		var id string
		var b []byte
		if e = rows.Scan(&id, &b); e != nil {
			return nil, e
		}

		out[id] = b
	}

	return out, rows.Err()
}

func (s *Store) FinishGitHubEvent(ctx context.Context, id, reason string) error {
	_, e := s.pool.Exec(ctx, `UPDATE github_webhook_deliveries SET processed_at=CASE WHEN $2='' THEN now() ELSE NULL END,error=$2 WHERE id=$1`, id, reason)
	return e
}

func (s *Store) GitHubHealth(ctx context.Context) (map[string]any, error) {
	out := map[string]any{}
	var received, reconciled *time.Time
	var reason string
	if e := s.pool.QueryRow(ctx, "SELECT max(received_at) FROM github_webhook_deliveries").Scan(&received); e != nil {
		return nil, e
	}

	e := s.pool.QueryRow(ctx, "SELECT reconciled_at,error FROM github_preview_health WHERE id=true").Scan(&reconciled, &reason)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return nil, e
	}

	out["last_webhook_at"] = received
	out["last_reconciled_at"] = reconciled
	out["error"] = reason
	return out, nil
}

func (s *Store) SetGitHubHealth(ctx context.Context, reason string) error {
	_, e := s.pool.Exec(ctx, `INSERT INTO github_preview_health(id,reconciled_at,error) VALUES(true,CASE WHEN $1='' THEN now() END,$1) ON CONFLICT(id) DO UPDATE SET reconciled_at=CASE WHEN $1='' THEN now() ELSE github_preview_health.reconciled_at END,error=$1`, reason)
	return e
}

// Lock the owner before any composition mutation. An API stop wins by changing
// the preview version; a stale controller claim cannot create or update compute.
func guardPRPreview(ctx context.Context, tx pgx.Tx, composition, operation string) error {
	claim, controller := domain.PreviewClaim(ctx)
	var b []byte
	var version int64
	var id string
	query := `SELECT id,body,version FROM github_pr_previews WHERE composition_id=$1 FOR UPDATE`
	arg := composition
	if controller {
		query = `SELECT id,body,version FROM github_pr_previews WHERE id=$1 FOR UPDATE`
		arg = claim.ID
	}

	e := tx.QueryRow(ctx, query, arg).Scan(&id, &b, &version)
	if errors.Is(e, pgx.ErrNoRows) && !controller {
		return nil
	}

	if e != nil {
		return e
	}

	var p domain.PRPreview
	if e = json.Unmarshal(b, &p); e != nil {
		return e
	}

	if controller {
		if claim.Version != version || (operation != "destroy" && p.Terminal) {
			return &domain.Error{Code: "conflict", Message: "PR preview lifecycle changed"}
		}

		if operation != "destroy" {
			var enabled bool
			if e = tx.QueryRow(ctx, "SELECT enabled FROM github_preview_policies WHERE project=$1 AND repository=$2 FOR SHARE", p.Policy.Project, p.Policy.Repository).Scan(&enabled); e != nil {
				return e
			}

			if !enabled {
				return &domain.Error{Code: "conflict", Message: "PR preview policy was disabled"}
			}
		}

		return nil
	}

	if operation == "update" {
		return &domain.Error{Code: "conflict", Message: "PR preview is managed automatically; stop it and create an independent environment to edit builds"}
	}

	if operation == "destroy" {
		p.Terminal = true
		p.RestartBarrierAt = time.Now().UTC()
		p.Status = "stopped"
		p.Reason = "Stopped explicitly"
		b, _ = json.Marshal(p)
		_, e = tx.Exec(ctx, "UPDATE github_pr_previews SET body=$2,version=version+1 WHERE id=$1", id, b)
	}

	return e
}
