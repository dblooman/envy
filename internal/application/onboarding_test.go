package application

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/dblooman/envy/internal/domain"
)

type onboardingFixture struct {
	catalogFixture
	checks, applies int
}

func (f *onboardingFixture) CheckCatalog(context.Context, domain.CatalogManifest) error {
	f.checks++
	return nil
}

func (f *onboardingFixture) ApplyCatalog(context.Context, domain.CatalogManifest) error {
	f.applies++
	return nil
}

type connectedBaseline struct{ calls int }

func (v *connectedBaseline) ValidateBaseline(context.Context, domain.Baseline, map[string]domain.Component) error {
	v.calls++
	return nil
}

func TestOnboardingNormalizesChecksAndRejectsBeforeRegistration(t *testing.T) {
	data, err := os.ReadFile("../../examples/shop/application.json")
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name   string
		change func(*domain.CatalogManifest)
		valid  bool
	}{
		{"http", func(m *domain.CatalogManifest) {
			m.Baseline.Verification.Path = ""
			m.Baseline.Verification.ExpectedStatus = 0
			m.Components[0].Project = ""
		}, true},
		{"unknown version", func(m *domain.CatalogManifest) { m.APIVersion = "envy/v99" }, false},
		{"scope", func(m *domain.CatalogManifest) { m.Components[0].Project = "other" }, false},
		{"duplicate", func(m *domain.CatalogManifest) { m.Components[1] = m.Components[0] }, false},
		{"missing profile", func(m *domain.CatalogManifest) { m.Components = m.Components[:1] }, false},
		{"redirect target", func(m *domain.CatalogManifest) { m.Baseline.Verification.Path = "//external.example" }, false},
		{"failure status", func(m *domain.CatalogManifest) { m.Baseline.Verification.ExpectedStatus = 503 }, false},
		{"mixed contracts", func(m *domain.CatalogManifest) { m.Baseline.Verification.Chain = []string{"pricing", "storefront"} }, false},
		{"missing entry", func(m *domain.CatalogManifest) { m.Baseline.Routing.EntryComponent = "missing" }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var m domain.CatalogManifest
			if err := json.Unmarshal(data, &m); err != nil {
				t.Fatal(err)
			}

			tc.change(&m)
			f := &onboardingFixture{}
			v := &connectedBaseline{}
			s := New(f, Config{CatalogValidator: v})
			report, err := s.Onboard(context.Background(), m, false)
			if (err == nil) != tc.valid {
				t.Fatalf("unexpected validation: %v", err)
			}

			if f.applies != 0 {
				t.Fatal("validate wrote catalog")
			}

			if !tc.valid {
				if f.checks != 0 {
					t.Fatal("invalid config reached catalog")
				}

				return
			}

			if report.Applied || len(report.Warnings) != 1 || len(report.Checks) != 3 || report.Configuration.Baseline.Verification.ExpectedStatus != 200 {
				t.Fatal("validation report lost semantics")
			}

			report, err = s.Onboard(context.Background(), report.Configuration, true)
			if err != nil || !report.Applied || f.applies != 1 {
				t.Fatalf("apply failed: %+v %v", report, err)
			}
		})
	}
}
