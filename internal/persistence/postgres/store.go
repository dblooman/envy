// Package postgres stores canonical desired state, observations, and operations.
package postgres

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/dblooman/envy/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrations embed.FS

type Store struct{ pool *pgxpool.Pool }

func Open(ctx context.Context, databaseURL string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, unavailable("parse database configuration")
	}
	cfg.MaxConns = 8
	cfg.ConnConfig.ConnectTimeout = 5 * time.Second
	p, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, unavailable("open database")
	}
	if err = p.Ping(ctx); err != nil {
		p.Close()
		return nil, unavailable("connect to database")
	}
	return &Store{pool: p}, nil
}
func (s *Store) Close() { s.pool.Close() }
func (s *Store) Ping(ctx context.Context) error {
	if err := s.pool.Ping(ctx); err != nil {
		return unavailable("check database")
	}
	return nil
}

// Errors intentionally omit PostgreSQL connection strings and credentials.
func unavailable(action string) error {
	return &domain.Error{Code: "unavailable", Message: action + ": database unavailable", Retryable: true}
}
func (s *Store) Migrate(ctx context.Context) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return unavailable("begin migration")
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(818820)"); err != nil {
		return unavailable("lock migration")
	}
	if _, err = tx.Exec(ctx, "CREATE TABLE IF NOT EXISTS envy_schema_migrations (name text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())"); err != nil {
		return unavailable("initialize migration history")
	}
	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		return err
	}
	for _, e := range entries {
		var exists bool
		if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM envy_schema_migrations WHERE name=$1)", e.Name()).Scan(&exists); err != nil {
			return unavailable("read migration history")
		}
		if exists {
			continue
		}
		body, err := migrations.ReadFile("migrations/" + e.Name())
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, string(body)); err != nil {
			return unavailable("apply migration " + e.Name())
		}
		if _, err = tx.Exec(ctx, "INSERT INTO envy_schema_migrations(name) VALUES($1)", e.Name()); err != nil {
			return unavailable("record migration")
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return unavailable("commit migrations")
	}
	return nil
}

type scanner interface{ Scan(...any) error }

