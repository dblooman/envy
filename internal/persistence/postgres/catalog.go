package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/dblooman/envy/internal/domain"
	"github.com/dblooman/envy/internal/persistence/postgres/sqlc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// SeedDemo only inserts absent bindings. Restarting Envy never replaces a
// registered baseline revision or turns live staging into frozen deployments.
func (s *Store) SeedDemo(ctx context.Context) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return unavailable("begin demo seed")
	}
	defer tx.Rollback(ctx)
	qtx := s.queries.WithTx(tx)
	project := domain.Project{ID: "demo", Name: "Envy demo"}
	body, _ := json.Marshal(project)
	if err = qtx.SeedProject(ctx, sqlc.SeedProjectParams{ID: project.ID, Body: body}); err != nil {
		return unavailable("seed project")
	}

	bindings := map[string]domain.BaselineBinding{}
	for _, name := range []string{"gateway", "service-a", "service-b"} {
		c := domain.Component{ID: name, Project: "demo", Protocol: "http", Port: 8080, HealthPath: "/healthz", ReadinessPath: "/readyz", Profile: "http-small", Overridable: true}
		if name == "gateway" {
			c.Env = map[string]string{"DOWNSTREAM_URL": "http://service-a.envy-baseline.svc.cluster.local:8080"}
		}

		if name == "service-a" {
			c.Env = map[string]string{"DOWNSTREAM_URL": "http://service-b.envy-baseline.svc.cluster.local:8080"}
		}

		body, _ = json.Marshal(c)
		if err = qtx.SeedComponent(ctx, sqlc.SeedComponentParams{Project: c.Project, ID: c.ID, Body: body}); err != nil {
			return unavailable("seed component")
		}

		bindings[name] = domain.BaselineBinding{ServiceHost: name + ".envy-baseline.svc.cluster.local", Port: 8080, Image: "envy/" + name + ":v1"}
	}

	b := domain.Baseline{ID: "staging", Project: "demo", Revision: "demo-v1", Endpoint: "http://baseline.envy.localhost:8080", Components: bindings, Routing: domain.BaselineRouting{Namespace: "envy-baseline", Gateway: "envy-preview", EntryComponent: "gateway"}, Verification: domain.VerificationContract{Kind: "envy-chain", Chain: []string{"gateway", "service-a", "service-b"}}}
	body, _ = json.Marshal(b)
	if err = qtx.SeedBaseline(ctx, sqlc.SeedBaselineParams{Project: b.Project, ID: b.ID, Body: body}); err != nil {
		return unavailable("seed baseline")
	}

	for _, binding := range bindings {
		if err = qtx.SeedBaselineHostClaim(ctx, sqlc.SeedBaselineHostClaimParams{Host: binding.ServiceHost, Project: b.Project, Baseline: b.ID}); err != nil {
			return unavailable("seed routing claims")
		}
	}

	if err = tx.Commit(ctx); err != nil {
		return unavailable("commit demo seed")
	}

	return nil
}

func (s *Store) ensureProject(ctx context.Context, project string) error {
	exists, err := s.queries.CheckProjectExists(ctx, project)
	if err != nil {
		return unavailable("read project")
	}

	if !exists {
		return domain.NotFound("project not found")
	}

	return nil
}

