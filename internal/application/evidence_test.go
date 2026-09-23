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

func TestRequestLinkRequiresCurrentObservedPreviewIdentity(t *testing.T) {
	id := "123e4567-e89b-12d3-a456-426614174000"
	c := domain.Composition{ID: "synthetic", Project: "demo", Generation: 2, BaselineObservation: &domain.BaselineObservation{State: "current", Fingerprint: "baseline-two"}}
	evidence := domain.VerificationEvidence{Generation: 2, Outcome: "passed", BaselineFingerprint: "baseline-two", ContractFingerprint: "contract", Probes: []domain.VerificationProbe{{Target: "baseline", RequestID: "aaaaaaaaaaaaaaaa"}, {Target: "preview", RequestID: id}}}
	templates := []domain.ObservabilityTemplate{{Project: "demo", Label: "Request", Kind: "traces", URL: "https://traces.example/request/{request_id}?generation={generation}"}, {Project: "demo", Label: "Dashboard", Kind: "dashboard", URL: "https://traces.example/dashboard?preview={preview}"}}
	repo := diagnosticsRepo{c: c, verification: []domain.VerificationEvidence{evidence}}
	s := New(repo, Config{Observability: templates})
	links, err := s.Observability(context.Background(), c.ID, "")
	if err != nil || len(links.Items) != 2 || !strings.Contains(links.Items[0].URL, id) || !strings.Contains(links.Items[0].URL, "generation=2") {
		t.Fatalf("current request context missing: %+v %v", links, err)
	}

	repo.c.Generation = 3
	s = New(repo, Config{Observability: templates})
	links, err = s.Observability(context.Background(), c.ID, "")
	if err != nil || len(links.Items) != 1 || links.Items[0].Label != "Dashboard" {
		t.Fatalf("stale request identity leaked into link: %+v %v", links, err)
	}

	repo.c.Generation = 2
	repo.verification[0].Probes[1].RequestID = "secret?token=abc"
	s = New(repo, Config{Observability: templates})
	links, err = s.Observability(context.Background(), c.ID, "")
	if err != nil || len(links.Items) != 1 {
		t.Fatalf("unsafe request identity reached link: %+v %v", links, err)
	}
}
