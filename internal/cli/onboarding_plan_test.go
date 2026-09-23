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

func TestCLIPlanReviewAndGuardedCreate(t *testing.T) {
	plans, creates := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request domain.CreateRequest
		if r.Method != http.MethodPost || json.NewDecoder(r.Body).Decode(&request) != nil || request.Project != "demo" || request.ExpectedBaselineRevision != "revision-42" {
			t.Errorf("lost guarded request at %s", r.URL.Path)
		}

		switch r.URL.Path {
		case "/v1/compositions/plan":
			plans++
			_ = json.NewEncoder(w).Encode(domain.PreviewPlan{Ready: true, Project: "demo", Baseline: "staging", BaselineRevision: "revision-42", Selected: []domain.PlannedWorkload{}, Inherited: map[string]domain.BaselineBinding{}, Blockers: []domain.PlanningBlocker{}, Cautions: []domain.PlanningBlocker{}})
		case "/v1/compositions":
			creates++
			if r.Header.Get("Idempotency-Key") != "reviewed-retry" {
				t.Error("create lost idempotency identity")
			}

			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(domain.Composition{ID: "one", Phase: domain.PhaseCreated})
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	env := func(key string) string {
		return map[string]string{"ENVY_API_URL": server.URL, "ENVY_API_TOKEN": "test"}[key]
	}
	file := filepath.Join(t.TempDir(), "request.json")
	request := domain.CreateRequest{Project: "demo", Baseline: "staging", Name: "reviewed", ExpectedBaselineRevision: "revision-42", Overrides: map[string]domain.ComponentOverride{"service-b": {Image: "envy/service-b:v2"}}}
	data, _ := json.Marshal(request)
	if err := os.WriteFile(file, data, 0o600); err != nil {
		t.Fatal(err)
	}

	var out, diag bytes.Buffer
	if code := Run(context.Background(), []string{"composition", "plan", "--file", file}, &out, &diag, env); code != 0 {
		t.Fatalf("plan: code=%d %s", code, diag.String())
	}

	var plan domain.PreviewPlan
	if err := json.Unmarshal(out.Bytes(), &plan); err != nil || !plan.Ready || plans != 1 || creates != 0 {
		t.Fatalf("plan created resources or lost result: %+v %v", plan, err)
	}

	// Closing preparation before the create command is issued must leave no
	// composition. The subsequent explicit command carries the reviewed guard.
	out.Reset()
	diag.Reset()
	args := []string{"composition", "create", "--project", "demo", "--baseline", "staging", "--name", "reviewed", "--image", "envy/service-b:v2", "--expected-baseline-revision", plan.BaselineRevision, "--idempotency-key", "reviewed-retry"}
	if code := Run(context.Background(), args, &out, &diag, env); code != 0 || creates != 1 {
		t.Fatalf("guarded create: code=%d creates=%d %s", code, creates, diag.String())
	}
}
