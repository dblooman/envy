package api

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"strconv"
	"strings"

	"github.com/dblooman/envy/internal/domain"
)

// BuildCredential is server-only configuration. These tokens authorize only
// build reporting to their exact project/repository/component scope.
type BuildCredential struct {
	Token      string   `json:"token"`
	Project    string   `json:"project"`
	Repository string   `json:"repository"`
	Components []string `json:"components"`
}
type buildScopeKey struct{}

func buildCredential(r *http.Request, credentials []BuildCredential) *BuildCredential {
	if r.Method != "POST" {
		return nil
	}
	values := r.Header.Values("Authorization")
	if len(values) != 1 || !strings.HasPrefix(values[0], "Bearer ") {
		return nil
	}
	token := strings.TrimPrefix(values[0], "Bearer ")
	hash := sha256.Sum256([]byte(token))
	for _, c := range credentials {
		expected := sha256.Sum256([]byte(c.Token))
		if c.Token != "" && subtle.ConstantTimeCompare(hash[:], expected[:]) == 1 && r.URL.EscapedPath() == "/v1/projects/"+c.Project+"/repositories/"+c.Repository+"/builds" {
			return &c
		}
	}
	return nil
}

type buildService interface {
	SourceRepositories(context.Context, string, string, int) ([]domain.SourceRepository, string, error)
	RegisterSourceRepository(context.Context, domain.SourceRepository) (domain.SourceRepository, error)
	EnableSourceRepository(context.Context, string, string, bool) (domain.SourceRepository, error)
	SourceBranches(context.Context, string, string, int) ([]domain.GitBranch, error)
	SourceCommits(context.Context, string, string, string, int) ([]domain.GitCommit, error)
	ResolveRevision(context.Context, string, string, string, string, string, int) (domain.RevisionResolution, error)
	RecordBuild(context.Context, string, string, domain.BuildReport) (domain.Build, error)
}

func (h *handler) builds(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	s, ok := h.service.(buildService)
	if !ok {
		writeError(w, &domain.Error{Code: "unavailable", Message: "build catalog is unavailable"})
		return
	}
	ctx := r.Context()
	project, id := r.PathValue("project"), r.PathValue("repository")
	var out any
	var err error
	switch r.Pattern {
	case "GET /v1/projects/{project}/repositories":
		after, limit, e := pagination(r)
		if e != nil {
			writeError(w, e)
			return
		}
		var items []domain.SourceRepository
		var next string
		items, next, err = s.SourceRepositories(ctx, project, after, limit)
		out = map[string]any{"items": items, "next_cursor": next}
	case "POST /v1/projects/{project}/repositories":
		var req domain.SourceRepository
		if !decodeCatalog(w, r, &req) {
			return
		}
		if req.Project != "" && req.Project != project {
			writeError(w, domain.Validation("project must match URL"))
			return
		}
		req.Project = project
		out, err = s.RegisterSourceRepository(ctx, req)
	case "PATCH /v1/projects/{project}/repositories/{repository}":
		var req struct {
			Enabled *bool `json:"enabled"`
		}
		if !decodeCatalog(w, r, &req) {
			return
		}
		if req.Enabled == nil {
			writeError(w, domain.Validation("enabled is required"))
			return
		}
		out, err = s.EnableSourceRepository(ctx, project, id, *req.Enabled)
	case "GET /v1/projects/{project}/repositories/{repository}/branches", "GET /v1/projects/{project}/repositories/{repository}/commits":
		page := 1
		if raw := r.URL.Query().Get("page"); raw != "" {
			page, err = strconv.Atoi(raw)
			if err != nil {
				writeError(w, domain.Validation("invalid page"))
				return
			}
		}
		if strings.HasSuffix(r.URL.Path, "/branches") {
			var items []domain.GitBranch
			items, err = s.SourceBranches(ctx, project, id, page)
			out = map[string]any{"items": items, "page": page, "has_more": len(items) == 30}
		} else {
			var items []domain.GitCommit
			items, err = s.SourceCommits(ctx, project, id, r.URL.Query().Get("branch"), page)
			out = map[string]any{"items": items, "page": page, "has_more": len(items) == 30}
		}
	case "GET /v1/projects/{project}/repositories/{repository}/resolve":
		after, limit, e := pagination(r)
		if e != nil {
			writeError(w, e)
			return
		}
		out, err = s.ResolveRevision(ctx, project, id, r.URL.Query().Get("component"), r.URL.Query().Get("ref"), after, limit)
	case "POST /v1/projects/{project}/repositories/{repository}/builds":
		scope, ok := ctx.Value(buildScopeKey{}).(*BuildCredential)
		if !ok {
			writeError(w, &domain.Error{Code: "unauthorized", Message: "a scoped CI reporting credential is required"})
			return
		}
		var req domain.BuildReport
		if !decodeCatalog(w, r, &req) {
			return
		}
		allowed := false
		for _, component := range scope.Components {
			if component == req.Component {
				allowed = true
			}
		}
		if !allowed {
			writeError(w, &domain.Error{Code: "unauthorized", Message: "CI credential does not permit this component"})
			return
		}
		out, err = s.RecordBuild(ctx, project, id, req)
	}
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
