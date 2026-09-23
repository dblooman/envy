package mcp

import (
	"context"
	"fmt"

	"github.com/dblooman/envy/internal/client"
	"github.com/dblooman/envy/internal/domain"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type PageInput struct {
	After string `json:"after,omitempty"`
	Limit int    `json:"limit,omitempty"`
}
type CatalogInput struct {
	Project string `json:"project"`
	After   string `json:"after,omitempty"`
	Limit   int    `json:"limit,omitempty"`
}
type ComponentInput struct {
	Project   string `json:"project"`
	Component string `json:"component"`
}
type DraftInput struct {
	Project string `json:"project"`
}
type DeleteDraftInput struct {
	Project  string `json:"project"`
	Revision int64  `json:"revision"`
}

func pageLimit(n int) int {
	if n == 0 {
		return 20
	}

	return n
}

func addCatalogTools(s *sdk.Server, c *client.Client) {
	sdk.AddTool(s, &sdk.Tool{Name: "get_onboarding_draft", Description: "Load this authenticated author's non-secret preparation draft for a project."}, func(ctx context.Context, _ *sdk.CallToolRequest, in DraftInput) (*sdk.CallToolResult, domain.OnboardingDraft, error) {
		out, err := c.GetOnboardingDraft(ctx, in.Project)
		if err != nil {
			return nil, out, err
		}

		return textResult("Loaded preparation draft revision."), out, nil
	})
	sdk.AddTool(s, &sdk.Tool{Name: "save_onboarding_draft", Description: "Save non-secret preparation with optimistic revision; revision 0 creates a draft. A draft is not approval."}, func(ctx context.Context, _ *sdk.CallToolRequest, in domain.OnboardingDraft) (*sdk.CallToolResult, domain.OnboardingDraft, error) {
		out, err := c.SaveOnboardingDraft(ctx, in)
		if err != nil {
			return nil, out, err
		}

		return textResult("Saved preparation draft revision."), out, nil
	})
	sdk.AddTool(s, &sdk.Tool{Name: "delete_onboarding_draft", Description: "Discard this author's preparation draft with its current revision; creates no preview resources."}, func(ctx context.Context, _ *sdk.CallToolRequest, in DeleteDraftInput) (*sdk.CallToolResult, map[string]any, error) {
		if err := c.DeleteOnboardingDraft(ctx, in.Project, in.Revision); err != nil {
			return nil, nil, err
		}

		out := map[string]any{"project": in.Project, "deleted": true}
		return textResult("Preparation draft deleted."), out, nil
	})
	sdk.AddTool(s, &sdk.Tool{Name: "list_projects", Description: "Discover registered projects. Pagination defaults to 20, maximum 100; pass next_cursor as after."}, func(ctx context.Context, _ *sdk.CallToolRequest, in PageInput) (*sdk.CallToolResult, client.Page[domain.Project], error) {
		out, err := c.Projects(ctx, in.After, pageLimit(in.Limit))
		if err != nil {
			return nil, out, err
		}

		return textResult(fmt.Sprintf("Returned %d projects.", len(out.Items))), out, nil
	})
	sdk.AddTool(s, &sdk.Tool{Name: "list_components", Description: "List a project's approved component profiles. Pass next_cursor as after."}, func(ctx context.Context, _ *sdk.CallToolRequest, in CatalogInput) (*sdk.CallToolResult, client.Page[domain.Component], error) {
		out, err := c.Components(ctx, in.Project, in.After, pageLimit(in.Limit))
		if err != nil {
			return nil, out, err
		}

		return textResult(fmt.Sprintf("Returned %d components.", len(out.Items))), out, nil
	})
	sdk.AddTool(s, &sdk.Tool{Name: "list_baselines", Description: "List a project's registered baseline bindings and verification contracts. Pass next_cursor as after."}, func(ctx context.Context, _ *sdk.CallToolRequest, in CatalogInput) (*sdk.CallToolResult, client.Page[domain.Baseline], error) {
		out, err := c.Baselines(ctx, in.Project, in.After, pageLimit(in.Limit))
		if err != nil {
			return nil, out, err
		}

		return textResult(fmt.Sprintf("Returned %d baselines.", len(out.Items))), out, nil
	})
	sdk.AddTool(s, &sdk.Tool{Name: "get_component", Description: "Inspect one approved component profile within its project."}, func(ctx context.Context, _ *sdk.CallToolRequest, in ComponentInput) (*sdk.CallToolResult, domain.Component, error) {
		out, err := c.Component(ctx, in.Project, in.Component)
		if err != nil {
			return nil, out, err
		}

		return textResult("Component " + out.Project + "/" + out.ID + "."), out, nil
	})
}
