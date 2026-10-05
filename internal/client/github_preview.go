package client

import (
	"context"
	"net/url"
	"strconv"

	"github.com/dblooman/envy/internal/domain"
)

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
