//nolint:wsl_v5 // Fixtures are intentionally compact to focus each catalog contract.
package application

import (
	"context"
	"errors"
	"testing"

	"github.com/dblooman/envy/internal/domain"
)

func approvedComponent() domain.Component {
	return domain.Component{ID: "worker", Project: "orders", Protocol: "http", Port: 8080, HealthPath: "/healthz", ReadinessPath: "/readyz", Profile: "http-small", Overridable: true}
}
func TestApprovedProfileBounds(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*domain.Component)
		ok     bool
	}{
		{"approved", func(c *domain.Component) {
			c.Env = map[string]string{"DOWNSTREAM_URL": "http://worker.orders.svc.cluster.local:8080"}
		}, true},
		{"privileged port", func(c *domain.Component) { c.Port = 80 }, false},
		{"unsupported profile", func(c *domain.Component) { c.Profile = "privileged" }, false},
		{"encrypted protocol", func(c *domain.Component) { c.Protocol = "https" }, false},
		{"health URL", func(c *domain.Component) { c.HealthPath = "http://other/health" }, false},
		{"reserved context", func(c *domain.Component) { c.Env = map[string]string{"ENVY_COMPOSITION_ID": "forged"} }, false},
		{"reserved identity", func(c *domain.Component) { c.Env = map[string]string{"POD_UID": "forged"} }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := approvedComponent()
			tc.change(&c)
			if (ValidateComponent(c) == nil) != tc.ok {
				t.Fatal("unexpected validation outcome")
			}
		})
	}
}

type catalogFixture struct {
	Repository
	writes      int
	composition domain.Composition
}

type jobCatalogFixture struct{ catalogFixture }

func (f *jobCatalogFixture) Component(_ context.Context, project, id string) (domain.Component, error) {
	if project != "orders" || id != "report" {
		return domain.Component{}, domain.NotFound("component")
	}
	return domain.Component{ID: "report", Project: "orders", Profile: "job", Overridable: true, Execution: &domain.WorkloadExecution{Kind: domain.WorkloadJob, Timeout: "1m", RetryLimit: 1}}, nil
}

func (f *catalogFixture) Component(_ context.Context, project, id string) (domain.Component, error) {
	c := approvedComponent()
	if project != c.Project || id != c.ID {
		return domain.Component{}, domain.NotFound("component")
	}

	return c, nil
}
func (f *catalogFixture) RegisterBaseline(_ context.Context, b domain.Baseline) (domain.Baseline, error) {
	f.writes++
	return b, nil
}
func (f *catalogFixture) RegisterComponent(_ context.Context, c domain.Component) (domain.Component, error) {
	return c, nil
}
func (f *catalogFixture) RegisterProject(_ context.Context, p domain.Project) (domain.Project, error) {
	return p, nil
}
func (f *catalogFixture) Get(context.Context, string) (domain.Composition, error) {
	return f.composition, nil
}

type rejectBaseline struct{ calls int }

func (v *rejectBaseline) ValidateBaseline(context.Context, domain.Baseline, map[string]domain.Component) error {
	v.calls++
	return domain.Validation("not connected")
}
func TestBaselineRejectsBeforePersistence(t *testing.T) {
	f := &catalogFixture{}
	v := &rejectBaseline{}
	s := New(f, Config{CatalogValidator: v})
	b := domain.Baseline{ID: "staging", Project: "orders", Revision: "v1", Endpoint: "http://orders.envy.localhost:8080", Routing: domain.BaselineRouting{Namespace: "orders", Gateway: "preview", EntryComponent: "worker"}, Verification: domain.VerificationContract{Kind: "envy-chain", Chain: []string{"worker"}}, Components: map[string]domain.BaselineBinding{"worker": {ServiceHost: "worker.orders.svc.cluster.local", Port: 8080, Image: "worker:v1"}}}
	if _, err := s.RegisterBaseline(context.Background(), b); err == nil || v.calls != 1 || f.writes != 0 {
		t.Fatal("unverified baseline persisted")
	}

	b.Project = "other"
	if _, err := s.RegisterBaseline(context.Background(), b); err == nil || v.calls != 1 {
		t.Fatal("cross-project component accepted")
	}
}

