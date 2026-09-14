// Package postgres stores canonical desired state, observations, and operations.
package postgres

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"maps"
	"time"

	"github.com/dblooman/envy/internal/domain"
	"github.com/dblooman/envy/internal/persistence/postgres/sqlc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrations embed.FS

type Store struct {
	pool    *pgxpool.Pool
	queries *sqlc.Queries
}

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
	return &Store{pool: p, queries: sqlc.New(p)}, nil
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
	qtx := s.queries.WithTx(tx)
	if err = qtx.AdvisoryXactLock(ctx, 818820); err != nil {
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
		exists, err := qtx.CheckMigrationApplied(ctx, e.Name())
		if err != nil {
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
		if err = qtx.RecordMigration(ctx, e.Name()); err != nil {
			return unavailable("record migration")
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return unavailable("commit migrations")
	}
	return nil
}

func decodeComposition(body, runtime []byte, deletion bool) (domain.Composition, error) {
	var c domain.Composition
	if err := json.Unmarshal(body, &c); err != nil {
		return c, unavailable("decode composition")
	}
	if err := json.Unmarshal(runtime, &c.Runtime); err != nil {
		return c, unavailable("decode runtime state")
	}
	c.DeletionRequested = deletion
	// Records written before evolving compositions shipped did not distinguish
	// desired and published selections. Their current desired map was the only
	// selection ever routed, so it is the safe migration value.
	c.RefreshPreviewProvenance()
	if c.Runtime.PublishedOverrides == nil {
		c.Runtime.PublishedOverrides = copyOverrides(c.Overrides)
	}
	if c.Runtime.RetiringWorkloads == nil {
		c.Runtime.RetiringWorkloads = map[string]domain.WorkloadRef{}
	}
	return c, nil
}

func copyOverrides(in map[string]domain.ComponentOverride) map[string]domain.ComponentOverride {
	out := make(map[string]domain.ComponentOverride, len(in))
	maps.Copy(out, in)
	return out
}

func (s *Store) Get(ctx context.Context, id string) (domain.Composition, error) {
	row, err := s.queries.GetComposition(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Composition{}, domain.NotFound("composition not found")
	}
	if err != nil {
		return domain.Composition{}, unavailable("read composition")
	}
	return decodeComposition(row.Body, row.Runtime, row.DeletionRequested)
}
func (s *Store) Create(ctx context.Context, c domain.Composition, key, hash string, max int) (domain.Composition, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return c, unavailable("begin create")
	}
	defer tx.Rollback(ctx)
	qtx := s.queries.WithTx(tx)
	// The transaction lock serializes idempotency and the live composition cap
	// across API replicas, without holding the reconciler lease.
	if err = qtx.AdvisoryXactLock(ctx, 818819); err != nil {
		return c, unavailable("lock create")
	}
	if key != "" {
		idempotencyRow, err := qtx.GetIdempotencyKey(ctx, key)
		if err == nil {
			if idempotencyRow.RequestHash != hash {
				return c, &domain.Error{Code: "conflict", Message: "idempotency key was already used with a different request"}
			}
			row, err := qtx.GetComposition(ctx, idempotencyRow.CompositionID)
			if errors.Is(err, pgx.ErrNoRows) {
				return c, domain.NotFound("composition not found")
			}
			if err != nil {
				return c, unavailable("read composition")
			}
			return decodeComposition(row.Body, row.Runtime, row.DeletionRequested)
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return c, unavailable("read idempotency key")
		}
	}
	if err = validateBuildOverrides(ctx, qtx, c.Project, c.Overrides); err != nil {
		return c, err
	}
	count, err := qtx.CountActiveCompositions(ctx)
	if err != nil {
		return c, unavailable("check composition capacity")
	}
	if int(count) >= max {
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
	if err = qtx.InsertComposition(ctx, sqlc.InsertCompositionParams{
		ID:                c.ID,
		Project:           c.Project,
		Baseline:          c.Baseline,
		Generation:        c.Generation,
		DeletionRequested: c.DeletionRequested,
		Phase:             string(c.Phase),
		ExpiresAt:         pgtype.Timestamptz{Time: c.ExpiresAt, Valid: true},
		Body:              body,
		Runtime:           runtime,
	}); err != nil {
		return c, unavailable("persist composition")
	}
	if key != "" {
		if err = qtx.InsertIdempotencyKey(ctx, sqlc.InsertIdempotencyKeyParams{
			Key:           key,
			RequestHash:   hash,
			CompositionID: c.ID,
		}); err != nil {
			return c, unavailable("persist idempotency key")
		}
	}
	if err = saveOperation(ctx, qtx, c); err != nil {
		return c, err
	}
	if err = insertRevision(ctx, tx, c); err != nil {
		return c, err
	}
	if err = insertActivity(ctx, tx, domain.Activity{Action: "composition.create", Outcome: "accepted", Project: c.Project, ResourceType: "composition", ResourceID: c.ID, Composition: c.ID, Operation: c.LatestOperation.ID, GenerationTo: c.Generation, Changes: activityChanges(map[string]any{"name": c.Name, "baseline": c.Baseline, "baseline_revision": c.BaselineRevision, "overrides": c.Overrides, "expires_at": c.ExpiresAt})}); err != nil {
		return c, err
	}
	if err = tx.Commit(ctx); err != nil {
		return c, unavailable("commit composition")
	}
	return c, nil
}
func saveOperation(ctx context.Context, qtx *sqlc.Queries, c domain.Composition) error {
	body, err := json.Marshal(c.LatestOperation)
	if err != nil {
		return err
	}
	if err = qtx.UpsertOperation(ctx, sqlc.UpsertOperationParams{
		ID:            c.LatestOperation.ID,
		CompositionID: c.ID,
		Body:          body,
	}); err != nil {
		return unavailable("persist operation")
	}
	return nil
}
func (s *Store) List(ctx context.Context, project, after string, limit int) ([]domain.Composition, string, error) {
	rows, err := s.queries.ListCompositions(ctx, sqlc.ListCompositionsParams{
		Project: project,
		After:   after,
		Limit:   int32(limit + 1),
	})
	if err != nil {
		return nil, "", unavailable("list compositions")
	}
	items := make([]domain.Composition, 0, len(rows))
	for _, row := range rows {
		c, err := decodeComposition(row.Body, row.Runtime, row.DeletionRequested)
		if err != nil {
			return nil, "", err
		}
		items = append(items, c)
	}
	next := ""
	if len(items) > limit {
		items = items[:limit]
		next = items[len(items)-1].ID
	}
	return items, next, nil
}
func (s *Store) Active(ctx context.Context) ([]domain.Composition, error) {
	rows, err := s.queries.ListActiveCompositions(ctx)
	if err != nil {
		return nil, unavailable("scan active compositions")
	}
	items := make([]domain.Composition, 0, len(rows))
	for _, row := range rows {
		c, err := decodeComposition(row.Body, row.Runtime, row.DeletionRequested)
		if err != nil {
			return nil, err
		}
		items = append(items, c)
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
	qtx := s.queries.WithTx(tx)
	rowsAffected, err := qtx.UpdateCompositionObservation(ctx, sqlc.UpdateCompositionObservationParams{
		ID:                c.ID,
		Phase:             string(c.Phase),
		Body:              body,
		Runtime:           runtime,
		Generation:        c.Generation,
		DeletionRequested: c.DeletionRequested,
	})
	if err != nil {
		return unavailable("persist observation")
	}
	if rowsAffected != 1 {
		return domain.ErrStaleObservation
	}
	if err = saveOperation(ctx, qtx, c); err != nil {
		return err
	}
	if c.LatestOperation.Status == "succeeded" || c.LatestOperation.Status == "failed" {
		systemContext := domain.WithRequestIdentity(ctx, domain.RequestIdentity{Principal: domain.Principal{Kind: "system", ID: "reconciler", DisplayName: "Reconciler"}, Channel: "system"})
		if err = insertActivity(systemContext, tx, domain.Activity{Action: "composition." + c.LatestOperation.Kind, Outcome: c.LatestOperation.Status, Project: c.Project, ResourceType: "composition", ResourceID: c.ID, Composition: c.ID, Operation: c.LatestOperation.ID, GenerationTo: c.Generation, Changes: activityChanges(map[string]any{"phase": c.Phase, "initiator": c.LatestOperation.Initiator, "error": c.LatestOperation.Error})}); err != nil {
			return err
		}
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
	qtx := s.queries.WithTx(tx)
	row, err := qtx.GetCompositionForUpdate(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Composition{}, domain.NotFound("composition not found")
	}
	if err != nil {
		return domain.Composition{}, unavailable("read composition")
	}
	c, err := decodeComposition(row.Body, row.Runtime, row.DeletionRequested)
	if err != nil {
		return c, err
	}
	if c.DeletionRequested || c.Phase == domain.PhaseDestroyed {
		return c, nil
	}
	previousGeneration := c.Generation
	if err = requestDeletion(&c, time.Now().UTC(), "requested"); err != nil {
		return c, err
	}
	identity := activityIdentity(ctx)
	c.LatestOperation.Initiator = &identity.Principal
	if err = writeDeletion(ctx, qtx, c); err != nil {
		return c, err
	}
	if err = insertRevision(ctx, tx, c); err != nil {
		return c, err
	}
	if err = insertActivity(ctx, tx, domain.Activity{Action: "composition.destroy", Outcome: "accepted", Project: c.Project, ResourceType: "composition", ResourceID: c.ID, Composition: c.ID, Operation: c.LatestOperation.ID, GenerationFrom: previousGeneration, GenerationTo: c.Generation}); err != nil {
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
	c.VerificationLevel = "none"
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
func writeDeletion(ctx context.Context, qtx *sqlc.Queries, c domain.Composition) error {
	body, err := json.Marshal(c)
	if err != nil {
		return err
	}
	runtime, err := json.Marshal(c.Runtime)
	if err != nil {
		return err
	}
	if err = qtx.UpdateCompositionDeletion(ctx, sqlc.UpdateCompositionDeletionParams{
		ID:         c.ID,
		Generation: c.Generation,
		Phase:      string(c.Phase),
		Body:       body,
		Runtime:    runtime,
	}); err != nil {
		return unavailable("persist deletion")
	}
	return saveOperation(ctx, qtx, c)
}
func (s *Store) Expire(ctx context.Context, now time.Time) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return unavailable("begin expiry")
	}
	defer tx.Rollback(ctx)
	qtx := s.queries.WithTx(tx)
	rows, err := qtx.ListExpiredCompositionsForUpdate(ctx, pgtype.Timestamptz{Time: now, Valid: true})
	if err != nil {
		return unavailable("scan expired compositions")
	}
	var expired []domain.Composition
	for _, row := range rows {
		c, err := decodeComposition(row.Body, row.Runtime, row.DeletionRequested)
		if err != nil {
			return err
		}
		expired = append(expired, c)
	}
	for _, c := range expired {
		ctx = domain.WithRequestIdentity(ctx, domain.RequestIdentity{Principal: domain.Principal{Kind: "system", ID: "expiry-controller", DisplayName: "Expiry controller"}, Channel: "system"})
		previousGeneration := c.Generation
		if err = requestDeletion(&c, now, "expired"); err != nil {
			return err
		}
		identity := activityIdentity(ctx)
		c.LatestOperation.Initiator = &identity.Principal
		if err = writeDeletion(ctx, qtx, c); err != nil {
			return err
		}
		if err = insertRevision(ctx, tx, c); err != nil {
			return err
		}
		if err = insertActivity(ctx, tx, domain.Activity{Action: "composition.expire", Outcome: "accepted", Project: c.Project, ResourceType: "composition", ResourceID: c.ID, Composition: c.ID, Operation: c.LatestOperation.ID, GenerationFrom: previousGeneration, GenerationTo: c.Generation}); err != nil {
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
	qtx := s.queries.WithTx(tx)
	// Serialize keyed retries before reading generation. This lets a caller
	// recover an accepted update even though the original generation is now stale.
	if err = qtx.AdvisoryXactLock(ctx, 919820); err != nil {
		return domain.Composition{}, unavailable("lock update")
	}
	var requestHash string
	if req.IdempotencyKey != "" {
		canonical := struct {
			ExpectedPreviewRevisions map[string]int64                    `json:"expected_preview_revisions,omitempty"`
			ExpectedGeneration       int64                               `json:"expected_generation"`
			Overrides                map[string]domain.ComponentOverride `json:"overrides"`
		}{ExpectedPreviewRevisions: req.ExpectedPreviewRevisions, ExpectedGeneration: req.ExpectedGeneration, Overrides: req.Overrides}
		encoded, e := json.Marshal(canonical)
		if e != nil {
			return domain.Composition{}, e
		}
		sum := sha256.Sum256(encoded)
		requestHash = hex.EncodeToString(sum[:])
		var previousHash, operation string
		e = tx.QueryRow(ctx, `SELECT request_hash, operation_id FROM composition_update_idempotency WHERE composition_id=$1 AND key=$2`, id, req.IdempotencyKey).Scan(&previousHash, &operation)
		if e == nil {
			if previousHash != requestHash {
				return domain.Composition{}, &domain.Error{Code: "conflict", Message: "idempotency key was already used with a different update", Composition: id}
			}
			row, readErr := qtx.GetComposition(ctx, id)
			if readErr != nil {
				return domain.Composition{}, unavailable("read idempotent update")
			}
			return decodeComposition(row.Body, row.Runtime, row.DeletionRequested)
		}
		if !errors.Is(e, pgx.ErrNoRows) {
			return domain.Composition{}, unavailable("read update idempotency")
		}
	}
	row, err := qtx.GetCompositionForUpdate(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Composition{}, domain.NotFound("composition not found")
	}
	if err != nil {
		return domain.Composition{}, unavailable("read composition")
	}
	c, err := decodeComposition(row.Body, row.Runtime, row.DeletionRequested)
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
	if err = validateBuildOverrides(ctx, qtx, c.Project, req.Overrides); err != nil {
		return c, err
	}
	beforeOverrides := copyOverrides(c.Overrides)
	c.Generation++
	c.Overrides = req.Overrides
	if req.Plan != nil {
		c.Runtime.Plan = req.Plan
		c.RefreshPreviewProvenance()
	}
	c.Phase = domain.PhaseUpdating
	c.VerificationLevel = "none"
	c.UpdatedAt = now
	c.LastError = nil
	c.LatestOperation = domain.Operation{ID: operationID, Kind: "update", Status: "pending"}
	identity := activityIdentity(ctx)
	c.LatestOperation.Initiator = &identity.Principal
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
	if err = qtx.UpdateCompositionDesired(ctx, sqlc.UpdateCompositionDesiredParams{
		ID:         c.ID,
		Generation: c.Generation,
		Phase:      string(c.Phase),
		Body:       body,
		Runtime:    runtime,
	}); err != nil {
		return c, unavailable("persist update")
	}
	if err = saveOperation(ctx, qtx, c); err != nil {
		return c, err
	}
	if req.IdempotencyKey != "" {
		if _, err = tx.Exec(ctx, `INSERT INTO composition_update_idempotency(composition_id, key, request_hash, operation_id) VALUES ($1,$2,$3,$4)`, c.ID, req.IdempotencyKey, requestHash, c.LatestOperation.ID); err != nil {
			return c, unavailable("persist update idempotency")
		}
	}
	if err = insertRevision(ctx, tx, c); err != nil {
		return c, err
	}
	if err = insertActivity(ctx, tx, domain.Activity{Action: "composition.update", Outcome: "accepted", Project: c.Project, ResourceType: "composition", ResourceID: c.ID, Composition: c.ID, Operation: c.LatestOperation.ID, GenerationFrom: req.ExpectedGeneration, GenerationTo: c.Generation, Changes: activityChanges(map[string]any{"before_overrides": beforeOverrides, "after_overrides": c.Overrides})}); err != nil {
		return c, err
	}
	if err = tx.Commit(ctx); err != nil {
		return c, unavailable("commit update")
	}
	return c, nil
}
