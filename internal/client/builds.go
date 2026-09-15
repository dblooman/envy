package client

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	"github.com/dblooman/envy/internal/domain"
)

type GitPage[T any] struct {
	Items   []T  `json:"items"`
	Page    int  `json:"page"`
	HasMore bool `json:"has_more"`
}

func sourcePath(project, repository string) (string, error) {
	path, err := catalogPath(project)
	if err != nil {
		return "", err
	}

	if !domain.ValidCatalogID(repository) {
		return "", domain.Validation("invalid repository ID")
	}

	return path + "/repositories/" + repository, nil
}
func (c *Client) SourceRepositories(ctx context.Context, project, after string, limit int) (Page[domain.SourceRepository], error) {
	path, err := catalogPath(project)
	if err != nil {
		return Page[domain.SourceRepository]{}, err
	}

	return catalogList[domain.SourceRepository](ctx, c, path+"/repositories", after, limit)
}
func (c *Client) RegisterSourceRepository(ctx context.Context, r domain.SourceRepository) (domain.SourceRepository, error) {
	var out domain.SourceRepository
	path, err := catalogPath(r.Project)
	if err != nil {
		return out, err
	}

	err = c.request(ctx, http.MethodPost, path+"/repositories", r, "", &out)
	return out, err
}
func (c *Client) EnableSourceRepository(ctx context.Context, project, repository string, enabled bool) (domain.SourceRepository, error) {
	var out domain.SourceRepository
	path, err := sourcePath(project, repository)
	if err != nil {
		return out, err
	}

	err = c.request(ctx, http.MethodPatch, path, map[string]bool{"enabled": enabled}, "", &out)
	return out, err
}
func (c *Client) SourceBranches(ctx context.Context, project, repository string, page int) (GitPage[domain.GitBranch], error) {
	var out GitPage[domain.GitBranch]
	path, err := sourcePath(project, repository)
	if err != nil {
		return out, err
	}

	err = c.request(ctx, http.MethodGet, path+"/branches?page="+strconv.Itoa(page), nil, "", &out)
	return out, err
}
func (c *Client) SourceCommits(ctx context.Context, project, repository, branch string, page int) (GitPage[domain.GitCommit], error) {
	var out GitPage[domain.GitCommit]
	path, err := sourcePath(project, repository)
	if err != nil {
		return out, err
	}

	q := url.Values{"branch": {branch}, "page": {strconv.Itoa(page)}}
	err = c.request(ctx, http.MethodGet, path+"/commits?"+q.Encode(), nil, "", &out)
	return out, err
}
func (c *Client) ResolveRevision(ctx context.Context, project, repository, component, ref, after string, limit int) (domain.RevisionResolution, error) {
	var out domain.RevisionResolution
	path, err := sourcePath(project, repository)
	if err != nil {
		return out, err
	}

	q := url.Values{"component": {component}, "ref": {ref}, "after": {after}, "limit": {strconv.Itoa(limit)}}
	err = c.request(ctx, http.MethodGet, path+"/resolve?"+q.Encode(), nil, "", &out)
	return out, err
}
func (c *Client) RecordBuild(ctx context.Context, project, repository string, report domain.BuildReport) (domain.Build, error) {
	var out domain.Build
	path, err := sourcePath(project, repository)
	if err != nil {
		return out, err
	}

	err = c.request(ctx, http.MethodPost, path+"/builds", report, "", &out)
	return out, err
}
