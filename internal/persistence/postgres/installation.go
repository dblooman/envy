package postgres

import (
	"context"
	"errors"
	"fmt"
	"github.com/dblooman/envy/internal/mesh"
	"github.com/jackc/pgx/v5"
)

// BindInstallation runs before catalog writes and reconciliation. The transaction
// lock serializes simultaneous first boots, including different provider choices.
func (s *Store) BindInstallation(ctx context.Context, installation, provider string) error {
	profile, err := mesh.Resolve(provider)
	if err != nil {
		return err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return unavailable("bind installation")
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(818821)"); err != nil {
		return unavailable("lock installation profile")
	}

	var id, existing string
	err = tx.QueryRow(ctx, "SELECT installation_id, provider FROM installation_profile WHERE singleton").Scan(&id, &existing)
	if errors.Is(err, pgx.ErrNoRows) {
		var populated bool
		if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM projects)").Scan(&populated); err != nil {
			return unavailable("inspect legacy catalog")
		}

		if populated && profile.Name != "istio" {
			return fmt.Errorf("populated legacy installation requires istio; drain experimental compositions with the previous server and use a fresh database for %s", profile.Name)
		}

		if _, err = tx.Exec(ctx, "INSERT INTO installation_profile (installation_id,provider) VALUES ($1,$2)", installation, profile.Name); err != nil {
			return unavailable("persist installation profile")
		}
	} else if err != nil {
		return unavailable("read installation profile")
	} else if id != installation || existing != profile.Name {
		return fmt.Errorf("database belongs to installation %q using %s; live installation/provider switching is unsupported", id, existing)
	}

	if err = tx.Commit(ctx); err != nil {
		return unavailable("commit installation profile")
	}

	return nil
}
