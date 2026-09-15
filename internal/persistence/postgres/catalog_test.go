package postgres

import (
	"context"
	"testing"

	"github.com/dblooman/envy/internal/application"
	"github.com/dblooman/envy/internal/domain"
)

func TestCatalogClaimsAndResolvedPlans(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	_, err := s.RegisterProject(ctx, domain.Project{ID: "orders", Name: "Orders"})
	if err != nil {
		t.Fatal(err)
	}

	c := domain.Component{ID: "service-a", Project: "orders", Protocol: "http", Port: 9090, Profile: "http-small", HealthPath: "/healthz", ReadinessPath: "/readyz", Overridable: true, Env: map[string]string{"LISTEN_ADDR": ":9090"}}
	if _, err = s.RegisterComponent(ctx, c); err != nil {
		t.Fatal(err)
	}

	if _, err = s.RegisterComponent(ctx, c); err == nil {
		t.Fatal("duplicate registration accepted")
	} else {
		checkCode(t, err, "conflict")
	}

	b := domain.Baseline{ID: "staging", Project: "orders", Revision: "v1", Endpoint: "http://orders.envy.localhost:8080", Components: map[string]domain.BaselineBinding{"service-a": {ServiceHost: "service-a.orders.svc.cluster.local", Port: 9090, Image: "v1"}}}
	if _, err = s.RegisterBaseline(ctx, b); err != nil {
		t.Fatal(err)
	}

	collision := b
	collision.ID = "other"
	collision.Endpoint = "http://other.envy.localhost:8080"
	if _, err = s.RegisterBaseline(ctx, collision); err == nil {
		t.Fatal("duplicate Service claim accepted")
	} else {
		checkCode(t, err, "conflict")
	}

	if _, err = s.Baseline(ctx, "orders", "other"); err == nil {
		t.Fatal("conflicting baseline partially committed")
	}

	if _, err = s.Component(ctx, "demo", "service-a"); err != nil {
		t.Fatal("same logical ID in another project was affected")
	}

	app := application.New(s, application.Config{})
	created, err := app.Create(ctx, domain.CreateRequest{Project: "orders", Baseline: "staging", Name: "custom", Overrides: map[string]domain.ComponentOverride{"service-a": {Image: "v2"}}}, "")
	if err != nil {
		t.Fatal(err)
	}

	got, err := s.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}

	if got.Runtime.Plan == nil || got.Runtime.Plan.Profiles()["service-a"].Port != 9090 || got.Runtime.Plan.Profiles()["service-a"].Env["LISTEN_ADDR"] != ":9090" || got.Runtime.Plan.Baseline.Components["service-a"].ServiceHost != "service-a.orders.svc.cluster.local" {
		t.Fatal("resolved plan was not durably preserved")
	}
}

func TestCatalogUpgradeRetainsExistingWorkloadIdentity(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	app := application.New(s, application.Config{})
	c, err := app.Create(ctx, request("pre-catalog"), "")
	if err != nil {
		t.Fatal(err)
	}

	c.Runtime.Workload = domain.WorkloadRef{Namespace: "envy-" + c.ID, NamespaceUID: "original-namespace", DeploymentUID: "original-deployment", ServiceUID: "original-service", OwnershipToken: c.Runtime.OwnershipToken}
	if err = s.SaveObservation(ctx, c); err != nil {
		t.Fatal(err)
	}

	// Reproduce the pre-catalog database in this test's isolated schema.
	for _, query := range []string{
		"DROP TABLE baseline_host_claims",
		"DROP INDEX baseline_endpoint_unique",
		"DELETE FROM envy_schema_migrations WHERE name='003_catalog.sql'",
		"UPDATE components SET body=body-'profile'-'readiness_path'",
		"UPDATE baselines SET body=body-'routing'-'verification'",
		"UPDATE compositions SET runtime=runtime-'Plan'",
	} {
		if _, err = s.pool.Exec(ctx, query); err != nil {
			t.Fatal(err)
		}
	}

	if err = s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	got, err := s.Get(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}

	if got.Runtime.Plan == nil || got.Runtime.Plan.Component.Profile != "http-small" || got.Runtime.Plan.Baseline.Routing.Namespace != "envy-baseline" || got.Runtime.Workload != c.Runtime.Workload || got.Runtime.OwnershipToken != c.Runtime.OwnershipToken {
		t.Fatal("migration lost resolved bindings or existing Kubernetes ownership")
	}
}

