package mcp

import (
	"context"

	"github.com/dblooman/envy/internal/client"
	"github.com/dblooman/envy/internal/domain"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func addPRPreviewTools(s *sdk.Server, c *client.Client) {
	sdk.AddTool(s, &sdk.Tool{Name: "list_pr_previews", Description: "List GitHub label-owned previews and requested/deployed revisions."}, func(ctx context.Context, _ *sdk.CallToolRequest, in CatalogInput) (*sdk.CallToolResult, client.Page[domain.PRPreview], error) {
		out, e := c.PRPreviews(ctx, in.Project, in.After, pageLimit(in.Limit))
		return textResult("PR previews."), out, e
	})
	type input struct {
		ID string `json:"id"`
	}
	for _, action := range []string{"get", "stop", "restart"} {
		sdk.AddTool(s, &sdk.Tool{Name: action + "_pr_preview", Description: action + " a label-owned PR preview. Restart requires an open labelled same-repository PR; stop prevents automatic recreation."}, func(ctx context.Context, _ *sdk.CallToolRequest, in input) (*sdk.CallToolResult, domain.PRPreview, error) {
			var out domain.PRPreview
			var e error
			if action == "get" {
				out, e = c.PRPreview(ctx, in.ID)
			} else {
				out, e = c.ControlPRPreview(ctx, in.ID, action)
			}

			return textResult("PR preview."), out, e
		})
	}
}
