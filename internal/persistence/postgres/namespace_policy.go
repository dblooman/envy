package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// BindNamespacePolicy preserves upgraded installations and fences configuration
// changes against a running reconciler. Operators drain and stop all replicas.
func (s *Store) BindNamespacePolicy(ctx context.Context, mode, fingerprint string) (string, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", unavailable("bind namespace policy")
	}

	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(818821)"); err != nil {
		return "", unavailable("lock namespace policy")
	}

	var existing, old string
	if err = tx.QueryRow(ctx, "SELECT namespace_policy_mode, namespace_policy_fingerprint FROM installation_profile WHERE singleton FOR UPDATE").Scan(&existing, &old); err != nil {
		return "", unavailable("read namespace policy")
	}

	if mode == "" {
		mode = existing
	}

	if mode != "legacy" && mode != "isolated" {
		return "", fmt.Errorf("invalid namespace policy mode")
	}

	if mode != existing || (old != "" && old != fingerprint) {
		if err := checkPolicyTransition(ctx, tx); err != nil {
			return "", err
		}
	}

	if _, err = tx.Exec(ctx, "UPDATE installation_profile SET namespace_policy_mode=$1, namespace_policy_fingerprint=$2 WHERE singleton", mode, fingerprint); err != nil {
		return "", unavailable("save namespace policy")
	}

	if err = tx.Commit(ctx); err != nil {
		return "", unavailable("commit namespace policy")
	}

	return mode, nil
}

func (s *Store) CheckNamespacePolicy(ctx context.Context, mode, fingerprint string) error {
	var matches bool
	if err := s.pool.QueryRow(ctx, "SELECT namespace_policy_mode=$1 AND namespace_policy_fingerprint=$2 FROM installation_profile WHERE singleton", mode, fingerprint).Scan(&matches); err != nil {
		return unavailable("check namespace policy")
	}

	if !matches {
		return fmt.Errorf("installation namespace policy changed; restart this replica")
	}

	return nil
}

func checkPolicyTransition(ctx context.Context, tx pgx.Tx) error {
	var locked bool
	if err := tx.QueryRow(ctx, "SELECT pg_try_advisory_xact_lock($1)", leaseID).Scan(&locked); err != nil {
		return unavailable("lock policy transition")
	}

	if !locked {
		return fmt.Errorf("stop reconciler replicas before changing namespace policy")
	}

	// Excludes new composition writes while checking the drain boundary.
	if _, err := tx.Exec(ctx, "LOCK TABLE compositions IN SHARE ROW EXCLUSIVE MODE"); err != nil {
		return unavailable("lock policy transition")
	}

	var active bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM compositions WHERE phase <> 'destroyed')").Scan(&active); err != nil {
		return unavailable("check policy transition")
	}

	if active {
		return fmt.Errorf("drain active previews before changing namespace policy")
	}

	return nil
}
