package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/dblooman/envy/internal/domain"
	"github.com/dblooman/envy/internal/persistence/postgres/sqlc"
	"github.com/jackc/pgx/v5"
	"reflect"
	"slices"
)

func matchingCatalog(body []byte, err error, want any, label string, required bool) error {
	if errors.Is(err, pgx.ErrNoRows) && !required {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return &domain.Error{Code: "conflict", Message: label + " conflicts with an existing registration"}
	}
	if err != nil {
		return unavailable("read onboarding catalog")
	}
	var got, expected any
	data, _ := json.Marshal(want)
	if json.Unmarshal(body, &got) != nil || json.Unmarshal(data, &expected) != nil {
		return unavailable("decode onboarding catalog")
	}
	if !reflect.DeepEqual(got, expected) {
		return &domain.Error{Code: "conflict", Message: label + " already exists with different immutable configuration"}
	}
	return nil
}
func (s *Store) CheckCatalog(ctx context.Context, m domain.CatalogManifest) error {
	return catalogBundle(ctx, s.queries, m, false)
}
func (s *Store) ApplyCatalog(ctx context.Context, m domain.CatalogManifest) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return unavailable("begin onboarding")
	}
	defer tx.Rollback(ctx)
	if err = catalogBundle(ctx, s.queries.WithTx(tx), m, true); err != nil {
		return err
	}
	if err = insertActivity(ctx, tx, domain.Activity{Action: "catalog.apply", Outcome: "accepted", Project: m.Project.ID, ResourceType: "catalog", ResourceID: m.Project.ID, Changes: activityChanges(map[string]any{"project": m.Project.ID, "baseline": m.Baseline.ID, "component_count": len(m.Components)})}); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return catalogError(err)
	}
	return nil
}
func catalogBundle(ctx context.Context, q *sqlc.Queries, m domain.CatalogManifest, apply bool) error {
	data, _ := json.Marshal(m.Project)
	if apply {
		if err := q.SeedProject(ctx, sqlc.SeedProjectParams{ID: m.Project.ID, Body: data}); err != nil {
			return catalogError(err)
		}
	}
	body, err := q.GetProject(ctx, m.Project.ID)
	if err = matchingCatalog(body, err, m.Project, "project "+m.Project.ID, apply); err != nil {
		return err
	}
	for _, c := range m.Components {
		data, _ = json.Marshal(c)
		if apply {
			if err = q.SeedComponent(ctx, sqlc.SeedComponentParams{Project: c.Project, ID: c.ID, Body: data}); err != nil {
				return catalogError(err)
			}
		}
		body, err = q.GetComponent(ctx, sqlc.GetComponentParams{Project: c.Project, ID: c.ID})
		if err = matchingCatalog(body, err, c, "component "+c.ID, apply); err != nil {
			return err
		}
	}
	b := m.Baseline
	owner, err := q.GetBaselineEndpointOwner(ctx, b.Endpoint)
	if err == nil && (owner.Project != b.Project || owner.ID != b.ID) {
		return &domain.Error{Code: "conflict", Message: "baseline endpoint is already registered"}
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return unavailable("read baseline endpoint ownership")
	}
	data, _ = json.Marshal(b)
	if apply {
		if err = q.SeedBaseline(ctx, sqlc.SeedBaselineParams{Project: b.Project, ID: b.ID, Body: data}); err != nil {
			return catalogError(err)
		}
	}
	body, err = q.GetBaseline(ctx, sqlc.GetBaselineParams{Project: b.Project, ID: b.ID})
	if err = matchingCatalog(body, err, b, "baseline "+b.ID, apply); err != nil {
		return err
	}
	// Stable host order prevents competing bundles from taking claim locks in opposite order.
	hosts := make([]string, 0, len(b.Components))
	for _, binding := range b.Components {
		hosts = append(hosts, binding.ServiceHost)
	}
	slices.Sort(hosts)
	for _, host := range hosts {
		if apply {
			if err = q.SeedBaselineHostClaim(ctx, sqlc.SeedBaselineHostClaimParams{Host: host, Project: b.Project, Baseline: b.ID}); err != nil {
				return catalogError(err)
			}
		}
		claim, err := q.GetBaselineHostClaim(ctx, host)
		if errors.Is(err, pgx.ErrNoRows) && !apply {
			continue
		}
		if err != nil {
			return unavailable("read routing ownership")
		}
		if claim.Project != b.Project || claim.Baseline != b.ID {
			return &domain.Error{Code: "conflict", Message: "Service host " + host + " is already registered to another baseline"}
		}
	}
	return nil
}
