package api

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dblooman/envy/internal/domain"
	"github.com/google/jsonschema-go/jsonschema"
	"sigs.k8s.io/yaml"
)

// Check emitted wire responses against the checked-in OpenAPI JSON Schemas,
// protecting the contract from domain/handler/documentation drift.
func TestPublishedOpenAPIContract(t *testing.T) {
	data, err := os.ReadFile("../../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	data, err = yaml.YAMLToJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		OpenAPI    string `json:"openapi"`
		Components struct {
			Schemas map[string]json.RawMessage `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	if document.OpenAPI != "3.1.0" {
		t.Fatalf("unsupported OpenAPI %s", document.OpenAPI)
	}
	validate := func(t *testing.T, name string, payload []byte) {
		t.Helper()
		root, err := json.Marshal(map[string]any{"$ref": "#/$defs/" + name, "$defs": document.Components.Schemas})
		if err != nil {
			t.Fatal(err)
		}
		root = bytes.ReplaceAll(root, []byte("#/components/schemas/"), []byte("#/$defs/"))
		var schema jsonschema.Schema
		if err := json.Unmarshal(root, &schema); err != nil {
			t.Fatal(err)
		}
		resolved, err := schema.Resolve(nil)
		if err != nil {
			t.Fatalf("resolve %s: %v", name, err)
		}
		var value any
		if err := json.Unmarshal(payload, &value); err != nil {
			t.Fatal(err)
		}
		if err := resolved.Validate(value); err != nil {
			t.Fatalf("%s violates OpenAPI: %v\n%s", name, err, payload)
		}
	}
	shop, err := os.ReadFile("../../examples/shop/application.json")
	if err != nil {
		t.Fatal(err)
	}
	validate(t, "CatalogManifest", shop)
	var manifest domain.CatalogManifest
	if err = json.Unmarshal(shop, &manifest); err != nil {
		t.Fatal(err)
	}
	report, _ := json.Marshal(domain.CatalogReport{Configuration: manifest, Applied: true, Checks: []domain.Condition{{Type: "BaselineConnectivity", Status: true}}, Warnings: []string{"reachability only"}})
	validate(t, "CatalogReport", report)
	guarded, _ := json.Marshal(domain.CreateRequest{Project: "shop", Baseline: "staging", ExpectedBaselineRevision: "shop-v1", Name: "recreated", Overrides: map[string]domain.ComponentOverride{"pricing": {BuildID: strings.Repeat("a", 64)}}})
	validate(t, "CreateComposition", guarded)
	guardedUpdate, _ := json.Marshal(domain.UpdateRequest{ExpectedGeneration: 1, Overrides: map[string]domain.ComponentOverride{"pricing": {BuildID: strings.Repeat("a", 64)}}})
	validate(t, "UpdateComposition", guardedUpdate)

	now := time.Now().UTC()
	binding := domain.FrontendBinding{Project: "shop", Frontend: "web", Revision: strings.Repeat("a", 40), Composition: "abc", Repository: "https://example.com/web", Version: 2, URL: "https://web.pages.dev", CreatedAt: now, UpdatedAt: now, Check: &domain.FrontendCheck{CompositionGeneration: 1, Status: "passed", Message: "Caller report", ReportedAt: now}}
	for name, value := range map[string]any{
		"FrontendBindingView":    domain.FrontendBindingView{Binding: binding, CompositionPhase: domain.PhaseReady, CompositionGeneration: 1, ExpiresAt: now.Add(time.Hour), Ready: true, VerificationLevel: "reachability", CheckState: "current"},
		"FrontendResolution":     domain.FrontendResolution{Project: "shop", Frontend: "web", Revision: binding.Revision, Composition: "abc", CompositionGeneration: 1, BindingVersion: 2, APIURL: "https://preview.example", ExpiresAt: now.Add(time.Hour), VerificationLevel: "reachability"},
		"BindFrontendRequest":    domain.BindFrontendRequest{Composition: "abc", Repository: binding.Repository},
		"PublishFrontendRequest": domain.PublishFrontendRequest{ExpectedVersion: 1, URL: binding.URL},
		"FrontendCheckRequest":   domain.FrontendCheckRequest{ExpectedVersion: 2, CompositionGeneration: 1, Status: "passed", Message: "Caller report"},
	} {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		validate(t, name, data)
	}

	sourceRepo := domain.SourceRepository{Project: "demo", ID: "backend", GitHubRepository: "acme/backend", InstallationID: 42, Enabled: true, Images: map[string]string{"service-b": "registry.example.com/service-b"}}
	build := domain.Build{Component: "service-b", Revision: strings.Repeat("a", 40), Image: "registry.example.com/service-b@sha256:" + strings.Repeat("b", 64), RunID: "123", Attempt: 1, BuiltAt: now, ID: strings.Repeat("c", 64), Project: "demo", Repository: "backend", GitHubRepository: "acme/backend", RunURL: "https://github.com/acme/backend/actions/runs/123/attempts/1"}
	for name, value := range map[string]any{"SourceRepository": sourceRepo, "PublishedBuild": build, "BuildReport": build.BuildReport, "RevisionResolution": domain.RevisionResolution{Repository: sourceRepo, Commit: domain.GitCommit{SHA: build.Revision, Message: "change"}, Builds: []domain.Build{build}, CIURL: "https://github.com/acme/backend/actions"}, "ComponentOverride": domain.ComponentOverride{BuildID: build.ID}, "ResolvedComponentOverride": domain.ComponentOverride{BuildID: build.ID, Image: build.Image, Source: &build}} {
		data, _ := json.Marshal(value)
		validate(t, name, data)
	}
	s := &fakeService{composition: domain.Composition{
		VerificationLevel: "reachability", ID: "abc", Project: "demo", Baseline: "staging", BaselineRevision: "1", Name: "test",
		Overrides:  map[string]domain.ComponentOverride{"service-b": {Image: "envy/service-b:v2"}},
		Generation: 1, ObservedGeneration: 1, Phase: domain.PhaseReady,
		CreatedAt: now, UpdatedAt: now, ExpiresAt: now.Add(time.Hour),
		Components:      map[string]domain.ComponentObservation{"service-b": {Source: "override", Status: "ready", Image: "envy/service-b:v2"}},
		Conditions:      []domain.Condition{{Type: "RequestRoutingVerified", Status: true, Message: "verified"}},
		Endpoints:       map[string]domain.Endpoint{"public": {URL: "http://cmp-abc.envy.localhost:8080", Ready: true}},
		LatestOperation: domain.Operation{ID: "op", Kind: "create", Status: "succeeded"},
	}}
	h := NewHandler(s, "secret", nil)
	for _, tc := range []struct{ method, path, body, schema string }{
		{"POST", "/v1/compositions", validCreate, "Composition"},
		{"GET", "/v1/compositions/abc", "", "Composition"},
		{"GET", "/v1/compositions/abc/status", "", "CompositionStatus"},
		{"DELETE", "/v1/compositions/abc", "", "Composition"},
		{"PATCH", "/v1/compositions/abc", `{"expected_generation":1,"overrides":{"service-b":{"image":"envy/service-b:v3"}}}`, "Composition"},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			w := request(h, tc.method, tc.path, tc.body, "secret")
			if w.Code != 200 && w.Code != 202 {
				t.Fatalf("HTTP %d: %s", w.Code, w.Body.String())
			}
			validate(t, tc.schema, w.Body.Bytes())
		})
	}
	example, err := os.ReadFile("../../examples/create-composition.json")
	if err != nil {
		t.Fatal(err)
	}
	validate(t, "CreateComposition", example)
	diagnostics := NewHandler(&diagnosticsService{}, "secret", nil)
	validate(t, "ComponentLogs", request(diagnostics, "GET", "/v1/compositions/abc/components/gateway/logs", "", "secret").Body.Bytes())
	validate(t, "EventsPage", request(diagnostics, "GET", "/v1/compositions/abc/events", "", "secret").Body.Bytes())
	event, _ := json.Marshal(domain.LifecycleEvent{ID: "1", Composition: "abc", Project: "demo", Generation: 1, Type: "create_requested", Phase: domain.PhaseCreated, OccurredAt: now, Operation: domain.Operation{ID: "op", Kind: "create", Status: "pending"}, Conditions: []domain.Condition{}})
	validate(t, "LifecycleEvent", event)
	validate(t, "UpdateComposition", []byte(`{"expected_generation":1,"overrides":{"service-b":{"image":"envy/service-b:v3"}}}`))
	s.composition.Phase = domain.PhaseUpdating
	s.composition.LatestOperation.Kind = "update"
	validate(t, "Composition", request(h, "GET", "/v1/compositions/abc", "", "secret").Body.Bytes())
}
