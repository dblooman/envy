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

func frontendKey(r *http.Request) domain.FrontendKey {
	return domain.FrontendKey{Project: r.PathValue("project"), Frontend: r.PathValue("frontend"), Revision: r.PathValue("revision")}
}

func (h *handler) bindFrontend(w http.ResponseWriter, r *http.Request) {
	var request domain.BindFrontendRequest
	if !decodeJSON(w, r, &request, catalogJSONError, catalogJSONExtra) {
		return
	}
	view, err := h.service.BindFrontend(r.Context(), frontendKey(r), request)
	writeResult(w, http.StatusOK, view, err)
}

func (h *handler) frontendBinding(w http.ResponseWriter, r *http.Request) {
	view, err := h.service.FrontendBinding(r.Context(), frontendKey(r))
	writeResult(w, http.StatusOK, view, err)
}

func (h *handler) resolveFrontend(w http.ResponseWriter, r *http.Request) {
	resolution, err := h.service.ResolveFrontend(r.Context(), frontendKey(r))
	writeResult(w, http.StatusOK, resolution, err)
}

func (h *handler) publishFrontend(w http.ResponseWriter, r *http.Request) {
	var request domain.PublishFrontendRequest
	if !decodeJSON(w, r, &request, catalogJSONError, catalogJSONExtra) {
		return
	}
	view, err := h.service.PublishFrontend(r.Context(), frontendKey(r), request)
	writeResult(w, http.StatusOK, view, err)
}

func (h *handler) checkFrontend(w http.ResponseWriter, r *http.Request) {
	var request domain.FrontendCheckRequest
	if !decodeJSON(w, r, &request, catalogJSONError, catalogJSONExtra) {
		return
	}
	view, err := h.service.CheckFrontend(r.Context(), frontendKey(r), request)
	writeResult(w, http.StatusOK, view, err)
}

func (h *handler) listFrontendBindings(w http.ResponseWriter, r *http.Request) {
	after, limit, err := pagination(r)
	if err != nil {
		writeError(w, err)
		return
	}
	items, next, err := h.service.FrontendBindings(r.Context(), r.PathValue("id"), after, limit)
	writeResult(w, http.StatusOK, map[string]any{"items": items, "next_cursor": next}, err)
}
