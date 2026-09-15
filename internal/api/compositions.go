package api

import (
	"net/http"
	"strings"

	"github.com/dblooman/envy/internal/domain"
)

func (h *handler) create(w http.ResponseWriter, r *http.Request) {
	var request domain.CreateRequest
	if !decodeJSON(w, r, &request, "body must be a JSON composition request of at most 64 KiB with no unknown fields", "body must contain exactly one JSON value") {
		return
	}

	keys := r.Header.Values("Idempotency-Key")
	if len(keys) > 1 {
		writeError(w, domain.Validation("supply at most one Idempotency-Key"))
		return
	}

	key := r.Header.Get("Idempotency-Key")
	if len(key) > 128 || strings.IndexFunc(key, func(c rune) bool { return c < 32 || c > 126 }) >= 0 {
		writeError(w, domain.Validation("Idempotency-Key must contain at most 128 printable ASCII characters"))
		return
	}

	composition, err := h.service.Create(r.Context(), request, key)
	if err != nil {
		writeResult(w, http.StatusAccepted, composition, err)
		return
	}

	w.Header().Set("Location", "/v1/compositions/"+composition.ID)
	writeResult(w, http.StatusAccepted, composition, nil)
}

func (h *handler) get(w http.ResponseWriter, r *http.Request) {
	composition, err := h.service.Get(r.Context(), r.PathValue("id"))
	writeResult(w, http.StatusOK, composition, err)
}

func (h *handler) status(w http.ResponseWriter, r *http.Request) {
	composition, err := h.service.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeResult(w, http.StatusOK, nil, err)
		return
	}

	if composition.VerificationLevel == "" {
		composition.VerificationLevel = "none"
	}

	response := map[string]any{"verification_level": composition.VerificationLevel, "id": composition.ID, "phase": composition.Phase, "generation": composition.Generation, "observed_generation": composition.ObservedGeneration, "conditions": composition.Conditions, "latest_operation": composition.LatestOperation}
	if composition.LastError != nil {
		response["last_error"] = composition.LastError
	}

	writeResult(w, http.StatusOK, response, nil)
}

func (h *handler) endpoints(w http.ResponseWriter, r *http.Request) {
	composition, err := h.service.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeResult(w, http.StatusOK, nil, err)
		return
	}

	writeResult(w, http.StatusOK, map[string]any{"id": composition.ID, "endpoints": composition.Endpoints}, nil)
}

func (h *handler) destroy(w http.ResponseWriter, r *http.Request) {
	composition, err := h.service.Destroy(r.Context(), r.PathValue("id"))
	if err != nil {
		writeResult(w, http.StatusAccepted, composition, err)
		return
	}

	w.Header().Set("Location", "/v1/compositions/"+composition.ID)
	writeResult(w, http.StatusAccepted, composition, nil)
}

func (h *handler) list(w http.ResponseWriter, r *http.Request) {
	after, limit, err := pagination(r)
	if err != nil {
		writeError(w, err)
		return
	}

	items, next, err := h.service.List(r.Context(), r.URL.Query().Get("project"), after, limit)
	if err != nil {
		writeResult(w, http.StatusOK, nil, err)
		return
	}

	writePage(w, items, next)
}

func (h *handler) projects(w http.ResponseWriter, r *http.Request) {
	after, limit, err := pagination(r)
	if err != nil {
		writeError(w, err)
		return
	}

	items, next, err := h.service.Projects(r.Context(), after, limit)
	if err != nil {
		writeResult(w, http.StatusOK, nil, err)
		return
	}

	writePage(w, items, next)
}

func (h *handler) components(w http.ResponseWriter, r *http.Request) {
	after, limit, err := pagination(r)
	if err != nil {
		writeError(w, err)
		return
	}

	items, next, err := h.service.Components(r.Context(), r.PathValue("project"), after, limit)
	if err != nil {
		writeResult(w, http.StatusOK, nil, err)
		return
	}

	writePage(w, items, next)
}

func (h *handler) component(w http.ResponseWriter, r *http.Request) {
	component, err := h.service.Component(r.Context(), r.PathValue("project"), r.PathValue("component"))
	writeResult(w, http.StatusOK, component, err)
}

func (h *handler) baselines(w http.ResponseWriter, r *http.Request) {
	after, limit, err := pagination(r)
	if err != nil {
		writeError(w, err)
		return
	}

	items, next, err := h.service.Baselines(r.Context(), r.PathValue("project"), after, limit)
	if err != nil {
		writeResult(w, http.StatusOK, nil, err)
		return
	}

	writePage(w, items, next)
}

func (h *handler) update(w http.ResponseWriter, r *http.Request) {
	var request domain.UpdateRequest
	if !decodeJSON(w, r, &request, "body must be a JSON update request of at most 64 KiB with no unknown fields", "body must contain exactly one JSON value") {
		return
	}

	keys := r.Header.Values("Idempotency-Key")
	if len(keys) > 1 {
		writeError(w, domain.Validation("supply at most one Idempotency-Key"))
		return
	}

	request.IdempotencyKey = r.Header.Get("Idempotency-Key")
	if len(request.IdempotencyKey) > 128 || strings.IndexFunc(request.IdempotencyKey, func(c rune) bool { return c < 32 || c > 126 }) >= 0 {
		writeError(w, domain.Validation("Idempotency-Key must contain at most 128 printable ASCII characters"))
		return
	}

	composition, err := h.service.Update(r.Context(), r.PathValue("id"), request)
	if err != nil {
		writeResult(w, http.StatusAccepted, composition, err)
		return
	}

	w.Header().Set("Location", "/v1/compositions/"+composition.ID)
	writeResult(w, http.StatusAccepted, composition, nil)
}
