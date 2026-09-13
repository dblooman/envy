package mcp

import (
	"context"
	"github.com/dblooman/envy/internal/client"
	"github.com/dblooman/envy/internal/domain"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type PreviewScope struct {
	Project   string `json:"project"`
	Baseline  string `json:"baseline"`
	Component string `json:"component"`
}
type PreviewDiscoverInput struct {
	PreviewScope
	Selection domain.PreviewSelection `json:"selection"`
}
type PreviewApproveInput struct {
	PreviewScope
	Approval domain.PreviewApproval `json:"approval"`
}

func addPreviewTools(s *sdk.Server, c *client.Client) {
	sdk.AddTool(s, &sdk.Tool{Name: "discover_preview_profile", Description: "Read an existing Deployment and propose preview configuration. Inspect blockers, dependency names and connectivity assumptions; never returns Secret values."}, func(ctx context.Context, _ *sdk.CallToolRequest, in PreviewDiscoverInput) (*sdk.CallToolResult, domain.PreviewReport, error) {
		out, err := c.DiscoverPreview(ctx, in.Project, in.Baseline, in.Component, in.Selection)
		return textResult("Review discovery and resolve blockers before approving."), out, err
	})
	sdk.AddTool(s, &sdk.Tool{Name: "approve_preview_profile", Description: "Approve a reviewed discovery with its inspection fingerprint, expected profile revision and explicit connectivity confirmation. Enables derived previews for this component and baseline."}, func(ctx context.Context, _ *sdk.CallToolRequest, in PreviewApproveInput) (*sdk.CallToolResult, domain.PreviewProfile, error) {
		out, err := c.ApprovePreview(ctx, in.Project, in.Baseline, in.Component, in.Approval)
		return textResult("Preview profile approval result."), out, err
	})
	sdk.AddTool(s, &sdk.Tool{Name: "inspect_preview_profile", Description: "Read the latest approved preview profile and revision."}, func(ctx context.Context, _ *sdk.CallToolRequest, in PreviewScope) (*sdk.CallToolResult, domain.PreviewProfile, error) {
		out, err := c.InspectPreview(ctx, in.Project, in.Baseline, in.Component)
		return textResult("Approved preview profile."), out, err
	})
}
