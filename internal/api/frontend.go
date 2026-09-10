package api

import (
	"context"
	"net/http"

	"github.com/dblooman/envy/internal/domain"
)

type frontendService interface {
	BindFrontend(context.Context, domain.FrontendKey, domain.BindFrontendRequest) (domain.FrontendBindingView, error)
	FrontendBinding(context.Context, domain.FrontendKey) (domain.FrontendBindingView, error)
	ResolveFrontend(context.Context, domain.FrontendKey) (domain.FrontendResolution, error)
	FrontendBindings(context.Context, string, string, int) ([]domain.FrontendBindingView, string, error)
	PublishFrontend(context.Context, domain.FrontendKey, domain.PublishFrontendRequest) (domain.FrontendBindingView, error)
	CheckFrontend(context.Context, domain.FrontendKey, domain.FrontendCheckRequest) (domain.FrontendBindingView, error)
}

func (h *handler) frontend(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	s, ok := h.service.(frontendService)
	if !ok {
		writeError(w, &domain.Error{Code: "unavailable", Message: "frontend bindings are unavailable"})
		return
	}
	k := domain.FrontendKey{Project: r.PathValue("project"), Frontend: r.PathValue("frontend"), Revision: r.PathValue("revision")}
	var result any
	var err error
	switch r.Pattern {
	case "PUT /v1/projects/{project}/frontend-bindings/{frontend}/{revision}":
		var req domain.BindFrontendRequest
		if !decodeCatalog(w, r, &req) {
			return
		}
		result, err = s.BindFrontend(r.Context(), k, req)
	case "GET /v1/projects/{project}/frontend-bindings/{frontend}/{revision}":
		result, err = s.FrontendBinding(r.Context(), k)
	case "GET /v1/projects/{project}/frontend-bindings/{frontend}/{revision}/resolve":
		result, err = s.ResolveFrontend(r.Context(), k)
	case "POST /v1/projects/{project}/frontend-bindings/{frontend}/{revision}/deployment":
		var req domain.PublishFrontendRequest
		if !decodeCatalog(w, r, &req) {
			return
		}
		result, err = s.PublishFrontend(r.Context(), k, req)
	case "POST /v1/projects/{project}/frontend-bindings/{frontend}/{revision}/check":
		var req domain.FrontendCheckRequest
		if !decodeCatalog(w, r, &req) {
			return
		}
		result, err = s.CheckFrontend(r.Context(), k, req)
	case "GET /v1/compositions/{id}/frontend-bindings":
		after, limit, pageErr := pagination(r)
		if pageErr != nil {
			writeError(w, pageErr)
			return
		}
		var items []domain.FrontendBindingView
		var next string
		items, next, err = s.FrontendBindings(r.Context(), r.PathValue("id"), after, limit)
		result = map[string]any{"items": items, "next_cursor": next}
	}
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
