package api

import (
	"context"
	"net/http"
	"strconv"
	"time"

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
	page, err := h.service.Activity(r.Context(), domain.ActivityFilter{After: q.Get("after"), Project: q.Get("project"), Actor: q.Get("actor"), Action: q.Get("action"), Outcome: q.Get("outcome"), ResourceType: q.Get("resource_type"), ResourceID: q.Get("resource_id"), From: from, To: to, Limit: limit})
	writeResult(w, http.StatusOK, page, err)
}

func (h *handler) revisions(w http.ResponseWriter, r *http.Request) {
	after, limit, err := pagination(r)
	if err != nil {
		writeError(w, err)
		return
	}

	page, err := h.service.Revisions(r.Context(), r.PathValue("id"), after, limit)
	writeResult(w, http.StatusOK, page, err)
}

func (h *handler) revision(w http.ResponseWriter, r *http.Request) {
	g, err := strconv.ParseInt(r.PathValue("generation"), 10, 64)
	if err != nil || g < 1 {
		writeError(w, domain.Validation("generation must be positive"))
		return
	}

	item, err := h.service.Revision(r.Context(), r.PathValue("id"), g)
	writeResult(w, http.StatusOK, item, err)
}
