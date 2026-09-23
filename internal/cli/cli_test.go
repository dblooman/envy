package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dblooman/envy/internal/domain"
)

func TestCommandsUseRESTAndEmitJSON(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer file-secret" {
			t.Error("token file did not take precedence")
		}

		switch r.Method {
		case "POST":
			var body domain.CreateRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error("could not decode create request")
			} else if body.Name == "cli" && (!body.MessageIsolation || body.Overrides["service-b"].Image != "envy/service-b:v2" || r.Header.Get("Idempotency-Key") != "retry") {
				t.Error("create arguments lost")
			} else if body.Name == "inherit" && len(body.Overrides) != 0 {
				t.Error("inherit-all create did not send an empty override set")
			}
		case "PATCH":
			var body domain.UpdateRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ExpectedGeneration != 4 || body.Overrides["service-b"].Image != "envy/service-b:v3" {
				t.Error("update arguments lost")
			}
		}

		if r.Method == "GET" && r.URL.Path == "/v1/compositions" {
			if r.URL.Query().Get("project") != "demo" || r.URL.Query().Get("after") != "abc" || r.URL.Query().Get("limit") != "3" {
				t.Error("pagination lost")
			}

			json.NewEncoder(w).Encode(map[string]any{"items": []domain.Composition{}, "next_cursor": "def"})
			return
		}

		json.NewEncoder(w).Encode(domain.Composition{ID: "abc", Phase: domain.PhaseReady})
	}))
	defer server.Close()
	token := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(token, []byte("file-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	env := func(k string) string {
		return map[string]string{"ENVY_API_URL": server.URL, "ENVY_API_TOKEN": "ignored", "ENVY_API_TOKEN_FILE": token}[k]
	}
	for _, args := range [][]string{
		{"create", "--name", "cli", "--message-isolation", "--image", "envy/service-b:v2", "--idempotency-key", "retry"},
		{"create", "--name", "inherit", "--inherit-all"},
		{"update", "abc", "--expected-generation", "4", "--image", "envy/service-b:v3"},
		{"get", "abc"},
		{"inspect", "abc"},
		{"wait", "abc", "--timeout", "1s"},
		{"endpoints", "abc"},
		{"destroy", "abc"},
		{"list", "--project", "demo", "--after", "abc", "--limit", "3"},
	} {
		var out, diag bytes.Buffer
		code := Run(context.Background(), append([]string{"composition"}, args...), &out, &diag, env)
		if code != 0 || !json.Valid(out.Bytes()) || diag.Len() != 0 {
			t.Fatalf("%v: code=%d stdout=%s stderr=%s", args, code, &out, &diag)
		}
	}

	if calls != 9 {
		t.Fatalf("got %d HTTP calls", calls)
	}
}

func TestVersionUsesEnvyRootCommand(t *testing.T) {
	var out, diag bytes.Buffer
	if code := Run(context.Background(), []string{"version"}, &out, &diag, func(string) string { return "" }); code != 0 || diag.Len() != 0 {
		t.Fatalf("version: code=%d stderr=%s", code, &diag)
	}
	var got map[string]string
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["version"] != "dev" || got["commit"] != "unknown" {
		t.Fatalf("version output = %#v", got)
	}
	var helpOut bytes.Buffer
	if code := Run(context.Background(), []string{"--help"}, &helpOut, &diag, func(string) string { return "" }); code != 0 || !strings.Contains(helpOut.String(), "envy version") || strings.Contains(helpOut.String(), "delivery") {
		t.Fatalf("help: code=%d out=%s", code, &helpOut)
	}
}

