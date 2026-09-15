package api

import (
	"context"
	"strings"
	"testing"

	"github.com/dblooman/envy/internal/domain"
)

type diagnosticsService struct {
	fakeService
	calls   int
	options domain.LogOptions
}

func (s *diagnosticsService) Logs(_ context.Context, id, component string, o domain.LogOptions) (domain.ComponentLogs, error) {
	s.calls++
	s.options = o
	return domain.ComponentLogs{ID: id, Project: "demo", Component: component, Source: "shared-baseline", Message: "Shared-baseline logs", Streams: []domain.LogStream{}}, nil
}
func (s *diagnosticsService) Events(_ context.Context, id, after string, limit int) (domain.EventsPage, error) {
	s.calls++
	return domain.EventsPage{Items: []domain.LifecycleEvent{}}, nil
}
func TestDiagnosticsHTTPBoundsAndAuthentication(t *testing.T) {
	s := &diagnosticsService{}
	h := NewHandler(s, "secret", nil)
	logs := "/v1/compositions/abc/components/gateway/logs"
	for _, query := range []string{"tail_lines=0", "tail_lines=1001", "max_bytes=262145", "previous=yes", "since_seconds=-1", "tail_lines=2&tail_lines=3", "container=istio-proxy", "follow=true"} {
		w := request(h, "GET", logs+"?"+query, "", "secret")
		if w.Code != 400 {
			t.Fatalf("accepted %s: %d", query, w.Code)
		}
	}

	for _, query := range []string{"after=-1", "after=01", "after=1&after=2", "limit=101", "follow=true"} {
		w := request(h, "GET", "/v1/compositions/abc/events?"+query, "", "secret")
		if w.Code != 400 {
			t.Fatalf("accepted event option %s: %d", query, w.Code)
		}
	}

	if w := request(h, "GET", logs, "", ""); w.Code != 401 {
		t.Fatal("unauthenticated log access")
	}

	if s.calls != 0 {
		t.Fatal("invalid request reached service")
	}

	w := request(h, "GET", logs+"?tail_lines=5&max_bytes=10&since_seconds=60&previous=true", "", "secret")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"source":"shared-baseline"`) || s.options.TailLines != 5 || s.options.MaxBytes != 10 || !s.options.Previous || s.options.SinceSeconds != 60 {
		t.Fatalf("bad log request/response %d %s", w.Code, w.Body)
	}

	w = request(h, "GET", "/v1/compositions/abc/events?limit=1&after=2", "", "secret")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"items":[]`) {
		t.Fatalf("bad events %d %s", w.Code, w.Body)
	}
}
