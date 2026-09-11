package api

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/dblooman/envy/internal/application"
	"github.com/dblooman/envy/internal/domain"
)

type activityService interface {
	Activity(context.Context, domain.ActivityFilter) (domain.ActivityPage, error)
	Revisions(context.Context, string, string, int) (domain.RevisionsPage, error)
	Revision(context.Context, string, int64) (domain.CompositionRevision, error)
}

func (h *handler) session(w http.ResponseWriter, r *http.Request) {
	identity := domain.RequestIdentityFromContext(r.Context())
	writeJSON(w, http.StatusOK, Session{Principal: identity.Principal, AuthMode: h.auth.Mode, Channel: identity.Channel, Capabilities: []string{"catalog:write", "compositions:write", "frontends:write", "activity:read"}})
}
func (h *handler) installationInfo(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, h.installation)
}

func (h *handler) activity(w http.ResponseWriter, r *http.Request) {
	s, ok := h.service.(activityService)
	if !ok {
		writeError(w, &domain.Error{Code: "unavailable", Message: "activity history is unavailable"})
		return
	}
	_, limit, err := pagination(r)
	if err != nil {
		writeError(w, err)
		return
	}
	parseTime := func(name string) (time.Time, error) {
		value := r.URL.Query().Get(name)
		if value == "" {
			return time.Time{}, nil
		}
		t, e := time.Parse(time.RFC3339, value)
		if e != nil {
			return time.Time{}, domain.Validation(name + " must be RFC3339")
		}
		return t, nil
	}
	from, err := parseTime("from")
	if err != nil {
		writeError(w, err)
		return
	}
	to, err := parseTime("to")
	if err != nil {
		writeError(w, err)
		return
	}
	q := r.URL.Query()
	page, err := s.Activity(r.Context(), domain.ActivityFilter{After: q.Get("after"), Project: q.Get("project"), Actor: q.Get("actor"), Action: q.Get("action"), Outcome: q.Get("outcome"), ResourceType: q.Get("resource_type"), ResourceID: q.Get("resource_id"), From: from, To: to, Limit: limit})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}
func (h *handler) revisions(w http.ResponseWriter, r *http.Request) {
	s, ok := h.service.(activityService)
	if !ok {
		writeError(w, &domain.Error{Code: "unavailable", Message: "revision history is unavailable"})
		return
	}
	after, limit, err := pagination(r)
	if err != nil {
		writeError(w, err)
		return
	}
	page, err := s.Revisions(r.Context(), r.PathValue("id"), after, limit)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}
func (h *handler) revision(w http.ResponseWriter, r *http.Request) {
	s, ok := h.service.(activityService)
	if !ok {
		writeError(w, &domain.Error{Code: "unavailable", Message: "revision history is unavailable"})
		return
	}
	g, err := strconv.ParseInt(r.PathValue("generation"), 10, 64)
	if err != nil || g < 1 {
		writeError(w, domain.Validation("generation must be positive"))
		return
	}
	item, err := s.Revision(r.Context(), r.PathValue("id"), g)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

type recipeService interface {
	ExportRecipe(context.Context, application.ExportRecipeRequest) (domain.Recipe, error)
	ValidateRecipe(context.Context, domain.Recipe) (domain.Recipe, error)
	RecreateRecipe(context.Context, application.RecreateRecipeRequest) (application.RecreateRecipeResult, error)
}

func (h *handler) recipes(w http.ResponseWriter, r *http.Request) {
	s, ok := h.service.(recipeService)
	if !ok {
		writeError(w, &domain.Error{Code: "unavailable", Message: "recipe workflows are unavailable"})
		return
	}
	switch r.Pattern {
	case "POST /v1/recipes/export":
		var req application.ExportRecipeRequest
		if !decodeCatalog(w, r, &req) {
			return
		}
		out, err := s.ExportRecipe(r.Context(), req)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
	case "POST /v1/recipes/validate":
		var recipe domain.Recipe
		if !decodeCatalog(w, r, &recipe) {
			return
		}
		out, err := s.ValidateRecipe(r.Context(), recipe)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"valid": true, "recipe": out})
	case "POST /v1/recipes/recreate":
		var req application.RecreateRecipeRequest
		if !decodeCatalog(w, r, &req) {
			return
		}
		out, err := s.RecreateRecipe(r.Context(), req)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusAccepted, out)
	}
}