func TestWaitExitCodesAndValidation(t *testing.T) {
	phase := domain.PhaseUpdating
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(domain.Composition{ID: "abc", Phase: phase})
	}))
	defer server.Close()
	env := func(k string) string {
		return map[string]string{"ENVY_API_URL": server.URL, "ENVY_API_TOKEN": "secret"}[k]
	}
	for _, tc := range []struct {
		phase domain.Phase
		want  int
	}{{domain.PhaseUpdating, 2}, {domain.PhaseFailed, 1}, {domain.PhaseReady, 0}, {domain.PhaseDestroyed, 0}} {
		phase = tc.phase
		var out, diag bytes.Buffer
		code := Run(context.Background(), []string{"composition", "wait", "abc", "--timeout", "20ms"}, &out, &diag, env)
		if code != tc.want || !json.Valid(out.Bytes()) || diag.Len() != 0 {
			t.Fatalf("%s: code=%d out=%s err=%s", phase, code, &out, &diag)
		}
	}

	for _, args := range [][]string{{"composition", "update", "abc", "--image", "v3"}, {"composition", "wait", "abc", "--timeout", "61s"}, {"composition", "get"}, {"composition", "get", "abc", "extra"}, {"composition", "destroy", "../abc"}} {
		var out, diag bytes.Buffer
		if code := Run(context.Background(), args, &out, &diag, env); code != 1 || out.Len() != 0 || !json.Valid(diag.Bytes()) {
			t.Fatalf("bad arguments accepted %v", args)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out, diag bytes.Buffer
	if code := Run(ctx, []string{"composition", "wait", "abc"}, &out, &diag, env); code != 130 || !strings.Contains(diag.String(), "cancelled") {
		t.Fatalf("cancel code=%d diag=%s", code, &diag)
	}
}

func TestAPIConflictRemainsStructured(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(409)
		json.NewEncoder(w).Encode(map[string]any{"error": &domain.Error{Code: "conflict", Message: "stale generation", Composition: "abc"}})
	}))
	defer server.Close()
	var out, diag bytes.Buffer
	env := func(k string) string {
		return map[string]string{"ENVY_API_URL": server.URL, "ENVY_API_TOKEN": "secret"}[k]
	}
	code := Run(context.Background(), []string{"composition", "update", "abc", "--expected-generation", "1", "--image", "v3"}, &out, &diag, env)
	if code != 1 || out.Len() != 0 || !strings.Contains(diag.String(), `"code":"conflict"`) {
		t.Fatalf("lost conflict: %d %s", code, &diag)
	}
}

func TestDiagnosticsCommands(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/compositions/abc/components/gateway/logs":
			if r.URL.Query().Get("container") != "bootstrap" || r.URL.Query().Get("since_seconds") != "3600" || r.URL.Query().Get("max_bytes") != "123" || r.URL.Query().Get("tail_lines") != "4" || r.URL.Query().Get("previous") != "true" {
				t.Error("CLI lost log options")
			}

			json.NewEncoder(w).Encode(domain.ComponentLogs{ID: "abc", Source: "shared-baseline", Streams: []domain.LogStream{}})
		case "/v1/compositions/abc/verification":
			if r.URL.Query().Get("after") != "9" || r.URL.Query().Get("limit") != "2" {
				t.Error("CLI lost verification pagination")
			}
			if err := json.NewEncoder(w).Encode(domain.VerificationPage{Items: []domain.VerificationEvidence{}}); err != nil {
				t.Error(err)
			}
		case "/v1/compositions/abc/diagnosis":
			if err := json.NewEncoder(w).Encode(domain.Diagnosis{Composition: "abc", State: "blocked", Verification: "failed", Blockers: []domain.DiagnosticFinding{{Code: "verification_failed", Scope: "verification"}}, Notes: []domain.DiagnosticFinding{}}); err != nil {
				t.Error(err)
			}
		case "/v1/compositions/abc/observability":
			if r.URL.Query().Get("component") != "gateway" {
				t.Error("CLI lost component scope")
			}

			if err := json.NewEncoder(w).Encode(domain.ObservabilityLinks{Items: []domain.ObservabilityLink{}}); err != nil {
				t.Error(err)
			}
		case "/v1/compositions/abc/events":
			if r.URL.Query().Get("after") != "9" || r.URL.Query().Get("limit") != "2" {
				t.Error("CLI lost pagination")
			}

			json.NewEncoder(w).Encode(domain.EventsPage{Items: []domain.LifecycleEvent{}, NextCursor: "11"})
		default:
			t.Errorf("unexpected route %s", r.URL.Path)
		}
	}))
	defer server.Close()
	env := func(k string) string {
		return map[string]string{"ENVY_API_URL": server.URL, "ENVY_API_TOKEN": "secret"}[k]
	}
	for _, args := range [][]string{{"logs", "abc", "--component", "gateway", "--tail-lines", "4", "--max-bytes", "123", "--since", "1h", "--previous", "--container", "bootstrap"}, {"events", "abc", "--after", "9", "--limit", "2"}, {"verification", "abc", "--after", "9", "--limit", "2"}, {"diagnosis", "abc"}, {"observability", "abc", "--component", "gateway"}} {
		var out, diag bytes.Buffer
		if code := Run(context.Background(), append([]string{"composition"}, args...), &out, &diag, env); code != 0 || !json.Valid(out.Bytes()) || diag.Len() != 0 {
			t.Fatalf("%v failed: %d %s", args, code, &diag)
		}
	}

	for _, args := range [][]string{{"logs", "abc", "--since", "25h"}, {"logs", "abc", "--since", "0.5s"}, {"logs", "abc", "--max-bytes", "0"}, {"events", "abc", "--after", "-1"}} {
		var out, diag bytes.Buffer
		if code := Run(context.Background(), append([]string{"composition"}, args...), &out, &diag, env); code != 1 {
			t.Fatalf("invalid flags accepted: %v", args)
		}
	}
}

