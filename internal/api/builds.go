package api

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"slices"
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

func repositoryPath(r *http.Request) (string, string) {
	return r.PathValue("project"), r.PathValue("repository")
}

func (h *handler) sourceRepositories(w http.ResponseWriter, r *http.Request) {
	after, limit, err := pagination(r)
	if err != nil {
		writeError(w, err)
		return
	}
	items, next, err := h.service.SourceRepositories(r.Context(), r.PathValue("project"), after, limit)
	writeResult(w, http.StatusOK, map[string]any{"items": items, "next_cursor": next}, err)
}

func (h *handler) registerSourceRepository(w http.ResponseWriter, r *http.Request) {
	var request domain.SourceRepository
	if !decodeJSON(w, r, &request, catalogJSONError, catalogJSONExtra) {
		return
	}
	if project := r.PathValue("project"); request.Project != "" && request.Project != project {
		writeError(w, domain.Validation("project must match URL"))
		return
	} else {
		request.Project = project
	}
	repository, err := h.service.RegisterSourceRepository(r.Context(), request)
	writeResult(w, http.StatusOK, repository, err)
}

func (h *handler) enableSourceRepository(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Enabled *bool `json:"enabled"`
	}
	if !decodeJSON(w, r, &request, catalogJSONError, catalogJSONExtra) {
		return
	}
	if request.Enabled == nil {
		writeError(w, domain.Validation("enabled is required"))
		return
	}
	project, repository := repositoryPath(r)
	updated, err := h.service.EnableSourceRepository(r.Context(), project, repository, *request.Enabled)
	writeResult(w, http.StatusOK, updated, err)
}

func repositoryPage(w http.ResponseWriter, r *http.Request) (int, bool) {
	page := 1
	if raw := r.URL.Query().Get("page"); raw != "" {
		var err error
		page, err = strconv.Atoi(raw)
		if err != nil {
			writeError(w, domain.Validation("invalid page"))
			return 0, false
		}
	}
	return page, true
}

func (h *handler) sourceBranches(w http.ResponseWriter, r *http.Request) {
	page, ok := repositoryPage(w, r)
	if !ok {
		return
	}
	project, repository := repositoryPath(r)
	items, err := h.service.SourceBranches(r.Context(), project, repository, page)
	writeResult(w, http.StatusOK, map[string]any{"items": items, "page": page, "has_more": len(items) == 30}, err)
}

func (h *handler) sourceCommits(w http.ResponseWriter, r *http.Request) {
	page, ok := repositoryPage(w, r)
	if !ok {
		return
	}
	project, repository := repositoryPath(r)
	items, err := h.service.SourceCommits(r.Context(), project, repository, r.URL.Query().Get("branch"), page)
	writeResult(w, http.StatusOK, map[string]any{"items": items, "page": page, "has_more": len(items) == 30}, err)
}

func (h *handler) resolveRevision(w http.ResponseWriter, r *http.Request) {
	after, limit, err := pagination(r)
	if err != nil {
		writeError(w, err)
		return
	}
	project, repository := repositoryPath(r)
	resolution, err := h.service.ResolveRevision(r.Context(), project, repository, r.URL.Query().Get("component"), r.URL.Query().Get("ref"), after, limit)
	writeResult(w, http.StatusOK, resolution, err)
}

func (h *handler) recordBuild(w http.ResponseWriter, r *http.Request) {
	scope, ok := r.Context().Value(buildScopeKey{}).(*BuildCredential)
	if !ok {
		writeError(w, &domain.Error{Code: "unauthorized", Message: "a scoped CI reporting credential is required"})
		return
	}
	var request domain.BuildReport
	if !decodeJSON(w, r, &request, catalogJSONError, catalogJSONExtra) {
		return
	}
	allowed := slices.Contains(scope.Components, request.Component)
	if !allowed {
		writeError(w, &domain.Error{Code: "unauthorized", Message: "CI credential does not permit this component"})
		return
	}
	project, repository := repositoryPath(r)
	build, err := h.service.RecordBuild(r.Context(), project, repository, request)
	writeResult(w, http.StatusOK, build, err)
}
