package github

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/dblooman/envy/internal/domain"
)

func (p *Provider) RepositoryInstalled(ctx context.Context, r domain.SourceRepository) (bool, error) {
	token, e := p.jwt()
	if e != nil {
		return false, e
	}

	var out struct {
		ID          int64   `json:"id"`
		SuspendedAt *string `json:"suspended_at"`
	}
	e = p.request(ctx, "GET", repoPath(r)+"/installation", token, nil, &out)
	var de *domain.Error
	if errors.As(e, &de) && de.Code == "not_found" {
		return false, nil
	}

	if e != nil {
		return false, e
	}

	return out.ID == r.InstallationID && out.SuspendedAt == nil, nil
}

func (p *Provider) Installations(ctx context.Context, page int) ([]domain.GitHubInstallation, error) {
	t, e := p.jwt()
	if e != nil {
		return nil, e
	}

	var out []domain.GitHubInstallation
	e = p.request(ctx, "GET", fmt.Sprintf("/app/installations?per_page=30&page=%d", page), t, nil, &out)
	return out, e
}

func (p *Provider) InstallationRepositories(ctx context.Context, id int64, page int) ([]domain.GitHubRepository, error) {
	t, e := p.jwt()
	if e != nil {
		return nil, e
	}

	var token struct {
		Token string `json:"token"`
	}
	e = p.request(ctx, "POST", fmt.Sprintf("/app/installations/%d/access_tokens", id), t, map[string]any{"permissions": map[string]string{"metadata": "read"}}, &token)
	if e != nil {
		return nil, e
	}

	var out struct {
		Repositories []domain.GitHubRepository `json:"repositories"`
	}
	e = p.request(ctx, "GET", fmt.Sprintf("/installation/repositories?per_page=30&page=%d", page), token.Token, nil, &out)
	return out.Repositories, e
}

func (p *Provider) previewRequest(ctx context.Context, r domain.SourceRepository, permission, method, path string, in, out any) error {
	t, e := p.scopedToken(ctx, r, map[string]string{permission: map[bool]string{true: "read", false: "write"}[method == "GET"]})
	if e != nil {
		return e
	}

	return p.request(ctx, method, repoPath(r)+path, t, in, out)
}

func (p *Provider) PreviewAccess(ctx context.Context, r domain.SourceRepository) (domain.GitHubRepository, error) {
	var out domain.GitHubRepository
	t, e := p.scopedToken(ctx, r, map[string]string{"contents": "read", "pull_requests": "write", "deployments": "write", "actions": "read"})
	if e != nil {
		return out, e
	}

	e = p.request(ctx, "GET", repoPath(r), t, nil, &out)
	if e == nil && (out.ID < 1 || !strings.EqualFold(out.FullName, r.GitHubRepository)) {
		return out, domain.Validation("GitHub repository identity changed; review source registration")
	}

	return out, e
}

func (p *Provider) PullRequest(ctx context.Context, r domain.SourceRepository, n int) (domain.GitHubPR, error) {
	var out domain.GitHubPR
	e := p.previewRequest(ctx, r, "pull_requests", "GET", fmt.Sprintf("/pulls/%d", n), nil, &out)
	return out, e
}

func (p *Provider) PullRequests(ctx context.Context, r domain.SourceRepository, page int) ([]domain.GitHubPR, error) {
	var out []domain.GitHubPR
	e := p.previewRequest(ctx, r, "pull_requests", "GET", fmt.Sprintf("/pulls?state=open&per_page=30&page=%d", page), nil, &out)
	return out, e
}

func (p *Provider) WorkflowRuns(ctx context.Context, r domain.SourceRepository, id int64, sha string, page int) ([]domain.GitHubRun, error) {
	var out struct {
		Runs []domain.GitHubRun `json:"workflow_runs"`
	}
	e := p.previewRequest(ctx, r, "actions", "GET", fmt.Sprintf("/actions/workflows/%d/runs?head_sha=%s&per_page=30&page=%d", id, url.QueryEscape(sha), page), nil, &out)
	return out.Runs, e
}

func (p *Provider) RunAttempt(ctx context.Context, r domain.SourceRepository, id string, attempt int) (domain.GitHubRun, error) {
	var out domain.GitHubRun
	e := p.previewRequest(ctx, r, "actions", "GET", "/actions/runs/"+url.PathEscape(id)+"/attempts/"+strconv.Itoa(attempt), nil, &out)
	return out, e
}

func (p *Provider) PreviewComment(ctx context.Context, r domain.SourceRepository, n int, id int64, marker, body string) (int64, error) {
	// Recover a successful POST whose response was lost, using our App bot identity.
	if id == 0 {
		t, e := p.jwt()
		if e != nil {
			return 0, e
		}

		var app struct {
			Slug string `json:"slug"`
		}
		if e = p.request(ctx, "GET", "/app", t, nil, &app); e != nil {
			return 0, e
		}

		for page := 1; ; page++ {
			var rows []struct {
				ID   int64  `json:"id"`
				Body string `json:"body"`
				User struct {
					Login string `json:"login"`
				} `json:"user"`
			}
			if e = p.previewRequest(ctx, r, "pull_requests", "GET", fmt.Sprintf("/issues/%d/comments?per_page=100&page=%d", n, page), nil, &rows); e != nil {
				return 0, e
			}

			for _, row := range rows {
				if row.User.Login == app.Slug+"[bot]" && strings.Contains(row.Body, marker) {
					id = row.ID
					break
				}
			}

			if id != 0 || len(rows) < 100 {
				break
			}
		}
	}

	method, path := "POST", fmt.Sprintf("/issues/%d/comments", n)
	if id != 0 {
		method, path = "PATCH", fmt.Sprintf("/issues/comments/%d", id)
	}

	var out struct {
		ID int64 `json:"id"`
	}
	e := p.previewRequest(ctx, r, "pull_requests", method, path, map[string]string{"body": marker + "\n" + body}, &out)
	return out.ID, e
}

func (p *Provider) PreviewDeployment(ctx context.Context, r domain.SourceRepository, sha, environment string) (int64, error) {
	// Recover deployment creation after a lost response.
	var rows []struct {
		ID int64 `json:"id"`
	}
	if e := p.previewRequest(ctx, r, "deployments", "GET", "/deployments?sha="+url.QueryEscape(sha)+"&environment="+url.QueryEscape(environment), nil, &rows); e != nil {
		return 0, e
	}

	if len(rows) > 0 {
		return rows[0].ID, nil
	}

	var out struct {
		ID int64 `json:"id"`
	}
	e := p.previewRequest(ctx, r, "deployments", "POST", "/deployments", map[string]any{"ref": sha, "environment": environment, "auto_merge": false, "required_contexts": []string{}, "transient_environment": true, "production_environment": false}, &out)
	return out.ID, e
}

func (p *Provider) DeploymentStatus(ctx context.Context, r domain.SourceRepository, id int64, state, endpoint string) error {
	var out map[string]any
	return p.previewRequest(ctx, r, "deployments", "POST", fmt.Sprintf("/deployments/%d/statuses", id), map[string]any{"state": state, "environment_url": endpoint, "auto_inactive": false}, &out)
}
