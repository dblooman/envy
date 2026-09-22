package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/dblooman/envy/internal/domain"
)

func TestCatalogCommandsValidateThenApplyThroughREST(t *testing.T) {
	calls := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.URL.Path)
		var m domain.CatalogManifest
		if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer secret" || json.NewDecoder(r.Body).Decode(&m) != nil || m.APIVersion != "envy/v1" || m.Baseline.Verification.Kind != "http" {
			t.Error("configuration or auth lost")
		}

		json.NewEncoder(w).Encode(domain.CatalogReport{Configuration: m, Applied: r.URL.Path == "/v1/catalog/apply", Checks: []domain.Condition{}, Warnings: []string{"reachability only"}})
	}))
	defer server.Close()
	env := func(k string) string {
		return map[string]string{"ENVY_API_URL": server.URL, "ENVY_API_TOKEN": "secret"}[k]
	}
	for _, action := range []string{"validate", "apply"} {
		var out, diag bytes.Buffer
		if code := Run(context.Background(), []string{"catalog", action, "--file", "../../examples/shop/application.json"}, &out, &diag, env); code != 0 {
			t.Fatalf("%s: %s", action, &diag)
		}

		var report domain.CatalogReport
		if json.Unmarshal(out.Bytes(), &report) != nil || report.Applied != (action == "apply") {
			t.Fatal("invalid command result")
		}
	}

	file := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(file, []byte(`{"api_version":"envy/v1","unknown":true}`), 0o600); err != nil {
		t.Fatal(err)
	}

	var out, diag bytes.Buffer
	if Run(context.Background(), []string{"catalog", "apply", "--file", file}, &out, &diag, env) == 0 || len(calls) != 2 {
		t.Fatal("unknown fields reached registration")
	}

	if calls[0] != "/v1/catalog/validate" || calls[1] != "/v1/catalog/apply" {
		t.Fatal("wrong API operations")
	}
}
