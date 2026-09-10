package mcp

import (
	"context"

	"github.com/dblooman/envy/internal/client"
	"github.com/dblooman/envy/internal/domain"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type SourceInput struct {
	Project    string `json:"project"`
	Repository string `json:"repository"`
	Page       int    `json:"page,omitempty"`
	Branch     string `json:"branch,omitempty"`
}
type ResolveInput struct {
	Project    string `json:"project"`
	Repository string `json:"repository"`
	Component  string `json:"component"`
	Ref        string `json:"ref"`
	After      string `json:"after,omitempty"`
	Limit      int    `json:"limit,omitempty"`
}

func addBuildTools(s *sdk.Server, c *client.Client) {
	sdk.AddTool(s, &sdk.Tool{Name: "list_source_repositories", Description: "Discover explicitly registered repository/component image mappings. Disabled repositories cannot supply new builds or overrides."}, func(ctx context.Context, _ *sdk.CallToolRequest, in CatalogInput) (*sdk.CallToolResult, client.Page[domain.SourceRepository], error) {
		out, err := c.SourceRepositories(ctx, in.Project, in.After, pageLimit(in.Limit))
		return textResult("Registered source repositories."), out, err
	})
	sdk.AddTool(s, &sdk.Tool{Name: "list_source_branches", Description: "List accessible branches, 30 per page."}, func(ctx context.Context, _ *sdk.CallToolRequest, in SourceInput) (*sdk.CallToolResult, client.GitPage[domain.GitBranch], error) {
		if in.Page == 0 {
			in.Page = 1
		}
		out, err := c.SourceBranches(ctx, in.Project, in.Repository, in.Page)
		return textResult("GitHub branches."), out, err
	})
	sdk.AddTool(s, &sdk.Tool{Name: "list_source_commits", Description: "Browse commits on a branch, 30 per page."}, func(ctx context.Context, _ *sdk.CallToolRequest, in SourceInput) (*sdk.CallToolResult, client.GitPage[domain.GitCommit], error) {
		if in.Page == 0 {
			in.Page = 1
		}
		out, err := c.SourceCommits(ctx, in.Project, in.Repository, in.Branch, in.Page)
		return textResult("GitHub commits."), out, err
	})
	sdk.AddTool(s, &sdk.Tool{Name: "resolve_source_revision", Description: "Resolve a branch or full commit SHA and list its published builds. Freeze the returned SHA; select an explicit build_id for create/update. No builds means CI must publish first; this never starts CI."}, func(ctx context.Context, _ *sdk.CallToolRequest, in ResolveInput) (*sdk.CallToolResult, domain.RevisionResolution, error) {
		out, err := c.ResolveRevision(ctx, in.Project, in.Repository, in.Component, in.Ref, in.After, pageLimit(in.Limit))
		return textResult("Resolved revision and available builds."), out, err
	})
}
