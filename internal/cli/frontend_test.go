package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/dblooman/envy/internal/domain"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFrontendCommands(t *testing.T) {
	revision := strings.Repeat("a", 40)
	path := "/v1/projects/shop/frontend-bindings/web/" + revision
	for _, tc := range []struct {
		args         []string
		method, path string
		want         map[string]any
	}{
		{[]string{"bind", "--composition", "abc", "--repository", "https://example.com/web"}, "PUT", path, map[string]any{"composition": "abc", "repository": "https://example.com/web"}},
		{[]string{"get"}, "GET", path, nil},
		{[]string{"resolve", "--timeout", "0"}, "GET", path + "/resolve", nil},
		{[]string{"publish", "--expected-version", "2", "--url", "https://web.pages.dev"}, "POST", path + "/deployment", map[string]any{"expected_version": float64(2), "url": "https://web.pages.dev"}},
		{[]string{"check", "--expected-version", "3", "--composition-generation", "4", "--status", "passed", "--message", "Browser proof"}, "POST", path + "/check", map[string]any{"expected_version": float64(3), "composition_generation": float64(4), "status": "passed", "message": "Browser proof"}},
	} {
		t.Run(tc.args[0], func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != tc.method || r.URL.Path != tc.path || r.Header.Get("Authorization") != "Bearer secret" {
					t.Errorf("bad request %s %s", r.Method, r.URL.Path)
				}
				if tc.want != nil {
					var got map[string]any
					json.NewDecoder(r.Body).Decode(&got)
					for key, want := range tc.want {
						if got[key] != want {
							t.Errorf("%s=%v want %v", key, got[key], want)
						}
					}
				}
				json.NewEncoder(w).Encode(domain.FrontendBindingView{})
			}))
			defer server.Close()
			args := append([]string{"frontend"}, tc.args...)
			args = append(args, "--project", "shop", "--frontend", "web", "--revision", revision)
			var out, diag bytes.Buffer
			code := Run(context.Background(), args, &out, &diag, func(key string) string {
				return map[string]string{"ENVY_API_URL": server.URL, "ENVY_API_TOKEN": "secret"}[key]
			})
			if code != 0 || !json.Valid(out.Bytes()) || diag.Len() != 0 {
				t.Fatalf("code=%d out=%s diag=%s", code, &out, &diag)
			}
		})
	}
}
