package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dblooman/envy/internal/domain"
)

type githubAPIFake struct {
	fakeService
	githubPreviewService
	calls int
}

func (f *githubAPIFake) GitHubStatus(context.Context) (map[string]any, error) {
	return map[string]any{"configured": true}, nil
}

func (f *githubAPIFake) ControlPRPreview(_ context.Context, id, action string) (domain.PRPreview, error) {
	f.calls++
	return domain.PRPreview{ID: id, Status: action}, nil
}

func (f *githubAPIFake) ReceiveGitHubWebhook(_ context.Context, _, _, sig string, _ []byte) error {
	f.calls++
	if sig != "valid" {
		return &domain.Error{Code: "unauthorized", Message: "invalid signature"}
	}

	return nil
}

func TestGitHubRoutesRequireAppropriateAuthentication(t *testing.T) {
	f := &githubAPIFake{}
	h := NewHandler(f, "operator", nil)
	for _, test := range []struct {
		method, path, token, sig string
		want                     int
	}{{"GET", "/v1/github/status", "", "", 401}, {"GET", "/v1/github/status", "operator", "", 200}, {"POST", "/v1/github/previews/id/stop", "", "", 401}, {"POST", "/v1/github/previews/id/restart", "operator", "", 200}, {"POST", "/webhooks/github", "operator", "", 401}, {"POST", "/webhooks/github", "", "valid", 202}} {
		r := httptest.NewRequestWithContext(context.Background(), test.method, test.path, strings.NewReader(`{}`))
		if test.token != "" {
			r.Header.Set("Authorization", "Bearer "+test.token)
		}

		r.Header.Set("X-Hub-Signature-256", test.sig)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != test.want {
			t.Fatalf("%s %s: %d %s", test.method, test.path, w.Code, w.Body.String())
		}
	}

	r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/webhooks/github", strings.NewReader(strings.Repeat("x", (2<<20)+1)))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code == 202 {
		t.Fatal("oversized delivery accepted")
	}
}
