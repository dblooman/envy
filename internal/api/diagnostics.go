package api

import (
	"context"
	"net/http"
	"strconv"

	"github.com/dblooman/envy/internal/domain"
)

func (h *handler) logs(w http.ResponseWriter, r *http.Request) {
	var o domain.LogOptions
	for key, values := range r.URL.Query() {
		if len(values) != 1 {
			writeError(w, domain.Validation("supply each log option once"))
			return
		}

		value := values[0]
		if key == "container" {
			if value == "" {
				writeError(w, domain.Validation("container must be nonempty when supplied"))
				return
			}

			o.Container = value
			continue
		}

		if key == "previous" {
			if value != "true" && value != "false" {
				writeError(w, domain.Validation("previous must be true or false"))
				return
			}

			o.Previous = value == "true"
			continue
		}

		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil || n < 1 {
			writeError(w, domain.Validation("log limits must be positive integers"))
			return
		}

		switch key {
		case "tail_lines":
			o.TailLines = n
		case "max_bytes":
			o.MaxBytes = n
		case "since_seconds":
			o.SinceSeconds = n
		default:
			writeError(w, domain.Validation("unsupported log option"))
			return
		}
	}

	o, err := domain.NormalizeLogOptions(o)
	if err != nil {
		writeError(w, err)
		return
	}

	out, err := h.service.Logs(r.Context(), r.PathValue("id"), r.PathValue("component"), o)
	if err != nil {
		writeError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, out)
}

func (h *handler) events(w http.ResponseWriter, r *http.Request) {
	for key, values := range r.URL.Query() {
		if (key != "after" && key != "limit") || len(values) != 1 {
			writeError(w, domain.Validation("unsupported or repeated event option"))
			return
		}
	}

	after, limit, err := pagination(r)
	if err == nil {
		_, err = domain.EventCursor(after)
	}

	if err != nil {
		writeError(w, err)
		return
	}

	page, err := h.service.Events(r.Context(), r.PathValue("id"), after, limit)
	if err != nil {
		writeError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, page)
}

func (h *handler) verification(w http.ResponseWriter, r *http.Request) {
	for key, values := range r.URL.Query() {
		if (key != "after" && key != "limit") || len(values) != 1 {
			writeError(w, domain.Validation("unsupported or repeated event option"))
			return
		}
	}

	after, limit, err := pagination(r)
	if err == nil {
		_, err = domain.EventCursor(after)
	}

	if err != nil {
		writeError(w, err)
		return
	}

	reader, ok := h.service.(interface {
		Verification(context.Context, string, string, int) (domain.VerificationPage, error)
	})
	if !ok {
		writeError(w, &domain.Error{Code: "unavailable", Message: "verification history is unavailable"})
		return
	}

	page, err := reader.Verification(r.Context(), r.PathValue("id"), after, limit)
	if err != nil {
		writeError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, page)
}

func (h *handler) observability(w http.ResponseWriter, r *http.Request) {
	for key, values := range r.URL.Query() {
		if key != "component" || len(values) != 1 {
			writeError(w, domain.Validation("unsupported or repeated observability option"))
			return
		}
	}

	reader, ok := h.service.(interface {
		Observability(context.Context, string, string) (domain.ObservabilityLinks, error)
	})
	if !ok {
		writeError(w, &domain.Error{Code: "unavailable", Message: "observability links are unavailable"})
		return
	}

	out, err := reader.Observability(r.Context(), r.PathValue("id"), r.URL.Query().Get("component"))
	if err != nil {
		writeError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, out)
}
