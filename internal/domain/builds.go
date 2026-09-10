package domain

import (
	"context"
	"time"
)

// SourceRepository explicitly opts a repository into a project's compositions.
// Identity and image mappings are immutable; Enabled is the only mutable field.
type SourceRepository struct {
	Project          string            `json:"project"`
	ID               string            `json:"id"`
	GitHubRepository string            `json:"github_repository"`
	InstallationID   int64             `json:"installation_id"`
	Enabled          bool              `json:"enabled"`
	Images           map[string]string `json:"images"`
}
type BuildReport struct {
	Component string    `json:"component"`
	Revision  string    `json:"revision"`
	Image     string    `json:"image"`
	RunID     string    `json:"run_id"`
	Attempt   int       `json:"attempt"`
	BuiltAt   time.Time `json:"built_at"`
}
type Build struct {
	BuildReport
	ID               string `json:"id"`
	Project          string `json:"project"`
	Repository       string `json:"repository"`
	GitHubRepository string `json:"github_repository"`
	RunURL           string `json:"run_url"`
}
type GitCommit struct {
	SHA     string `json:"sha"`
	Message string `json:"message"`
}
type GitBranch struct {
	Name string `json:"name"`
	SHA  string `json:"sha"`
}
type RevisionResolution struct {
	Repository SourceRepository `json:"repository"`
	Commit     GitCommit        `json:"commit"`
	Builds     []Build          `json:"builds"`
	NextCursor string           `json:"next_cursor,omitempty"`
	CIURL      string           `json:"ci_url"`
}

// SourceControl checks live installation access, including removed repositories.
type SourceControl interface {
	Check(context.Context, SourceRepository) error
	Resolve(context.Context, SourceRepository, string) (GitCommit, error)
	Branches(context.Context, SourceRepository, int) ([]GitBranch, error)
	Commits(context.Context, SourceRepository, string, int) ([]GitCommit, error)
}
type ImageRegistry interface {
	Check(context.Context, string) error
}