func scanComposition(row scanner) (domain.Composition, error) {
	var c domain.Composition
	var body, runtime []byte
	var deletion bool
	if err := row.Scan(&body, &runtime, &deletion); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return c, domain.NotFound("composition not found")
		}
		return c, unavailable("read composition")
	}
	if err := json.Unmarshal(body, &c); err != nil {
		return c, unavailable("decode composition")
	}
	if err := json.Unmarshal(runtime, &c.Runtime); err != nil {
		return c, unavailable("decode runtime state")
	}
	c.DeletionRequested = deletion
	return c, nil
}
func (s *Store) Get(ctx context.Context, id string) (domain.Composition, error) {
	return scanComposition(s.pool.QueryRow(ctx, "SELECT body,runtime,deletion_requested FROM compositions WHERE id=$1", id))
}
func (s *Store) Create(ctx context.Context, c domain.Composition, key, hash string, max int) (domain.Composition, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return c, unavailable("begin create")
	}
	defer tx.Rollback(ctx)
	// The transaction lock serializes idempotency and the live composition cap
	// across API replicas, without holding the reconciler lease.
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(818819)"); err != nil {
		return c, unavailable("lock create")
	}
	if key != "" {
		var originalHash, id string
		err = tx.QueryRow(ctx, "SELECT request_hash,composition_id FROM idempotency_keys WHERE key=$1", key).Scan(&originalHash, &id)
		if err == nil {
			if originalHash != hash {
				return c, &domain.Error{Code: "conflict", Message: "idempotency key was already used with a different request"}
			}
			return scanComposition(tx.QueryRow(ctx, "SELECT body,runtime,deletion_requested FROM compositions WHERE id=$1", id))
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return c, unavailable("read idempotency key")
		}
	}
	var count int
	if err = tx.QueryRow(ctx, "SELECT count(*) FROM compositions WHERE phase <> 'destroyed'").Scan(&count); err != nil {
		return c, unavailable("check composition capacity")
	}
	if count >= max {
		return c, &domain.Error{Code: "capacity_exceeded", Message: "live composition limit reached", Retryable: true}
	}
	body, err := json.Marshal(c)
	if err != nil {
		return c, err
	}
	runtime, err := json.Marshal(c.Runtime)
	if err != nil {
		return c, err
	}
	if _, err = tx.Exec(ctx, "INSERT INTO compositions(id,project,baseline,generation,deletion_requested,phase,expires_at,body,runtime) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)", c.ID, c.Project, c.Baseline, c.Generation, c.DeletionRequested, c.Phase, c.ExpiresAt, body, runtime); err != nil {
		return c, unavailable("persist composition")
	}
	if key != "" {
		if _, err = tx.Exec(ctx, "INSERT INTO idempotency_keys(key,request_hash,composition_id) VALUES($1,$2,$3)", key, hash, c.ID); err != nil {
			return c, unavailable("persist idempotency key")
		}
	}
	if err = saveOperation(ctx, tx, c); err != nil {
		return c, err
	}
	if err = tx.Commit(ctx); err != nil {
		return c, unavailable("commit composition")
	}
	return c, nil
}
func saveOperation(ctx context.Context, tx pgx.Tx, c domain.Composition) error {
	body, err := json.Marshal(c.LatestOperation)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, "INSERT INTO operations(id,composition_id,body) VALUES($1,$2,$3) ON CONFLICT(id) DO UPDATE SET body=excluded.body,updated_at=now()", c.LatestOperation.ID, c.ID, body)
	if err != nil {
		return unavailable("persist operation")
	}
	return nil
}
func (s *Store) List(ctx context.Context, project, after string, limit int) ([]domain.Composition, string, error) {
	rows, err := s.pool.Query(ctx, "SELECT body,runtime,deletion_requested FROM compositions WHERE ($1='' OR project=$1) AND id>$2 ORDER BY id LIMIT $3", project, after, limit+1)
	if err != nil {
		return nil, "", unavailable("list compositions")
	}
	defer rows.Close()
	items := make([]domain.Composition, 0)
	for rows.Next() {
		c, err := scanComposition(rows)
		if err != nil {
			return nil, "", err
		}
		items = append(items, c)
	}
	if rows.Err() != nil {
		return nil, "", unavailable("list compositions")
	}
	next := ""
	if len(items) > limit {
		items = items[:limit]
		next = items[len(items)-1].ID
	}
	return items, next, nil
}
func (s *Store) Active(ctx context.Context) ([]domain.Composition, error) {
	rows, err := s.pool.Query(ctx, "SELECT body,runtime,deletion_requested FROM compositions WHERE phase <> 'destroyed' ORDER BY id")
	if err != nil {
		return nil, unavailable("scan active compositions")
	}
	defer rows.Close()
	items := make([]domain.Composition, 0)
	for rows.Next() {
		c, err := scanComposition(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, c)
	}
	if rows.Err() != nil {
		return nil, unavailable("scan active compositions")
	}
	return items, nil
}