func TestHelpOutputsJSON(t *testing.T) {
	env := func(k string) string { return "" }

	// Root and composition usage help
	for _, args := range [][]string{
		{},
		{"--help"},
		{"-h"},
		{"composition", "--help"},
		{"composition", "-h"},
	} {
		var out, diag bytes.Buffer
		code := Run(context.Background(), args, &out, &diag, env)
		if code != 0 || diag.Len() != 0 {
			t.Fatalf("help %v failed: code=%d err=%s", args, code, &diag)
		}

		var payload map[string]string
		if err := json.Unmarshal(out.Bytes(), &payload); err != nil || payload["usage"] != usage {
			t.Fatalf("bad help payload for %v: %s", args, out.String())
		}
	}

	// Subcommand usage help with flags map
	for _, args := range [][]string{
		{"composition", "create", "--help"},
		{"composition", "update", "--help"},
		{"composition", "logs", "--help"},
	} {
		var out, diag bytes.Buffer
		code := Run(context.Background(), args, &out, &diag, env)
		if code != 0 || diag.Len() != 0 {
			t.Fatalf("subcommand help %v failed: code=%d err=%s", args, code, &diag)
		}

		var payload struct {
			Usage   string            `json:"usage"`
			Command string            `json:"command"`
			Flags   map[string]string `json:"flags"`
		}
		if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
			t.Fatalf("invalid json help: %v: %s", err, out.String())
		}

		if payload.Usage != usage || payload.Command != args[1] || len(payload.Flags) == 0 {
			t.Fatalf("unexpected help content: %+v", payload)
		}

		if _, ok := payload.Flags["--api-url"]; !ok {
			t.Fatalf("expected persistent flag --api-url in help flags: %+v", payload.Flags)
		}
	}
}

func TestPersistentFlagsPlacement(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(domain.Composition{ID: "abc", Phase: domain.PhaseReady})
	}))
	defer server.Close()

	env := func(k string) string {
		return map[string]string{"ENVY_API_TOKEN": "secret"}[k]
	}

	// Persistent flag --api-url placed before or after command
	for _, args := range [][]string{
		{"--api-url", server.URL, "composition", "get", "abc"},
		{"composition", "get", "abc", "--api-url", server.URL},
	} {
		var out, diag bytes.Buffer
		code := Run(context.Background(), args, &out, &diag, env)
		if code != 0 || diag.Len() != 0 || !json.Valid(out.Bytes()) {
			t.Fatalf("persistent flag placement failed for %v: code=%d out=%s err=%s", args, code, &out, &diag)
		}
	}
}

func TestRepeatedOverrideArguments(t *testing.T) {
	for _, tc := range []struct {
		values    []string
		image     string
		component bool
		want      int
	}{
		{[]string{"service-a=a:v2", "service-b=b:v2"}, "", false, 2},
		{[]string{"a=v1", "a=v2"}, "", false, 0},
		{[]string{"a=v1"}, "legacy:v1", false, 0},
		{[]string{"a=v1"}, "", true, 0},
		{[]string{"a=v1", "b=v1", "c=v1", "d=v1"}, "", false, 0},
		{[]string{"a"}, "", false, 0},
	} {
		out, err := parseOverrides(tc.values, "service-b", tc.image, tc.component)
		if tc.want == 0 && err == nil || tc.want > 0 && (err != nil || len(out) != tc.want) {
			t.Fatalf("overrides %v: %+v %v", tc.values, out, err)
		}
	}
}
