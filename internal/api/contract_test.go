package api

import (
	"bytes"
	"encoding/json"
	"os"
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
	now := time.Now().UTC()
	s := &fakeService{composition: domain.Composition{
		ID: "abc", Project: "demo", Baseline: "staging", BaselineRevision: "1", Name: "test",
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
	validate(t, "UpdateComposition", []byte(`{"expected_generation":1,"overrides":{"service-b":{"image":"envy/service-b:v3"}}}`))
	s.composition.Phase = domain.PhaseUpdating
	s.composition.LatestOperation.Kind = "update"
	validate(t, "Composition", request(h, "GET", "/v1/compositions/abc", "", "secret").Body.Bytes())
}
