package domain

import (
	"strings"
	"testing"
	"time"
)

func TestFrontendValidation(t *testing.T) {
	for _, n := range []int{0, 7, 39, 40, 41, 64, 65} {
		err := ValidateFrontendKey(FrontendKey{Project: "shop", Frontend: "web", Revision: strings.Repeat("a", n)})
		if (err == nil) != (n == 40 || n == 64) {
			t.Errorf("revision length %d: %v", n, err)
		}
	}
	for _, raw := range []string{"https://pages.example/path", "http://localhost:4174", "http://127.0.0.1:4174", "http://[::1]:4174", "http://web.localhost"} {
		if !PublicFrontendURL(raw, true) {
			t.Errorf("rejected %s", raw)
		}
	}
	for _, raw := range []string{"javascript:alert(1)", "http://example.com", "https://u:secret@example.com", "https://example.com?token=secret", "https://example.com#secret", "https://example.com\\bad", "https://example.com\n", "//example.com"} {
		if PublicFrontendURL(raw, true) {
			t.Errorf("accepted %q", raw)
		}
	}
	if PublicFrontendURL("http://localhost/repo", false) {
		t.Fatal("repository accepted HTTP")
	}
}

func TestFrontendEvidenceAndAvailability(t *testing.T) {
	now := time.Now()
	c := Composition{ID: "abc", Project: "shop", Generation: 2, ObservedGeneration: 2, Phase: PhaseReady, ExpiresAt: now.Add(time.Hour), Endpoints: map[string]Endpoint{"public": {URL: "https://preview.example", Ready: true}}}
	b := FrontendBinding{Check: &FrontendCheck{CompositionGeneration: 2, Status: "passed"}}
	if v := ViewFrontend(b, c, now); !v.Ready || v.CheckState != "current" {
		t.Fatalf("%+v", v)
	}
	for _, change := range []func(*Composition){
		func(c *Composition) { c.ExpiresAt = now }, func(c *Composition) { c.DeletionRequested = true },
		func(c *Composition) { c.Phase = PhaseFailed }, func(c *Composition) { c.Generation++ },
		func(c *Composition) { c.Endpoints = map[string]Endpoint{} },
	} {
		changed := c
		change(&changed)
		if v := ViewFrontend(b, changed, now); v.Ready || v.CheckState != "stale" {
			t.Fatalf("unavailable evidence %+v", v)
		}
	}
}
