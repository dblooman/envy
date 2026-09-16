package api

import (
	"context"
	"io"
	"net/http"
	"strconv"

	"github.com/dblooman/envy/internal/domain"
)

type githubPreviewService interface {
	GitHubStatus(context.Context) (map[string]any, error)
	GitHubInstallations(context.Context, int) ([]domain.GitHubInstallation, error)
	GitHubRepositories(context.Context, int64, int) ([]domain.GitHubRepository, error)
	PreviewPolicies(context.Context) ([]domain.PRPreviewPolicy, error)
	SavePreviewPolicy(context.Context, domain.PRPreviewPolicy) (domain.PRPreviewPolicy, error)
	PRPreviews(context.Context, string, string, int) ([]domain.PRPreview, string, error)
	PRPreview(context.Context, string) (domain.PRPreview, error)
	ControlPRPreview(context.Context, string, string) (domain.PRPreview, error)
	ReceiveGitHubWebhook(context.Context, string, string, string, []byte) error
}

func (h *handler) installGitHubRoutes(root, v1 *http.ServeMux) {
	s, ok := h.service.(githubPreviewService)
	if !ok {
		return
	}

	root.HandleFunc("POST /webhooks/github", func(w http.ResponseWriter, r *http.Request) {
		body, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 2<<20))
		if e != nil {
			writeError(w, domain.Validation("webhook exceeds 2 MiB"))
			return
		}

		e = s.ReceiveGitHubWebhook(r.Context(), r.Header.Get("X-GitHub-Delivery"), r.Header.Get("X-GitHub-Event"), r.Header.Get("X-Hub-Signature-256"), body)
		writeResult(w, 202, map[string]bool{"accepted": e == nil}, e)
	})
	v1.HandleFunc("GET /v1/github/status", func(w http.ResponseWriter, r *http.Request) {
		out, e := s.GitHubStatus(r.Context())
		writeResult(w, 200, out, e)
	})
	v1.HandleFunc("GET /v1/github/installations", func(w http.ResponseWriter, r *http.Request) {
		page, ok := repositoryPage(w, r)
		if !ok {
			return
		}

		out, e := s.GitHubInstallations(r.Context(), page)
		writeResult(w, 200, map[string]any{"items": out, "page": page, "has_more": len(out) == 30}, e)
	})
	v1.HandleFunc("GET /v1/github/installations/{installation}/repositories", func(w http.ResponseWriter, r *http.Request) {
		page, ok := repositoryPage(w, r)
		if !ok {
			return
		}

		id, e := strconv.ParseInt(r.PathValue("installation"), 10, 64)
		if e != nil || id < 1 {
			writeError(w, domain.Validation("positive installation ID required"))
			return
		}

		out, e := s.GitHubRepositories(r.Context(), id, page)
		writeResult(w, 200, map[string]any{"items": out, "page": page, "has_more": len(out) == 30}, e)
	})
	v1.HandleFunc("GET /v1/github/preview-policies", func(w http.ResponseWriter, r *http.Request) {
		out, e := s.PreviewPolicies(r.Context())
		writeResult(w, 200, map[string]any{"items": out}, e)
	})
	v1.HandleFunc("PUT /v1/projects/{project}/repositories/{repository}/preview-policy", func(w http.ResponseWriter, r *http.Request) {
		var in domain.PRPreviewPolicy
		if !decodeJSON(w, r, &in, catalogJSONError, catalogJSONExtra) {
			return
		}

		in.Project = r.PathValue("project")
		in.Repository = r.PathValue("repository")
		out, e := s.SavePreviewPolicy(r.Context(), in)
		writeResult(w, 200, out, e)
	})
	v1.HandleFunc("GET /v1/projects/{project}/repositories/{repository}/preview-policy", func(w http.ResponseWriter, r *http.Request) {
		policies, e := s.PreviewPolicies(r.Context())
		if e != nil {
			writeError(w, e)
			return
		}

		for _, policy := range policies {
			if policy.Project == r.PathValue("project") && policy.Repository == r.PathValue("repository") {
				writeResult(w, 200, policy, nil)
				return
			}
		}

		writeError(w, domain.NotFound("preview policy not found"))
	})
	v1.HandleFunc("GET /v1/github/previews", func(w http.ResponseWriter, r *http.Request) {
		after, limit, e := pagination(r)
		if e != nil {
			writeError(w, e)
			return
		}

		out, next, e := s.PRPreviews(r.Context(), r.URL.Query().Get("project"), after, limit)
		writeResult(w, 200, map[string]any{"items": out, "next_cursor": next}, e)
	})
	v1.HandleFunc("GET /v1/github/previews/{id}", func(w http.ResponseWriter, r *http.Request) {
		out, e := s.PRPreview(r.Context(), r.PathValue("id"))
		writeResult(w, 200, out, e)
	})
	for _, action := range []string{"stop", "restart"} {
		v1.HandleFunc("POST /v1/github/previews/{id}/"+action, func(w http.ResponseWriter, r *http.Request) {
			out, e := s.ControlPRPreview(r.Context(), r.PathValue("id"), action)
			writeResult(w, 200, out, e)
		})
	}
}
