package client

import (
	"context"
	"net/url"
	"strconv"

	"github.com/dblooman/envy/internal/domain"
)

func (c *Client) GitHubStatus(ctx context.Context) (map[string]any, error) {
	var out map[string]any
	e := c.request(ctx, "GET", "/v1/github/status", nil, "", &out)
	return out, e
}

func (c *Client) GitHubInstallations(ctx context.Context, page int) (GitPage[domain.GitHubInstallation], error) {
	var out GitPage[domain.GitHubInstallation]
	e := c.request(ctx, "GET", "/v1/github/installations?page="+strconv.Itoa(page), nil, "", &out)
	return out, e
}

func (c *Client) GitHubRepositories(ctx context.Context, id int64, page int) (GitPage[domain.GitHubRepository], error) {
	var out GitPage[domain.GitHubRepository]
	e := c.request(ctx, "GET", "/v1/github/installations/"+strconv.FormatInt(id, 10)+"/repositories?page="+strconv.Itoa(page), nil, "", &out)
	return out, e
}

func (c *Client) PreviewPolicies(ctx context.Context) (Page[domain.PRPreviewPolicy], error) {
	var out Page[domain.PRPreviewPolicy]
	e := c.request(ctx, "GET", "/v1/github/preview-policies", nil, "", &out)
	return out, e
}

func (c *Client) PRPreviews(ctx context.Context, project, after string, limit int) (Page[domain.PRPreview], error) {
	var out Page[domain.PRPreview]
	if limit < 1 || limit > 100 {
		return out, domain.Validation("limit must be between 1 and 100")
	}

	q := url.Values{"project": {project}, "after": {after}, "limit": {strconv.Itoa(limit)}}
	e := c.request(ctx, "GET", "/v1/github/previews?"+q.Encode(), nil, "", &out)
	return out, e
}

func (c *Client) PRPreview(ctx context.Context, id string) (domain.PRPreview, error) {
	var out domain.PRPreview
	e := c.request(ctx, "GET", "/v1/github/previews/"+url.PathEscape(id), nil, "", &out)
	return out, e
}

func (c *Client) ControlPRPreview(ctx context.Context, id, action string) (domain.PRPreview, error) {
	var out domain.PRPreview
	if action != "stop" && action != "restart" {
		return out, domain.Validation("invalid preview action")
	}

	e := c.request(ctx, "POST", "/v1/github/previews/"+url.PathEscape(id)+"/"+action, nil, "", &out)
	return out, e
}

func (c *Client) SavePreviewPolicy(ctx context.Context, p domain.PRPreviewPolicy) (domain.PRPreviewPolicy, error) {
	var out domain.PRPreviewPolicy
	path, e := sourcePath(p.Project, p.Repository)
	if e != nil {
		return out, e
	}

	e = c.request(ctx, "PUT", path+"/preview-policy", p, "", &out)
	return out, e
}

func (c *Client) PreviewPolicy(ctx context.Context, project, repository string) (domain.PRPreviewPolicy, error) {
	var out domain.PRPreviewPolicy
	path, e := sourcePath(project, repository)
	if e != nil {
		return out, e
	}

	e = c.request(ctx, "GET", path+"/preview-policy", nil, "", &out)
	return out, e
}