// SaveObservation fences stale reconcilers against desired-state generations.
// It never allows an observation of generation N to overwrite deletion N+1.
func (s *Store) SaveObservation(ctx context.Context, c domain.Composition) error {
	c.UpdatedAt = time.Now().UTC()
	body, err := json.Marshal(c)
	if err != nil {
		return err
	}
	runtime, err := json.Marshal(c.Runtime)
	if err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return unavailable("begin observation")
	}
	defer tx.Rollback(ctx)
	result, err := tx.Exec(ctx, "UPDATE compositions SET phase=$2,body=$3,runtime=$4 WHERE id=$1 AND generation=$5 AND deletion_requested=$6", c.ID, c.Phase, body, runtime, c.Generation, c.DeletionRequested)
	if err != nil {
		return unavailable("persist observation")
	}
	if result.RowsAffected() != 1 {
		return domain.ErrStaleObservation
	}
	if err = saveOperation(ctx, tx, c); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return unavailable("commit observation")
	}
	return nil
}
func (s *Store) Destroy(ctx context.Context, id string) (domain.Composition, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.Composition{}, unavailable("begin destroy")
	}
	defer tx.Rollback(ctx)
	c, err := scanComposition(tx.QueryRow(ctx, "SELECT body,runtime,deletion_requested FROM compositions WHERE id=$1 FOR UPDATE", id))
	if err != nil {
		return c, err
	}
	if c.DeletionRequested || c.Phase == domain.PhaseDestroyed {
		return c, nil
	}
	if err = requestDeletion(&c, time.Now().UTC(), "requested"); err != nil {
		return c, err
	}
	if err = writeDeletion(ctx, tx, c); err != nil {
		return c, err
	}
	if err = tx.Commit(ctx); err != nil {
		return c, unavailable("commit destroy")
	}
	return c, nil
}
func requestDeletion(c *domain.Composition, now time.Time, reason string) error {
	c.Runtime.DeletionReason = reason
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return &domain.Error{Code: "unavailable", Message: "secure identity generation unavailable", Retryable: true}
	}
	c.Generation++
	c.DeletionRequested = true
	c.Phase = domain.PhaseDestroying
	c.UpdatedAt = now
	c.LastError = nil
	c.LatestOperation = domain.Operation{ID: hex.EncodeToString(b[:]), Kind: "destroy", Status: "pending"}
	c.Runtime.Attempts = 0
	c.Runtime.NextAttemptAt = time.Time{}
	for key, e := range c.Endpoints {
		e.Ready = false
		c.Endpoints[key] = e
	}
	return nil
}
func writeDeletion(ctx context.Context, tx pgx.Tx, c domain.Composition) error {
	body, err := json.Marshal(c)
	if err != nil {
		return err
	}
	runtime, err := json.Marshal(c.Runtime)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "UPDATE compositions SET generation=$2,deletion_requested=true,phase=$3,body=$4,runtime=$5 WHERE id=$1", c.ID, c.Generation, c.Phase, body, runtime); err != nil {
		return unavailable("persist deletion")
	}
	return saveOperation(ctx, tx, c)
}
func (s *Store) Expire(ctx context.Context, now time.Time) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return unavailable("begin expiry")
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, "SELECT body,runtime,deletion_requested FROM compositions WHERE expires_at <= $1 AND NOT deletion_requested AND phase <> 'destroyed' FOR UPDATE SKIP LOCKED", now)
	if err != nil {
		return unavailable("scan expired compositions")
	}
	var expired []domain.Composition
	for rows.Next() {
		c, err := scanComposition(rows)
		if err != nil {
			rows.Close()
			return err
		}
		expired = append(expired, c)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return unavailable("scan expired compositions")
	}
	for _, c := range expired {
		if err = requestDeletion(&c, now, "expired"); err != nil {
			return err
		}
		if err = writeDeletion(ctx, tx, c); err != nil {
			return err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return unavailable("commit expiry")
	}
	return nil
}

// Update serializes desired-state changes with deletion, expiry, and observations.
func (s *Store) Update(ctx context.Context, id string, req domain.UpdateRequest, operationID string) (domain.Composition, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.Composition{}, unavailable("begin update")
	}
	defer tx.Rollback(ctx)
	c, err := scanComposition(tx.QueryRow(ctx, "SELECT body,runtime,deletion_requested FROM compositions WHERE id=$1 FOR UPDATE", id))
	if err != nil {
		return c, err
	}
	now := time.Now().UTC()
	if c.Generation != req.ExpectedGeneration {
		return c, &domain.Error{Code: "conflict", Message: "expected_generation does not match current generation", Composition: id}
	}
	if c.DeletionRequested || !c.ExpiresAt.After(now) || (c.Phase != domain.PhaseReady && c.Phase != domain.PhaseFailed) {
		return c, &domain.Error{Code: "conflict", Message: "only ready or failed, unexpired compositions can be updated", Composition: id}
	}
	c.Generation++
	c.Overrides = req.Overrides
	c.Phase = domain.PhaseUpdating
	c.UpdatedAt = now
	c.LastError = nil
	c.LatestOperation = domain.Operation{ID: operationID, Kind: "update", Status: "pending"}
	c.Runtime.ProvisionStartedAt = now
	c.Runtime.Attempts = 0
	c.Runtime.NextAttemptAt = time.Time{}
	for key, endpoint := range c.Endpoints {
		endpoint.Ready = false
		c.Endpoints[key] = endpoint
	}
	c.Conditions = []domain.Condition{{Type: "WorkloadsReady", Message: "waiting for updated workload"}, {Type: "RoutesConfigured", Status: c.Runtime.RoutingActive}, {Type: "RouteVerified", Message: "waiting for updated ingress verification"}}
	body, err := json.Marshal(c)
	if err != nil {
		return c, err
	}
	runtime, err := json.Marshal(c.Runtime)
	if err != nil {
		return c, err
	}
	if _, err = tx.Exec(ctx, "UPDATE compositions SET generation=$2,phase=$3,body=$4,runtime=$5 WHERE id=$1", c.ID, c.Generation, c.Phase, body, runtime); err != nil {
		return c, unavailable("persist update")
	}
	if err = saveOperation(ctx, tx, c); err != nil {
		return c, err
	}
	if err = tx.Commit(ctx); err != nil {
		return c, unavailable("commit update")
	}
	return c, nil
}
