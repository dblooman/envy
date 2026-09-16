package github

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dblooman/envy/internal/domain"
)

func TestPreviewPermissionScopeAndWorkflowVerification(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	p, e := New("123", pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
	if e != nil {
		t.Fatal(e)
	}

	var permissions map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/access_tokens") {
			var body struct {
				Repositories []string          `json:"repositories"`
				Permissions  map[string]string `json:"permissions"`
			}
			if e := json.NewDecoder(r.Body).Decode(&body); e != nil {
				t.Error(e)
			}

			if len(body.Repositories) != 1 || body.Repositories[0] != "backend" {
				t.Error("unscoped repository token")
			}

			permissions = body.Permissions
			_ = json.NewEncoder(w).Encode(map[string]string{"token": "secret"})
			return
		}

		switch r.URL.Path {
		case "/repos/acme/backend":
			if permissions["pull_requests"] != "write" || permissions["actions"] != "read" || permissions["deployments"] != "write" {
				t.Error("missing preview permissions")
			}

			_, _ = w.Write([]byte(`{"id":12,"full_name":"acme/backend"}`))
		case "/repos/acme/backend/actions/workflows/11/runs":
			if permissions["actions"] != "read" || len(permissions) != 1 {
				t.Error("workflow token overprivileged")
			}

			if r.URL.Query().Get("head_sha") != "abc" || r.URL.Query().Get("page") != "2" {
				t.Error("lost pagination/revision")
			}

			_, _ = w.Write([]byte(`{"workflow_runs":[{"id":42,"workflow_id":11,"run_number":3,"run_attempt":2,"head_sha":"abc","status":"completed","conclusion":"success"}]}`))
		case "/repos/acme/backend/actions/runs/42/attempts/2":
			_, _ = w.Write([]byte(`{"id":42,"workflow_id":11,"run_number":3,"run_attempt":2,"head_sha":"abc","status":"completed","conclusion":"success"}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	p.baseURL = server.URL
	r := domain.SourceRepository{GitHubRepository: "acme/backend", InstallationID: 9}
	if _, e = p.PreviewAccess(context.Background(), r); e != nil {
		t.Fatal(e)
	}

	runs, e := p.WorkflowRuns(context.Background(), r, 11, "abc", 2)
	if e != nil || len(runs) != 1 || runs[0].Attempt != 2 {
		t.Fatal(runs, e)
	}

	run, e := p.RunAttempt(context.Background(), r, "42", 2)
	if e != nil || run.WorkflowID != 11 {
		t.Fatal(run, e)
	}
}

func TestGitHubRateLimitBackoff(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(429)
	}))
	defer server.Close()
	p := &Provider{baseURL: server.URL, client: server.Client()}
	for range 2 {
		var out any
		if e := p.request(context.Background(), "GET", "/app", "token", nil, &out); e == nil {
			t.Fatal("rate limit not surfaced")
		}
	}

	if calls != 1 {
		t.Fatal("did not respect backoff")
	}
}
