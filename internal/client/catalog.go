package client

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/dblooman/envy/internal/domain"
)

type Page[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"next_cursor,omitempty"`
}

func catalogPath(project string) (string, error) {
	if !domain.ValidCatalogID(project) {
		return "", domain.Validation("invalid project ID")
	}

	return "/v1/projects/" + project, nil
}

func catalogList[T any](ctx context.Context, c *Client, path, after string, limit int) (Page[T], error) {
	var out Page[T]
	if limit < 1 || limit > 100 {
		return out, domain.Validation("limit must be between 1 and 100")
	}

	q := url.Values{"after": {after}, "limit": {fmt.Sprint(limit)}}
	err := c.request(ctx, http.MethodGet, path+"?"+q.Encode(), nil, "", &out)
	return out, err
}

func (c *Client) Projects(ctx context.Context, after string, limit int) (Page[domain.Project], error) {
	return catalogList[domain.Project](ctx, c, "/v1/projects", after, limit)
}

func (c *Client) Components(ctx context.Context, project, after string, limit int) (Page[domain.Component], error) {
	path, err := catalogPath(project)
	if err != nil {
		return Page[domain.Component]{}, err
	}

	return catalogList[domain.Component](ctx, c, path+"/components", after, limit)
}

func (c *Client) Baselines(ctx context.Context, project, after string, limit int) (Page[domain.Baseline], error) {
	path, err := catalogPath(project)
	if err != nil {
		return Page[domain.Baseline]{}, err
	}

	return catalogList[domain.Baseline](ctx, c, path+"/baselines", after, limit)
}

func (c *Client) Component(ctx context.Context, project, id string) (domain.Component, error) {
	var out domain.Component
	path, err := catalogPath(project)
	if err != nil {
		return out, err
	}

	if !domain.ValidCatalogID(id) {
		return out, domain.Validation("invalid component ID")
	}

	err = c.request(ctx, http.MethodGet, path+"/components/"+id, nil, "", &out)
	return out, err
}

func (c *Client) Onboard(ctx context.Context, m domain.CatalogManifest, apply bool) (domain.CatalogReport, error) {
	var out domain.CatalogReport
	action := "validate"
	if apply {
		action = "apply"
	}

	err := c.request(ctx, http.MethodPost, "/v1/catalog/"+action, m, "", &out)
	return out, err
}
