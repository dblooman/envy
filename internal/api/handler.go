// Package api exposes the authoritative HTTP API. It never calls providers.
package api

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/dblooman/envy/internal/domain"
)

// Service is the application boundary shared by HTTP handlers and their tests.
type Service interface {
	Logs(context.Context, string, string, domain.LogOptions) (domain.ComponentLogs, error)
	Events(context.Context, string, string, int) (domain.EventsPage, error)
	Create(context.Context, domain.CreateRequest, string) (domain.Composition, error)
	Get(context.Context, string) (domain.Composition, error)
	Destroy(context.Context, string) (domain.Composition, error)
	Update(context.Context, string, domain.UpdateRequest) (domain.Composition, error)
	List(context.Context, string, string, int) ([]domain.Composition, string, error)
	Projects(context.Context, string, int) ([]domain.Project, string, error)
	Components(context.Context, string, string, int) ([]domain.Component, string, error)
	Component(context.Context, string, string) (domain.Component, error)
	Baselines(context.Context, string, string, int) ([]domain.Baseline, string, error)
}

type handler struct {
	service   Service
	tokenHash [32]byte
	tokenSet  bool
	ready     func(context.Context) error
}

// NewHandler installs authenticated v1 routes and unauthenticated health probes.
// Empty credentials fail closed. ready checks dependencies, not reconciliation leadership.
func NewHandler(service Service, token string, ready func(context.Context) error) http.Handler {
	h := &handler{service: service, tokenHash: sha256.Sum256([]byte(token)), tokenSet: token != "", ready: ready}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if h.ready != nil {
			if err := h.ready(r.Context()); err != nil {
				writeError(w, &domain.Error{Code: "unavailable", Message: "service is not ready", Retryable: true})
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})

	v1 := http.NewServeMux()
	v1.HandleFunc("POST /v1/catalog/validate", h.onboard)
	v1.HandleFunc("POST /v1/catalog/apply", h.onboard)
	v1.HandleFunc("POST /v1/projects", h.register)
	v1.HandleFunc("POST /v1/projects/{project}/components", h.register)
	v1.HandleFunc("POST /v1/projects/{project}/baselines", h.register)
	v1.HandleFunc("GET /v1/projects", h.projects)
	v1.HandleFunc("GET /v1/projects/{project}/components", h.components)
	v1.HandleFunc("GET /v1/projects/{project}/components/{component}", h.component)
	v1.HandleFunc("GET /v1/projects/{project}/baselines", h.baselines)
	v1.HandleFunc("POST /v1/compositions", h.create)
	v1.HandleFunc("GET /v1/compositions", h.list)
	v1.HandleFunc("GET /v1/compositions/{id}", h.get)
	v1.HandleFunc("GET /v1/compositions/{id}/status", h.status)
	v1.HandleFunc("GET /v1/compositions/{id}/endpoints", h.endpoints)
	v1.HandleFunc("DELETE /v1/compositions/{id}", h.destroy)
	v1.HandleFunc("PATCH /v1/compositions/{id}", h.update)
	v1.HandleFunc("GET /v1/compositions/{id}/components/{component}/logs", h.logs)
	v1.HandleFunc("GET /v1/compositions/{id}/events", h.events)
	v1.HandleFunc("/v1/", func(w http.ResponseWriter, r *http.Request) { writeError(w, domain.NotFound("API route not found")) })
	mux.Handle("/v1/", h.authenticate(v1))
	return mux
}

