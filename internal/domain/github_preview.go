package domain

import (
	"context"
	"time"
)

const PreviewLabel = "envy-preview"

type GitHubInstallation struct {
	ID      int64 `json:"id"`
	Account struct {
		Login string `json:"login"`
	} `json:"account"`
	Permissions map[string]string `json:"permissions"`
}
type GitHubRepository struct {
	ID       int64  `json:"id"`
	FullName string `json:"full_name"`
}
type GitHubPR struct {
	UpdatedAt time.Time `json:"updated_at"`
	Number    int       `json:"number"`
	State     string    `json:"state"`
	URL       string    `json:"html_url"`
	Head      struct {
		SHA  string           `json:"sha"`
		Repo GitHubRepository `json:"repo"`
	} `json:"head"`
	Base struct {
		Repo GitHubRepository `json:"repo"`
	} `json:"base"`
	Labels []struct {
		Name string `json:"name"`
	} `json:"labels"`
}

func (p GitHubPR) Requested() bool {
	if p.State != "open" {
		return false
	}

	for _, l := range p.Labels {
		if l.Name == PreviewLabel {
			return true
		}
	}

	return false
}

type GitHubRun struct {
	ID         int64  `json:"id"`
	WorkflowID int64  `json:"workflow_id"`
	Number     int64  `json:"run_number"`
	Attempt    int    `json:"run_attempt"`
	SHA        string `json:"head_sha"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
}
type PRPreviewPolicy struct {
	Project            string   `json:"project"`
	Repository         string   `json:"repository"`
	GitHubRepositoryID int64    `json:"github_repository_id"`
	Enabled            bool     `json:"enabled"`
	Baseline           string   `json:"baseline"`
	Components         []string `json:"components"`
	TTL                string   `json:"ttl"`
	WorkflowID         int64    `json:"workflow_id"`
}
type PRPreview struct {
	ID                    string                       `json:"id"`
	Version               int64                        `json:"version"`
	Policy                PRPreviewPolicy              `json:"policy"`
	Number                int                          `json:"number"`
	PRURL                 string                       `json:"pr_url"`
	Lifecycle             int64                        `json:"lifecycle"`
	Requested             bool                         `json:"requested"`
	Terminal              bool                         `json:"terminal"`
	RestartBarrierAt      time.Time                    `json:"restart_barrier_at"`
	LastStopEventAt       time.Time                    `json:"last_stop_event_at"`
	RequestedSHA          string                       `json:"requested_sha"`
	DeployedSHA           string                       `json:"deployed_sha"`
	SelectedSHA           string                       `json:"selected_sha"`
	Builds                map[string]ComponentOverride `json:"builds,omitempty"`
	RunNumber             int64                        `json:"run_number"`
	Attempt               int                          `json:"attempt"`
	CompositionID         string                       `json:"composition_id,omitempty"`
	Generation            int64                        `json:"generation"`
	URL                   string                       `json:"url,omitempty"`
	ExpiresAt             time.Time                    `json:"expires_at,omitzero"`
	Status                string                       `json:"status"`
	DeploymentStatus      string                       `json:"deployment_status,omitempty"`
	Reason                string                       `json:"reason,omitempty"`
	UpdatedAt             time.Time                    `json:"updated_at"`
	CommentID             int64                        `json:"comment_id,omitempty"`
	DeploymentID          int64                        `json:"deployment_id,omitempty"`
	DeploymentSHA         string                       `json:"deployment_sha,omitempty"`
	FeedbackHash          string                       `json:"feedback_hash,omitempty"`
	FeedbackError         string                       `json:"feedback_error,omitempty"`
	RetiringDeploymentIDs []int64                      `json:"retiring_deployment_ids,omitempty"`
}
type GitHubPreviewProvider interface {
	RepositoryInstalled(context.Context, SourceRepository) (bool, error)
	Installations(context.Context, int) ([]GitHubInstallation, error)
	InstallationRepositories(context.Context, int64, int) ([]GitHubRepository, error)
	PreviewAccess(context.Context, SourceRepository) (GitHubRepository, error)
	PullRequest(context.Context, SourceRepository, int) (GitHubPR, error)
	PullRequests(context.Context, SourceRepository, int) ([]GitHubPR, error)
	WorkflowRuns(context.Context, SourceRepository, int64, string, int) ([]GitHubRun, error)
	RunAttempt(context.Context, SourceRepository, string, int) (GitHubRun, error)
	PreviewComment(context.Context, SourceRepository, int, int64, string, string) (int64, error)
	PreviewDeployment(context.Context, SourceRepository, string, string) (int64, error)
	DeploymentStatus(context.Context, SourceRepository, int64, string, string) error
}

// Claims fence controller mutations against concurrent stop/restart requests.
type PRPreviewClaim struct {
	ID      string
	Version int64
}
type previewClaimKey struct{}

func WithPRPreviewClaim(ctx context.Context, p PRPreview) context.Context {
	return context.WithValue(ctx, previewClaimKey{}, PRPreviewClaim{p.ID, p.Version})
}

func PreviewClaim(ctx context.Context) (PRPreviewClaim, bool) {
	c, ok := ctx.Value(previewClaimKey{}).(PRPreviewClaim)
	return c, ok
}