func TestMultipleOverridePersistenceAndCompleteSetUpdates(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	app := application.New(s, application.Config{})
	req := request("two-overrides")
	req.Overrides["service-a"] = domain.ComponentOverride{Image: "envy/service-a:v2"}
	c, err := app.Create(ctx, req, "multi-idempotency")
	if err != nil {
		t.Fatal(err)
	}

	c.Phase = domain.PhaseReady
	c.Runtime.Workloads = map[string]domain.WorkloadRef{}
	for component := range c.Overrides {
		c.Runtime.Workloads[component] = domain.WorkloadRef{Namespace: domain.NamespaceForID(c.ID), NamespaceUID: "shared-ns", Deployment: component, DeploymentUID: component + "-deployment", Service: component, ServiceUID: component + "-service", OwnershipToken: c.Runtime.OwnershipToken}
	}

	if err = s.SaveObservation(ctx, c); err != nil {
		t.Fatal(err)
	}

	// Complete-set membership changes are supported; reject an unbound component.
	_, err = app.Update(ctx, c.ID, domain.UpdateRequest{ExpectedGeneration: 1, Overrides: map[string]domain.ComponentOverride{"unknown": {Image: "v3"}}})
	checkCode(t, err, "not_found")
	update := domain.UpdateRequest{ExpectedGeneration: 1, Overrides: map[string]domain.ComponentOverride{"service-a": {Image: "envy/service-a:v2"}, "service-b": {Image: "envy/service-b:v3"}}}
	updated, err := app.Update(ctx, c.ID, update)
	if err != nil {
		t.Fatal(err)
	}

	got, err := s.Get(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}

	if got.Generation != 2 || updated.Endpoints["public"].URL != c.Endpoints["public"].URL || len(got.Runtime.Workloads) != 2 || len(got.Runtime.Plan.Profiles()) != 2 {
		t.Fatal("multi-workload state did not survive update and reload")
	}

	for name, ref := range c.Runtime.Workloads {
		if got.Runtime.WorkloadFor(name) != ref {
			t.Fatal("update changed recorded workload identities")
		}
	}

	_, err = app.Update(ctx, c.ID, update)
	checkCode(t, err, "conflict")
	replay, err := app.Create(ctx, req, "multi-idempotency")
	if err != nil || replay.ID != c.ID {
		t.Fatal("multi-override replay did not retain identity")
	}
}
func TestUpgradeSingleWorkloadToMaps(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	app := application.New(s, application.Config{})
	c, err := app.Create(ctx, request("legacy-single"), "")
	if err != nil {
		t.Fatal(err)
	}

	c.Runtime.Plan.Component = c.Runtime.Plan.Profiles()["service-b"]
	c.Runtime.Workload = domain.WorkloadRef{Namespace: domain.NamespaceForID(c.ID), NamespaceUID: "old-namespace", Deployment: "service-b", DeploymentUID: "old-deployment", Service: "service-b", ServiceUID: "old-service", OwnershipToken: c.Runtime.OwnershipToken}
	if err = s.SaveObservation(ctx, c); err != nil {
		t.Fatal(err)
	}

	for _, query := range []string{"DELETE FROM envy_schema_migrations WHERE name='004_multiple_overrides.sql'", "UPDATE compositions SET runtime=jsonb_set(runtime-'Workloads','{Plan}',(runtime->'Plan')-'Components')"} {
		if _, err = s.pool.Exec(ctx, query); err != nil {
			t.Fatal(err)
		}
	}

	if err = s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	got, err := s.Get(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}

	if len(got.Runtime.Workloads) != 1 || got.Runtime.Workloads["service-b"] != c.Runtime.Workload || len(got.Runtime.Plan.Components) != 1 || got.Runtime.Plan.Components["service-b"].ID != "service-b" || got.Generation != c.Generation {
		t.Fatal("legacy migration changed identity or lost profile")
	}
}
