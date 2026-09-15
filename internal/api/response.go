package api

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/dblooman/envy/internal/domain"
)

const (
	catalogJSONError = "catalog request must be JSON of at most 64 KiB with no unknown fields"
	catalogJSONExtra = "body must contain one JSON value"
)

// decodeJSON accepts one bounded JSON object with no unknown fields.
func decodeJSON(w http.ResponseWriter, r *http.Request, target any, invalidMessage, extraMessage string) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(w, domain.Validation(invalidMessage))
		return false
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		writeError(w, domain.Validation(extraMessage))
		return false
	}
	return true
}

func pagination(r *http.Request) (string, int, error) {
	limit := 20
	if raw, ok := r.URL.Query()["limit"]; ok {
		if len(raw) != 1 {
			return "", 0, domain.Validation("supply limit once")
		}
		n, err := strconv.Atoi(raw[0])
		if err != nil || n < 1 || n > 100 {
			return "", 0, domain.Validation("limit must be between 1 and 100")
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

// writeResult is the common terminal path for application-backed endpoints.
func writeResult(w http.ResponseWriter, status int, value any, err error) {
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, status, value)
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
	case "gone":
		status = http.StatusGone
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
