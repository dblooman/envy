package api

import (
	"context"
	"net/http"
	"strconv"

	"github.com/dblooman/envy/internal/domain"
)

type draftService interface {
	GetOnboardingDraft(context.Context, string) (domain.OnboardingDraft, error)
	SaveOnboardingDraft(context.Context, domain.OnboardingDraft) (domain.OnboardingDraft, error)
	DeleteOnboardingDraft(context.Context, string, int64) error
}

func (h *handler) drafts() (draftService, error) {
	s, ok := h.service.(draftService)
	if !ok {
		return nil, &domain.Error{Code: "unavailable", Message: "onboarding drafts are unavailable"}
	}

	return s, nil
}

func (h *handler) getOnboardingDraft(w http.ResponseWriter, r *http.Request) {
	s, err := h.drafts()
	if err != nil {
		writeError(w, err)
		return
	}

	draft, err := s.GetOnboardingDraft(r.Context(), r.PathValue("project"))
	writeResult(w, http.StatusOK, draft, err)
}

func (h *handler) saveOnboardingDraft(w http.ResponseWriter, r *http.Request) {
	var draft domain.OnboardingDraft
	if !decodeJSON(w, r, &draft, "body must be a non-secret JSON onboarding draft of at most 64 KiB with no unknown fields", "body must contain exactly one JSON value") {
		return
	}

	if draft.Project != r.PathValue("project") {
		writeError(w, domain.Validation("draft project must match the URL"))
		return
	}

	s, err := h.drafts()
	if err != nil {
		writeError(w, err)
		return
	}

	saved, err := s.SaveOnboardingDraft(r.Context(), draft)
	writeResult(w, http.StatusOK, saved, err)
}

func (h *handler) deleteOnboardingDraft(w http.ResponseWriter, r *http.Request) {
	values := r.URL.Query()["revision"]
	if len(values) != 1 {
		writeError(w, domain.Validation("supply one draft revision"))
		return
	}

	revision, err := strconv.ParseInt(values[0], 10, 64)
	if err != nil || revision < 1 {
		writeError(w, domain.Validation("draft revision must be positive"))
		return
	}

	s, err := h.drafts()
	if err != nil {
		writeError(w, err)
		return
	}

	if err := s.DeleteOnboardingDraft(r.Context(), r.PathValue("project"), revision); err != nil {
		writeError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
