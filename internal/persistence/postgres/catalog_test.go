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
	if got.Runtime.Plan == nil || got.Runtime.Plan.Component.Port != 9090 || got.Runtime.Plan.Component.Env["LISTEN_ADDR"] != ":9090" || got.Runtime.Plan.Baseline.Components["service-a"].ServiceHost != "service-a.orders.svc.cluster.local" {
		t.Fatal("resolved plan was not durably preserved")
	}
}
