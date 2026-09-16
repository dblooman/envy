// Package mcp adapts the authoritative REST API to semantic MCP tools.
package mcp

import (
	"context"
	"fmt"
	"time"

	"github.com/dblooman/envy/internal/client"
	"github.com/dblooman/envy/internal/domain"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type CreateInput struct {
	MessageIsolation         bool                                `json:"message_isolation,omitempty" jsonschema:"Isolate Pub/Sub messages for this composition; immutable. Choose explicitly based on schema, behavior and downstream effects. Consumer deployment is optional"`
	ExpectedPreviewRevisions map[string]int64                    `json:"expected_preview_revisions,omitempty"`
	Project                  string                              `json:"project" jsonschema:"Project owning the registered baseline and component"`
	Baseline                 string                              `json:"baseline" jsonschema:"Registered baseline identifier"`
	Name                     string                              `json:"name" jsonschema:"Human-readable composition name"`
	Overrides                map[string]domain.ComponentOverride `json:"overrides" jsonschema:"Zero to three overrides: each selects image OR build_id. An empty object creates a preview that inherits the complete baseline. Never supply source; it is server-resolved provenance"`
	TTL                      string                              `json:"ttl,omitempty" jsonschema:"Positive Go duration; defaults to 8h with a 24h maximum"`
	IdempotencyKey           string                              `json:"idempotency_key,omitempty" jsonschema:"Optional stable retry key"`
}

type UpdateInput struct {
	ExpectedPreviewRevisions map[string]int64                    `json:"expected_preview_revisions,omitempty"`
	ID                       string                              `json:"id" jsonschema:"Composition identifier"`
	ExpectedGeneration       int64                               `json:"expected_generation" jsonschema:"Current desired generation; stale updates are rejected"`
	Overrides                map[string]domain.ComponentOverride `json:"overrides" jsonschema:"Complete desired override set using image OR build_id per component; an empty object inherits the complete baseline; omit source"`
}

type LogsInput struct {
	ID           string `json:"id"`
	Component    string `json:"component"`
	TailLines    int64  `json:"tail_lines,omitempty"`
	MaxBytes     int64  `json:"max_bytes,omitempty"`
	SinceSeconds int64  `json:"since_seconds,omitempty"`
	Previous     bool   `json:"previous,omitempty"`
}

type EventsInput struct {
	ID    string `json:"id"`
	After string `json:"after,omitempty"`
	Limit int    `json:"limit,omitempty"`
}

type IDInput struct {
	ID string `json:"id" jsonschema:"Composition identifier returned by create_composition"`
}
type WaitInput struct {
	ID             string `json:"id" jsonschema:"Composition identifier"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty" jsonschema:"Wait timeout in seconds; defaults to 30, maximum 60"`
}

// NewServer exposes REST-backed operations and local recipe validation. Its typed tools publish both
// input and output schemas and return structured content plus readable text.
func NewServer(c *client.Client) *sdk.Server {
	s := sdk.NewServer(&sdk.Implementation{Name: "envy", Version: "0.1.0"}, nil)
	sdk.AddTool(s, &sdk.Tool{Name: "create_composition", Description: "Create a temporary composition from a registered baseline and prebuilt workload override; poll for readiness."}, func(ctx context.Context, _ *sdk.CallToolRequest, in CreateInput) (*sdk.CallToolResult, domain.Composition, error) {
		out, err := c.Create(ctx, domain.CreateRequest{MessageIsolation: in.MessageIsolation, ExpectedPreviewRevisions: in.ExpectedPreviewRevisions, Project: in.Project, Baseline: in.Baseline, Name: in.Name, Overrides: in.Overrides, TTL: in.TTL}, in.IdempotencyKey)
		return compositionResult(out, err)
	})
	sdk.AddTool(s, &sdk.Tool{Name: "get_composition", Description: "Get the desired and observed state of a composition."}, func(ctx context.Context, _ *sdk.CallToolRequest, in IDInput) (*sdk.CallToolResult, domain.Composition, error) {
		out, err := c.Get(ctx, in.ID)
		return compositionResult(out, err)
	})
	sdk.AddTool(s, &sdk.Tool{Name: "wait_for_composition", Description: "Wait until ready, failed, or destroyed, returning the latest status after at most 60 seconds. Cancelling never destroys the composition."}, func(ctx context.Context, _ *sdk.CallToolRequest, in WaitInput) (*sdk.CallToolResult, domain.Composition, error) {
		if in.TimeoutSeconds < 0 || in.TimeoutSeconds > 60 {
			return nil, domain.Composition{}, &domain.Error{Code: "validation_error", Message: "timeout_seconds must be between 1 and 60, or omitted"}
		}

		out, err := c.Wait(ctx, in.ID, time.Duration(in.TimeoutSeconds)*time.Second)
		return compositionResult(out, err)
	})
	sdk.AddTool(s, &sdk.Tool{Name: "get_composition_endpoints", Description: "Get allocated composition URLs and observed endpoint readiness."}, func(ctx context.Context, _ *sdk.CallToolRequest, in IDInput) (*sdk.CallToolResult, client.EndpointsResponse, error) {
		out, err := c.Endpoints(ctx, in.ID)
		if err != nil {
			return nil, out, err
		}

		return textResult(fmt.Sprintf("Composition %s endpoints returned; inspect ready before use.", out.ID)), out, nil
	})
	sdk.AddTool(s, &sdk.Tool{Name: "destroy_composition", Description: "Request durable composition cleanup. Repeat safely and inspect status until destroyed."}, func(ctx context.Context, _ *sdk.CallToolRequest, in IDInput) (*sdk.CallToolResult, domain.Composition, error) {
		out, err := c.Destroy(ctx, in.ID)
		return compositionResult(out, err)
	})

	sdk.AddTool(s, &sdk.Tool{Name: "update_composition", Description: "Update a ready or failed composition's image with an expected generation. Preserves its ID, URL, and expiry; poll for new readiness."}, func(ctx context.Context, _ *sdk.CallToolRequest, in UpdateInput) (*sdk.CallToolResult, domain.Composition, error) {
		out, err := c.Update(ctx, in.ID, domain.UpdateRequest{ExpectedPreviewRevisions: in.ExpectedPreviewRevisions, ExpectedGeneration: in.ExpectedGeneration, Overrides: in.Overrides})
		return compositionResult(out, err)
	})

	sdk.AddTool(s, &sdk.Tool{Name: "get_component_logs", Description: "Read bounded application container logs from at most three pods. Inherited logs are explicitly shared-baseline, with no composition filtering. Defaults: 200 lines/pod, 65536 total bytes. Maximums: 1000 lines/pod, 262144 bytes, since_seconds 86400."}, func(ctx context.Context, _ *sdk.CallToolRequest, in LogsInput) (*sdk.CallToolResult, domain.ComponentLogs, error) {
		out, err := c.Logs(ctx, in.ID, in.Component, domain.LogOptions{TailLines: in.TailLines, MaxBytes: in.MaxBytes, SinceSeconds: in.SinceSeconds, Previous: in.Previous})
		if err != nil {
			return nil, out, err
		}

		return textResult(fmt.Sprintf("%s Returned %d pod log snapshots; partial=%t, truncated=%t.", out.Message, len(out.Streams), out.Partial, out.Truncated)), out, nil
	})
	sdk.AddTool(s, &sdk.Tool{Name: "list_composition_events", Description: "List durable Envy lifecycle events oldest first, including retained tombstones. Pass next_cursor as after. Default limit 20, maximum 100."}, func(ctx context.Context, _ *sdk.CallToolRequest, in EventsInput) (*sdk.CallToolResult, domain.EventsPage, error) {
		if in.Limit == 0 {
			in.Limit = 20
		}

		out, err := c.Events(ctx, in.ID, in.After, in.Limit)
		if err != nil {
			return nil, out, err
		}

		return textResult(fmt.Sprintf("Returned %d lifecycle events; next cursor: %s.", len(out.Items), out.NextCursor)), out, nil
	})
	addCatalogTools(s, c)
	addPreviewTools(s, c)
	addBuildTools(s, c)
	addPRPreviewTools(s, c)
	addFrontendTools(s, c)
	addRecipeTools(s, c)
	return s
}

func Run(ctx context.Context, c *client.Client) error {
	return NewServer(c).Run(ctx, &sdk.StdioTransport{})
}

func compositionResult(out domain.Composition, err error) (*sdk.CallToolResult, domain.Composition, error) {
	if err != nil {
		return nil, out, err
	}

	return textResult(fmt.Sprintf("Composition %s is %s.", out.ID, out.Phase)), out, nil
}

func textResult(text string) *sdk.CallToolResult {
	return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: text}}}
}
