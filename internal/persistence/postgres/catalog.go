package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/dblooman/envy/internal/domain"
	"github.com/jackc/pgx/v5"
)

// SeedDemo only inserts absent bindings. Restarting Envy never replaces a
// registered baseline revision or turns live staging into frozen deployments.
func (s *Store) SeedDemo(ctx context.Context) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return unavailable("begin demo seed")
	}
	defer tx.Rollback(ctx)
	project := domain.Project{ID: "demo", Name: "Envy demo"}
	body, _ := json.Marshal(project)
	if _, err = tx.Exec(ctx, "INSERT INTO projects(id,body) VALUES($1,$2) ON CONFLICT DO NOTHING", project.ID, body); err != nil {
		return unavailable("seed project")
	}
	bindings := map[string]domain.BaselineBinding{}
	for _, name := range []string{"gateway", "service-a", "service-b"} {
		c := domain.Component{ID: name, Project: "demo", Protocol: "http", Port: 8080, HealthPath: "/healthz", Overridable: name == "service-b"}
		body, _ = json.Marshal(c)
		if _, err = tx.Exec(ctx, "INSERT INTO components(project,id,body) VALUES($1,$2,$3) ON CONFLICT DO NOTHING", c.Project, c.ID, body); err != nil {
			return unavailable("seed component")
		}
		bindings[name] = domain.BaselineBinding{ServiceHost: name + ".envy-baseline.svc.cluster.local", Port: 8080, Image: "envy/" + name + ":v1"}
	}
	b := domain.Baseline{ID: "staging", Project: "demo", Revision: "demo-v1", Endpoint: "http://baseline.envy.localhost:8080", Components: bindings}
	body, _ = json.Marshal(b)
	if _, err = tx.Exec(ctx, "INSERT INTO baselines(project,id,body) VALUES($1,$2,$3) ON CONFLICT DO NOTHING", b.Project, b.ID, body); err != nil {
		return unavailable("seed baseline")
	}
	if err = tx.Commit(ctx); err != nil {
		return unavailable("commit demo seed")
	}
	return nil
}
func (s *Store) ensureProject(ctx context.Context, project string) error {
	var exists bool
	if err := s.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM projects WHERE id=$1)", project).Scan(&exists); err != nil {
		return unavailable("read project")
	}
	if !exists {
		return domain.NotFound("project not found")
	}
	return nil
}
func (s *Store) Component(ctx context.Context, project, id string) (domain.Component, error) {
	var c domain.Component
	var body []byte
	err := s.pool.QueryRow(ctx, "SELECT body FROM components WHERE project=$1 AND id=$2", project, id).Scan(&body)
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
	var body []byte
	err := s.pool.QueryRow(ctx, "SELECT body FROM baselines WHERE project=$1 AND id=$2", project, id).Scan(&body)
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
	return catalogPage[domain.Project](ctx, s, "SELECT body FROM projects WHERE id>$1 ORDER BY id LIMIT $2", limit, func(v domain.Project) string { return v.ID }, after, limit+1)
}
func (s *Store) Components(ctx context.Context, project, after string, limit int) ([]domain.Component, string, error) {
	if err := s.ensureProject(ctx, project); err != nil {
		return nil, "", err
	}
	return catalogPage[domain.Component](ctx, s, "SELECT body FROM components WHERE project=$1 AND id>$2 ORDER BY id LIMIT $3", limit, func(v domain.Component) string { return v.ID }, project, after, limit+1)
}
func (s *Store) Baselines(ctx context.Context, project, after string, limit int) ([]domain.Baseline, string, error) {
	if err := s.ensureProject(ctx, project); err != nil {
		return nil, "", err
	}
	return catalogPage[domain.Baseline](ctx, s, "SELECT body FROM baselines WHERE project=$1 AND id>$2 ORDER BY id LIMIT $3", limit, func(v domain.Baseline) string { return v.ID }, project, after, limit+1)
}
func catalogPage[T any](ctx context.Context, s *Store, query string, limit int, id func(T) string, args ...any) ([]T, string, error) {
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, "", unavailable("list catalog")
	}
	defer rows.Close()
	items := make([]T, 0)
	for rows.Next() {
		var body []byte
		if err = rows.Scan(&body); err != nil {
			return nil, "", unavailable("read catalog row")
		}
		var item T
		if err = json.Unmarshal(body, &item); err != nil {
			return nil, "", unavailable("decode catalog row")
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		return nil, "", unavailable("list catalog")
	}
	next := ""
	if len(items) > limit {
		items = items[:limit]
		next = id(items[len(items)-1])
	}
	return items, next, nil
}
