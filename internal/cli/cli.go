// Package cli provides the JSON-only delivery command over the private REST client.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/dblooman/envy/internal/client"
	"github.com/dblooman/envy/internal/domain"
)

const usage = "delivery composition create|list|get|inspect|wait|endpoints|update|destroy|logs|events [id] [flags]; use --help after a command for its flags"

// Run emits one JSON result on stdout, or a structured error on stderr. It
// returns a process exit code, allowing tests to exercise the real command parser.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	result, code, err := run(ctx, args, getenv)
	if err != nil {
		var public *domain.Error
		if !errors.As(err, &public) {
			public = &domain.Error{Code: "client_error", Message: err.Error()}
		}
		if errors.Is(err, context.Canceled) {
			public = &domain.Error{Code: "cancelled", Message: "command cancelled"}
			code = 130
		}
		_ = json.NewEncoder(stderr).Encode(map[string]any{"error": public})
		return code
	}
	if err := json.NewEncoder(stdout).Encode(result); err != nil {
		_ = json.NewEncoder(stderr).Encode(map[string]any{"error": &domain.Error{Code: "output_error", Message: "could not write JSON output"}})
		return 1
	}
	return code
}

func run(ctx context.Context, args []string, getenv func(string) string) (any, int, error) {
	if len(args) == 0 || (len(args) == 1 && (args[0] == "--help" || args[0] == "-h")) {
		return map[string]string{"usage": usage}, 0, nil
	}
	if len(args) < 2 || args[0] != "composition" {
		return nil, 1, domain.Validation(usage)
	}
	command, rest := args[1], args[2:]
	needsID := false
	switch command {
	case "create", "list":
	case "get", "inspect", "wait", "endpoints", "update", "destroy", "logs", "events":
		needsID = true
	default:
		return nil, 1, domain.Validation("unknown composition command: " + command)
	}
	id := ""
	if needsID && len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		id, rest = rest[0], rest[1:]
	}
	f := flag.NewFlagSet(command, flag.ContinueOnError)
	f.SetOutput(io.Discard)
	apiURL := getenv("ENVY_API_URL")
	if apiURL == "" {
		apiURL = "http://127.0.0.1:8081"
	}
	f.StringVar(&apiURL, "api-url", apiURL, "REST API URL")
	tokenFile := f.String("token-file", getenv("ENVY_API_TOKEN_FILE"), "API token file; takes precedence over ENVY_API_TOKEN")
	var project, baseline, name, image, ttl, key, after string
	var generation int64
	var limit int
	var timeout time.Duration
	var component string
	var logOptions domain.LogOptions
	var since time.Duration
	if command == "create" {
		f.StringVar(&project, "project", "demo", "registered project")
		f.StringVar(&baseline, "baseline", "staging", "registered baseline")
		f.StringVar(&name, "name", "", "composition name (required)")
		f.StringVar(&ttl, "ttl", "", "expiry duration; server default when omitted")
		f.StringVar(&key, "idempotency-key", "", "stable create retry key")
	}
	if command == "create" || command == "update" {
		f.StringVar(&image, "image", "", "prebuilt image (required)")
		f.StringVar(&component, "component", "service-b", "registered override component")
	}
	if command == "update" {
		f.Int64Var(&generation, "expected-generation", 0, "current desired generation (required)")
	}
	if command == "wait" {
		f.DurationVar(&timeout, "timeout", 30*time.Second, "bounded wait duration, maximum 60s")
	}
	if command == "logs" {
		f.StringVar(&component, "component", "service-b", "logical component; inherited logs are shared-baseline logs")
		f.Int64Var(&logOptions.TailLines, "tail-lines", 200, "maximum lines per pod, 1–1000")
		f.Int64Var(&logOptions.MaxBytes, "max-bytes", 65536, "total log byte cap, 1–262144")
		f.DurationVar(&since, "since", 0, "lookback duration in whole seconds, maximum 24h")
		f.BoolVar(&logOptions.Previous, "previous", false, "read last terminated container instance")
	}
	if command == "list" {
		f.StringVar(&project, "project", "", "optional project filter")
	}
	if command == "list" || command == "events" {
		f.StringVar(&after, "after", "", "next_cursor from preceding page")
		f.IntVar(&limit, "limit", 20, "page size, 1–100")
	}
	if err := f.Parse(rest); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			flags := map[string]string{}
			f.VisitAll(func(v *flag.Flag) { flags["--"+v.Name] = v.Usage })
			return map[string]any{"usage": usage, "command": command, "flags": flags}, 0, nil
		}
		return nil, 1, domain.Validation(err.Error())
	}
	if f.NArg() != 0 || (needsID && id == "") {
		return nil, 1, domain.Validation("put the composition ID before flags; unexpected or missing positional arguments")
	}
	if (command == "create" && strings.TrimSpace(name) == "") || ((command == "create" || command == "update") && strings.TrimSpace(image) == "") {
		return nil, 1, domain.Validation("create requires --name and --image; update requires --image")
	}
	if command == "update" && generation < 1 {
		return nil, 1, domain.Validation("--expected-generation must be positive")
	}
	if command == "wait" && (timeout <= 0 || timeout > 60*time.Second) {
		return nil, 1, domain.Validation("--timeout must be positive and at most 60s")
	}
	if command == "logs" {
		if since < 0 || since > 24*time.Hour || since%time.Second != 0 || logOptions.TailLines < 1 || logOptions.MaxBytes < 1 {
			return nil, 1, domain.Validation("log limits must be positive; --since must use whole seconds up to 24h")
		}
		logOptions.SinceSeconds = int64(since / time.Second)
	}
	token := getenv("ENVY_API_TOKEN")
	if *tokenFile != "" {
		data, err := os.ReadFile(*tokenFile)
		if err != nil {
			return nil, 1, fmt.Errorf("read API token file: %w", err)
		}
		token = strings.TrimSpace(string(data))
	}
	c, err := client.New(apiURL, token, nil)
	if err != nil {
		return nil, 1, err
	}
	var out any
	switch command {
	case "create":
		out, err = c.Create(ctx, domain.CreateRequest{Project: project, Baseline: baseline, Name: name, Overrides: map[string]domain.ComponentOverride{component: {Image: image}}, TTL: ttl}, key)
	case "update":
		out, err = c.Update(ctx, id, domain.UpdateRequest{ExpectedGeneration: generation, Overrides: map[string]domain.ComponentOverride{component: {Image: image}}})
	case "get", "inspect":
		out, err = c.Get(ctx, id)
	case "list":
		out, err = c.List(ctx, project, after, limit)
	case "logs":
		out, err = c.Logs(ctx, id, component, logOptions)
	case "events":
		out, err = c.Events(ctx, id, after, limit)
	case "endpoints":
		out, err = c.Endpoints(ctx, id)
	case "destroy":
		out, err = c.Destroy(ctx, id)
	case "wait":
		var composition domain.Composition
		composition, err = c.Wait(ctx, id, timeout)
		out = composition
		if err == nil {
			switch composition.Phase {
			case domain.PhaseReady, domain.PhaseDestroyed:
				return out, 0, nil
			case domain.PhaseFailed:
				return out, 1, nil
			default:
				return out, 2, nil
			}
		}
	}
	if err != nil {
		return nil, 1, err
	}
	return out, 0, nil
}
