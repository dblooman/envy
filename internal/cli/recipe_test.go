package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dblooman/envy/internal/domain"
)

func TestRecipeCLIRequiresKeyAndPreservesBaselineGuard(t *testing.T) {
	r := domain.Recipe{APIVersion: domain.RecipeVersion, Project: "demo", Baseline: "staging", BaselineRevision: "rev1", TTL: "1h", Overrides: map[string]domain.ComponentOverride{"service-b": {BuildID: strings.Repeat("a", 64)}}}
	file := filepath.Join(t.TempDir(), "recipe.json")
	data, _ := json.Marshal(r)
	os.WriteFile(file, data, 0600)
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		calls++
		var body domain.CreateRequest
		json.NewDecoder(req.Body).Decode(&body)
		if body.ExpectedBaselineRevision != "rev1" || req.Header.Get("Idempotency-Key") != "key" {
			t.Error("lost guard/key")
		}
		json.NewEncoder(w).Encode(domain.Composition{ID: "new"})
	}))
	defer server.Close()
	env := func(k string) string {
		return map[string]string{"ENVY_API_URL": server.URL, "ENVY_API_TOKEN": "secret"}[k]
	}
	for _, test := range []struct {
		args    []string
		success bool
	}{
		{[]string{"recipe", "validate", "--file", file}, true},
		{[]string{"recipe", "recreate", "--file", file, "--name", "new"}, false},
		{[]string{"recipe", "recreate", "--file", file, "--name", "new", "--idempotency-key", "key"}, true},
	} {
		var out, diag bytes.Buffer
		code := Run(context.Background(), test.args, &out, &diag, env)
		if (code == 0) != test.success {
			t.Fatalf("%v: %s", test.args, &diag)
		}
	}
	if calls != 1 {
		t.Fatalf("invalid recipe request or offline validation reached API: %d", calls)
	}
}
