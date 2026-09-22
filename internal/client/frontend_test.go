package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dblooman/envy/internal/domain"
)

func TestResolveFrontendDeadlineAndGone(t *testing.T) {
	for _, code := range []string{"not_found", "conflict", "gone"} {
		t.Run(code, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" {
					t.Error("wait mutated state")
				}

				w.WriteHeader(409)
				json.NewEncoder(w).Encode(map[string]any{"error": domain.Error{Code: code, Message: "not available", Retryable: code == "conflict"}})
			}))
			defer s.Close()
			c, _ := New(s.URL, "test-token", nil)
			k := domain.FrontendKey{Project: "demo", Frontend: "web", Revision: strings.Repeat("a", 40)}
			start := time.Now()
			out, err := c.ResolveFrontend(context.Background(), k, 30*time.Millisecond)
			var de *domain.Error
			expected := "timeout"
			if code == "gone" {
				expected = "gone"
			}

			if !errors.As(err, &de) || de.Code != expected || out.APIURL != "" || time.Since(start) > time.Second {
				t.Fatalf("%+v %v", out, err)
			}

			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			_, err = c.ResolveFrontend(ctx, k, time.Second)
			if !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
		})
	}
}