func (s *Store) Component(ctx context.Context, project, id string) (domain.Component, error) {
	var c domain.Component
	body, err := s.queries.GetComponent(ctx, sqlc.GetComponentParams{Project: project, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return c, domain.NotFound("component not found in project")
	}

	if err != nil {
		return c, unavailable("read component")
	}

	if err = json.Unmarshal(body, &c); err != nil {
		return c, unavailable("decode component")
	}

	return c, nil
}

func (s *Store) Baseline(ctx context.Context, project, id string) (domain.Baseline, error) {
	var b domain.Baseline
	body, err := s.queries.GetBaseline(ctx, sqlc.GetBaselineParams{Project: project, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return b, domain.NotFound("baseline not found in project")
	}

	if err != nil {
		return b, unavailable("read baseline")
	}

	if err = json.Unmarshal(body, &b); err != nil {
		return b, unavailable("decode baseline")
	}

	return b, nil
}

func (s *Store) Projects(ctx context.Context, after string, limit int) ([]domain.Project, string, error) {
	raw, err := s.queries.ListProjects(ctx, sqlc.ListProjectsParams{ID: after, Limit: int32(limit + 1)})
	if err != nil {
		return nil, "", unavailable("list catalog")
	}

	return decodePage(raw, limit, func(v domain.Project) string { return v.ID })
}

func (s *Store) Components(ctx context.Context, project, after string, limit int) ([]domain.Component, string, error) {
	if err := s.ensureProject(ctx, project); err != nil {
		return nil, "", err
	}

	raw, err := s.queries.ListComponents(ctx, sqlc.ListComponentsParams{Project: project, ID: after, Limit: int32(limit + 1)})
	if err != nil {
		return nil, "", unavailable("list catalog")
	}

	return decodePage(raw, limit, func(v domain.Component) string { return v.ID })
}

func (s *Store) Baselines(ctx context.Context, project, after string, limit int) ([]domain.Baseline, string, error) {
	if err := s.ensureProject(ctx, project); err != nil {
		return nil, "", err
	}

	raw, err := s.queries.ListBaselines(ctx, sqlc.ListBaselinesParams{Project: project, ID: after, Limit: int32(limit + 1)})
	if err != nil {
		return nil, "", unavailable("list catalog")
	}

	return decodePage(raw, limit, func(v domain.Baseline) string { return v.ID })
}

func decodePage[T any](rawItems [][]byte, limit int, id func(T) string) ([]T, string, error) {
	items := make([]T, 0, len(rawItems))
	for _, body := range rawItems {
		var item T
		if err := json.Unmarshal(body, &item); err != nil {
			return nil, "", unavailable("decode catalog row")
		}

		items = append(items, item)
	}

	next := ""
	if len(items) > limit {
		items = items[:limit]
		next = id(items[len(items)-1])
	}

	return items, next, nil
}

func catalogError(err error) error {
	if pg, ok := errors.AsType[*pgconn.PgError](err); ok {
		if pg.Code == "23505" {
			return &domain.Error{Code: "conflict", Message: "catalog ID, endpoint or Service host is already registered"}
		}

		if pg.Code == "23503" {
			return domain.NotFound("project or baseline not found")
		}
	}

	return unavailable("persist catalog registration")
}

func (s *Store) RegisterProject(ctx context.Context, p domain.Project) (domain.Project, error) {
	body, _ := json.Marshal(p)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.Project{}, unavailable("begin project registration")
	}
	defer tx.Rollback(ctx)
	q := s.queries.WithTx(tx)
	err = q.InsertProject(ctx, sqlc.InsertProjectParams{ID: p.ID, Body: body})
	if err != nil {
		return domain.Project{}, catalogError(err)
	}

	if err = insertActivity(ctx, tx, domain.Activity{Action: "catalog.project.register", Outcome: "accepted", Project: p.ID, ResourceType: "project", ResourceID: p.ID, Changes: body}); err != nil {
		return domain.Project{}, err
	}

	if err = tx.Commit(ctx); err != nil {
		return domain.Project{}, catalogError(err)
	}

	return p, nil
}

func (s *Store) RegisterComponent(ctx context.Context, c domain.Component) (domain.Component, error) {
	body, _ := json.Marshal(c)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.Component{}, unavailable("begin component registration")
	}
	defer tx.Rollback(ctx)
	q := s.queries.WithTx(tx)
	err = q.InsertComponent(ctx, sqlc.InsertComponentParams{Project: c.Project, ID: c.ID, Body: body})
	if err != nil {
		return domain.Component{}, catalogError(err)
	}

	if err = insertActivity(ctx, tx, domain.Activity{Action: "catalog.component.register", Outcome: "accepted", Project: c.Project, ResourceType: "component", ResourceID: c.ID, Changes: activityChanges(map[string]any{"id": c.ID, "profile": c.Profile, "protocol": c.Protocol, "port": c.Port, "overridable": c.Overridable})}); err != nil {
		return domain.Component{}, err
	}

	if err = tx.Commit(ctx); err != nil {
		return domain.Component{}, catalogError(err)
	}

	return c, nil
}

func (s *Store) RegisterBaseline(ctx context.Context, b domain.Baseline) (domain.Baseline, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.Baseline{}, unavailable("begin registration")
	}
	defer tx.Rollback(ctx)
	body, _ := json.Marshal(b)
	qtx := s.queries.WithTx(tx)
	if err = qtx.InsertBaseline(ctx, sqlc.InsertBaselineParams{Project: b.Project, ID: b.ID, Body: body}); err != nil {
		return domain.Baseline{}, catalogError(err)
	}

	for _, binding := range b.Components {
		if err = qtx.InsertBaselineHostClaim(ctx, sqlc.InsertBaselineHostClaimParams{Host: binding.ServiceHost, Project: b.Project, Baseline: b.ID}); err != nil {
			return domain.Baseline{}, catalogError(err)
		}
	}

	if err = insertActivity(ctx, tx, domain.Activity{Action: "catalog.baseline.register", Outcome: "accepted", Project: b.Project, ResourceType: "baseline", ResourceID: b.ID, Changes: activityChanges(map[string]any{"id": b.ID, "revision": b.Revision, "endpoint": b.Endpoint, "component_count": len(b.Components)})}); err != nil {
		return domain.Baseline{}, err
	}

	if err = tx.Commit(ctx); err != nil {
		return domain.Baseline{}, catalogError(err)
	}

	return b, nil
}
