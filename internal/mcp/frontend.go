package mcp

import (
	"context"
	"fmt"
	"time"

	"github.com/dblooman/envy/internal/client"
	"github.com/dblooman/envy/internal/domain"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type BindFrontendInput struct {
	domain.FrontendKey
	domain.BindFrontendRequest
}
type ResolveFrontendInput struct {
	domain.FrontendKey
	TimeoutSeconds int `json:"timeout_seconds,omitempty" jsonschema:"Bounded wait for this exact revision; default 60 seconds, maximum 300"`
}
type PublishFrontendInput struct {
	domain.FrontendKey
	domain.PublishFrontendRequest
}
type FrontendCheckInput struct {
	domain.FrontendKey
	domain.FrontendCheckRequest
}

func frontendResult(out domain.FrontendBindingView, err error) (*sdk.CallToolResult, domain.FrontendBindingView, error) {
	if err != nil {
		return nil, out, err
	}

	check := out.CheckState
	if out.Binding.Check != nil {
		check = out.Binding.Check.Status + " (" + out.CheckState + ")"
	}

	return textResult(fmt.Sprintf("Frontend %s is bound to %s. Backend ready=%t; caller-reported browser check=%s.", out.Binding.Frontend, out.Binding.Composition, out.Ready, check)), out, nil
}
func addFrontendTools(s *sdk.Server, c *client.Client) {
	sdk.AddTool(s, &sdk.Tool{Name: "bind_frontend", Description: "Persist an immutable association from an exact frontend commit to a composition. Identical retries are safe; another composition or repository conflicts."}, func(ctx context.Context, _ *sdk.CallToolRequest, in BindFrontendInput) (*sdk.CallToolResult, domain.FrontendBindingView, error) {
		out, err := c.BindFrontend(ctx, in.FrontendKey, in.BindFrontendRequest)
		return frontendResult(out, err)
	})
	sdk.AddTool(s, &sdk.Tool{Name: "get_frontend_binding", Description: "Inspect an exact revision binding, reported frontend URL, and current or stale browser evidence, including after composition expiry."}, func(ctx context.Context, _ *sdk.CallToolRequest, in domain.FrontendKey) (*sdk.CallToolResult, domain.FrontendBindingView, error) {
		out, err := c.FrontendBinding(ctx, in)
		return frontendResult(out, err)
	})
	sdk.AddTool(s, &sdk.Tool{Name: "resolve_frontend", Description: "Resolve the ready API URL for an exact frontend revision with a bounded wait. Missing, expired or unready compositions never fall back to staging. Cancellation changes nothing."}, func(ctx context.Context, _ *sdk.CallToolRequest, in ResolveFrontendInput) (*sdk.CallToolResult, domain.FrontendResolution, error) {
		if in.TimeoutSeconds < 0 || in.TimeoutSeconds > 300 {
			return nil, domain.FrontendResolution{}, domain.Validation("timeout_seconds must be between 0 and 300")
		}

		if in.TimeoutSeconds == 0 {
			in.TimeoutSeconds = 60
		}

		out, err := c.ResolveFrontend(ctx, in.FrontendKey, time.Duration(in.TimeoutSeconds)*time.Second)
		if err != nil {
			return nil, out, err
		}

		return textResult("Resolved API URL; this is readiness evidence, not a passing application test."), out, nil
	})
	sdk.AddTool(s, &sdk.Tool{Name: "publish_frontend", Description: "Record a caller-reported frontend deployment URL with the binding's expected_version. Does not deploy hosting or verify the URL; a changed URL clears earlier browser evidence."}, func(ctx context.Context, _ *sdk.CallToolRequest, in PublishFrontendInput) (*sdk.CallToolResult, domain.FrontendBindingView, error) {
		out, err := c.PublishFrontend(ctx, in.FrontendKey, in.PublishFrontendRequest)
		return frontendResult(out, err)
	})
	sdk.AddTool(s, &sdk.Tool{Name: "report_frontend_check", Description: "Record a browser check actually performed by the caller, against the current binding version and composition generation. Describe checks and limitations; Envy does not execute tests."}, func(ctx context.Context, _ *sdk.CallToolRequest, in FrontendCheckInput) (*sdk.CallToolResult, domain.FrontendBindingView, error) {
		out, err := c.CheckFrontend(ctx, in.FrontendKey, in.FrontendCheckRequest)
		return frontendResult(out, err)
	})
	sdk.AddTool(s, &sdk.Tool{Name: "list_frontend_bindings", Description: "List a composition's frontend revision bindings and caller-reported browser evidence. Pass next_cursor as after."}, func(ctx context.Context, _ *sdk.CallToolRequest, in EventsInput) (*sdk.CallToolResult, client.Page[domain.FrontendBindingView], error) {
		out, err := c.FrontendBindings(ctx, in.ID, in.After, pageLimit(in.Limit))
		if err != nil {
			return nil, out, err
		}

		return textResult(fmt.Sprintf("Returned %d frontend bindings.", len(out.Items))), out, nil
	})
	sdk.AddTool(s, &sdk.Tool{Name: "list_compositions", Description: "Discover existing compositions within a project before creating another. Names are not unique identities; coordinate using the explicit composition ID. Pass next_cursor as after."}, func(ctx context.Context, _ *sdk.CallToolRequest, in CatalogInput) (*sdk.CallToolResult, client.CompositionsPage, error) {
		out, err := c.List(ctx, in.Project, in.After, pageLimit(in.Limit))
		if err != nil {
			return nil, out, err
		}

		return textResult(fmt.Sprintf("Returned %d compositions.", len(out.Items))), out, nil
	})
}
