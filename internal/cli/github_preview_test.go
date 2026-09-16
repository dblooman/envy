package cli

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPRPreviewCommands(t *testing.T) {
	for _, action := range []string{"list", "get", "stop", "restart"} {
		t.Run(action, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				path, method := "/v1/github/previews", "GET"
				if action != "list" {
					path += "/preview-id"
				}

				if action == "stop" || action == "restart" {
					path += "/" + action
					method = "POST"
				}

				if r.URL.Path != path || r.Method != method || r.Header.Get("Authorization") != "Bearer secret" {
					t.Errorf("wrong preview request: %s %s", r.Method, r.URL)
				}

				if action == "list" {
					_, _ = w.Write([]byte(`{"items":[]}`))
					return
				}

				_, _ = w.Write([]byte(`{"id":"preview-id"}`))
			}))
			defer server.Close()
			args := []string{"pr-preview", action}
			if action != "list" {
				args = append(args, "preview-id")
			}

			var out, diag bytes.Buffer
			env := func(k string) string {
				return map[string]string{"ENVY_API_URL": server.URL, "ENVY_API_TOKEN": "secret"}[k]
			}
			if code := Run(context.Background(), args, &out, &diag, env); code != 0 {
				t.Fatalf("%d: %s", code, &diag)
			}

			if calls != 1 {
				t.Fatal("command did not call preview API")
			}
		})
	}
}