func (h *handler) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		values := r.Header.Values("Authorization")
		var supplied string
		if len(values) == 1 && strings.HasPrefix(values[0], "Bearer ") {
			supplied = strings.TrimPrefix(values[0], "Bearer ")
		}
		suppliedHash := sha256.Sum256([]byte(supplied))
		if subtle.ConstantTimeCompare(h.tokenHash[:], suppliedHash[:]) != 1 || !h.tokenSet || supplied == "" {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeError(w, &domain.Error{Code: "unauthorized", Message: "valid bearer credentials are required"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (h *handler) create(w http.ResponseWriter, r *http.Request) {
	var request domain.CreateRequest
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeError(w, &domain.Error{Code: "validation_error", Message: "body must be a JSON composition request of at most 64 KiB with no unknown fields"})
		return
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		writeError(w, &domain.Error{Code: "validation_error", Message: "body must contain exactly one JSON value"})
		return
	}
	keys := r.Header.Values("Idempotency-Key")
	if len(keys) > 1 {
		writeError(w, &domain.Error{Code: "validation_error", Message: "supply at most one Idempotency-Key"})
		return
	}
	key := r.Header.Get("Idempotency-Key")
	if len(key) > 128 || strings.IndexFunc(key, func(c rune) bool { return c < 32 || c > 126 }) >= 0 {
		writeError(w, &domain.Error{Code: "validation_error", Message: "Idempotency-Key must contain at most 128 printable ASCII characters"})
		return
	}
	c, err := h.service.Create(r.Context(), request, key)
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Location", "/v1/compositions/"+c.ID)
	writeJSON(w, http.StatusAccepted, c)
}

func (h *handler) get(w http.ResponseWriter, r *http.Request) {
	c, err := h.service.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (h *handler) status(w http.ResponseWriter, r *http.Request) {
	c, err := h.service.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	if c.VerificationLevel == "" {
		c.VerificationLevel = "none"
	}
	response := map[string]any{"verification_level": c.VerificationLevel, "id": c.ID, "phase": c.Phase, "generation": c.Generation, "observed_generation": c.ObservedGeneration, "conditions": c.Conditions, "latest_operation": c.LatestOperation}
	if c.LastError != nil {
		response["last_error"] = c.LastError
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *handler) endpoints(w http.ResponseWriter, r *http.Request) {
	c, err := h.service.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": c.ID, "endpoints": c.Endpoints})
}

func (h *handler) destroy(w http.ResponseWriter, r *http.Request) {
	c, err := h.service.Destroy(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Location", "/v1/compositions/"+c.ID)
	writeJSON(w, http.StatusAccepted, c)
}

func (h *handler) list(w http.ResponseWriter, r *http.Request) {
	after, limit, err := pagination(r)
	if err != nil {
		writeError(w, err)
		return
	}
	items, next, err := h.service.List(r.Context(), r.URL.Query().Get("project"), after, limit)
	if err != nil {
		writeError(w, err)
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
		writeError(w, err)
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
		writeError(w, err)
		return
	}
	writePage(w, items, next)
}

func (h *handler) component(w http.ResponseWriter, r *http.Request) {
	c, err := h.service.Component(r.Context(), r.PathValue("project"), r.PathValue("component"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (h *handler) baselines(w http.ResponseWriter, r *http.Request) {
	after, limit, err := pagination(r)
	if err != nil {
		writeError(w, err)
		return
	}
	items, next, err := h.service.Baselines(r.Context(), r.PathValue("project"), after, limit)
	if err != nil {
		writeError(w, err)
		return
	}
	writePage(w, items, next)
}

func pagination(r *http.Request) (string, int, error) {
	limit := 20
	if raw, ok := r.URL.Query()["limit"]; ok {
		if len(raw) != 1 {
			return "", 0, &domain.Error{Code: "validation_error", Message: "supply limit once"}
		}
		n, err := strconv.Atoi(raw[0])
		if err != nil || n < 1 || n > 100 {
			return "", 0, &domain.Error{Code: "validation_error", Message: "limit must be between 1 and 100"}
		}
		limit = n
	}
	return r.URL.Query().Get("after"), limit, nil
}

func writePage[T any](w http.ResponseWriter, items []T, next string) {
	if items == nil {
		items = []T{}
	}
	writeJSON(w, http.StatusOK, struct {
		Items      []T    `json:"items"`
		NextCursor string `json:"next_cursor,omitempty"`
	}{items, next})
}

func writeError(w http.ResponseWriter, err error) {
	var appErr *domain.Error
	if !errors.As(err, &appErr) {
		slog.Error("API application failure", "error", err)
		appErr = &domain.Error{Code: "unavailable", Message: "service temporarily unavailable", Retryable: true}
	}
	status := http.StatusInternalServerError
	switch appErr.Code {
	case "validation_error":
		status = http.StatusBadRequest
	case "unauthorized":
		status = http.StatusUnauthorized
	case "not_found":
		status = http.StatusNotFound
	case "conflict":
		status = http.StatusConflict
	case "capacity_exceeded":
		status = http.StatusTooManyRequests
	case "unavailable":
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, map[string]any{"error": appErr})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		slog.Debug("write API response", "error", err)
	}
}

func (h *handler) update(w http.ResponseWriter, r *http.Request) {
	var req domain.UpdateRequest
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeError(w, domain.Validation("body must be a JSON update request of at most 64 KiB with no unknown fields"))
		return
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		writeError(w, domain.Validation("body must contain exactly one JSON value"))
		return
	}
	c, err := h.service.Update(r.Context(), r.PathValue("id"), req)
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Location", "/v1/compositions/"+c.ID)
	writeJSON(w, http.StatusAccepted, c)
}