func TestEndpointFreeJobBaselineUsesImageOnlyBinding(t *testing.T) {
	f := &jobCatalogFixture{}
	validator := &connectedBaseline{}
	s := New(f, Config{CatalogValidator: validator})
	b := domain.Baseline{ID: "jobs", Project: "orders", Revision: "v1", Routing: domain.BaselineRouting{Namespace: "orders"}, Verification: domain.VerificationContract{Kind: "none"}, Components: map[string]domain.BaselineBinding{"report": {Image: "example/report:v1"}}}
	if _, err := s.RegisterBaseline(context.Background(), b); err != nil {
		t.Fatal(err)
	}
	if f.writes != 1 || validator.calls != 1 {
		t.Fatalf("endpoint-free baseline was not registered: writes=%d checks=%d", f.writes, validator.calls)
	}
	b.Endpoint = "https://should-not-exist.example"
	if _, err := s.RegisterBaseline(context.Background(), b); err == nil {
		t.Fatal("endpoint-free baseline accepted a public endpoint")
	}
}

func TestUpdateRequiresResolvedPlan(t *testing.T) {
	f := &catalogFixture{composition: domain.Composition{Project: "orders", Overrides: map[string]domain.ComponentOverride{"existing": {Image: "old"}}}}
	_, err := New(f, Config{}).Update(context.Background(), "id", domain.UpdateRequest{ExpectedGeneration: 1, Overrides: map[string]domain.ComponentOverride{"worker": {Image: "new"}}})
	var e *domain.Error
	if !errors.As(err, &e) || e.Code != "conflict" {
		t.Fatalf("resolved plan error: %v", err)
	}
}

func TestHTTPSBaselineReachesProviderValidation(t *testing.T) {
	f := &catalogFixture{}
	v := &rejectBaseline{}
	s := New(f, Config{PreviewBaseURL: "https://envy.test", CatalogValidator: v})
	b := domain.Baseline{ID: "staging", Project: "orders", Revision: "v1", Endpoint: "https://orders.envy.test", Routing: domain.BaselineRouting{Namespace: "orders", Gateway: "preview", EntryComponent: "worker"}, Verification: domain.VerificationContract{Kind: "envy-chain", Chain: []string{"worker"}}, Components: map[string]domain.BaselineBinding{"worker": {ServiceHost: "worker.orders.svc.cluster.local", Port: 8080, Image: "worker:v1"}}}
	if _, err := s.RegisterBaseline(context.Background(), b); err == nil || v.calls != 1 {
		t.Fatalf("HTTPS did not reach provider verification: %v", err)
	}

	b.Endpoint = "http://orders.envy.test"
	if _, err := s.RegisterBaseline(context.Background(), b); err == nil || v.calls != 1 {
		t.Fatalf("scheme mismatch accepted: %v", err)
	}
}

func TestComponentPullSecretsRequireOperatorApproval(t *testing.T) {
	c := approvedComponent()
	c.ImagePullSecrets = []string{"registry"}
	app := New(&catalogFixture{}, Config{})
	if _, err := app.RegisterComponent(context.Background(), c); err == nil {
		t.Fatal("unapproved pull Secret accepted")
	}

	app = New(&catalogFixture{}, Config{ApprovedImagePullSecrets: []string{"registry"}})
	if _, err := app.RegisterComponent(context.Background(), c); err != nil {
		t.Fatal(err)
	}

	c.ImagePullSecrets = []string{"registry", "registry"}
	if err := ValidateComponent(c); err == nil {
		t.Fatal("duplicate pull Secret accepted")
	}

	c.ImagePullSecrets = []string{"../registry"}
	if err := ValidateComponent(c); err == nil {
		t.Fatal("invalid pull Secret accepted")
	}
}

func TestBackgroundComponentValidation(t *testing.T) {
	valid := []domain.Component{
		{ID: "worker", Project: "orders", Profile: "worker", Execution: &domain.WorkloadExecution{Kind: domain.WorkloadWorker}, Overridable: true},
		{ID: "export", Project: "orders", Profile: "job", Execution: &domain.WorkloadExecution{Kind: domain.WorkloadJob, Timeout: "5m", RetryLimit: 1}, Overridable: true},
		{ID: "nightly", Project: "orders", Profile: "scheduled-job", Execution: &domain.WorkloadExecution{Kind: domain.WorkloadScheduledJob, Timeout: "5m", Schedule: "0 1 * * *", MaxRuns: 7, ConcurrencyPolicy: "forbid"}, Overridable: true},
	}
	for _, component := range valid {
		if err := ValidateComponent(component); err != nil {
			t.Fatalf("valid %s component rejected: %v", component.ID, err)
		}
	}

	invalid := valid[1]
	invalid.Port = 8080
	if err := ValidateComponent(invalid); err == nil {
		t.Fatal("job HTTP endpoint accepted")
	}
}
