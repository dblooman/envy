package application

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/dblooman/envy/internal/domain"
)

func TestObservabilityLinksScopeAndEncoding(t *testing.T) {
	c := domain.Composition{ID: "a/b &c", Project: "demo", Generation: 7, Components: map[string]domain.ComponentObservation{"api": {}}}
	templates := []domain.ObservabilityTemplate{{Project: "demo", Label: "Traces", Kind: "traces", URL: "https://grafana.example/p/{preview}?project={project}&generation={generation}&from={from}&to={to}"}, {Project: "other", Label: "Private", Kind: "logs", URL: "https://other.example"}, {Project: "demo", Label: "Service", Kind: "logs", URL: "https://logs.example?q={component}"}}
	s := New(diagnosticsRepo{c: c}, Config{Installation: "local", Observability: templates})
	out, err := s.Observability(context.Background(), c.ID, "")
	if err != nil || len(out.Items) != 1 {
		t.Fatalf("scope: %+v %v", out, err)
	}

	u, err := url.Parse(out.Items[0].URL)
	if err != nil || u.EscapedPath() != "/p/a%2Fb%20&c" || u.Query().Get("project") != "demo" || u.Query().Get("generation") != "7" || strings.Contains(u.String(), "{preview}") {
		t.Fatalf("encoding: %s %v", out.Items[0].URL, err)
	}

	out, err = s.Observability(context.Background(), c.ID, "api")
	if err != nil || len(out.Items) != 2 {
		t.Fatalf("component: %+v %v", out, err)
	}

	if _, err = s.Observability(context.Background(), c.ID, "unknown"); err == nil {
		t.Fatal("unknown component accepted")
	}
}

func TestObservabilityRejectsUnsafeConfiguration(t *testing.T) {
	for _, raw := range []string{"javascript:alert(1)", "https://user:password@example.com", "https://example.com?token=secret", "https://{project}.example.com", "https://example.com/{unknown}", "https://example.com/#secret"} {
		if err := ValidateObservabilityTemplates([]domain.ObservabilityTemplate{{Project: "demo", Label: "logs", Kind: "logs", URL: raw}}); err == nil {
			t.Fatal("accepted", raw)
		}
	}
}
