package api

import (
	"context"
	"encoding/json"
	"github.com/dblooman/envy/internal/domain"
	"io"
	"net/http"
)

type catalogService interface {
	RegisterProject(context.Context, domain.Project) (domain.Project, error)
	RegisterComponent(context.Context, domain.Component) (domain.Component, error)
	RegisterBaseline(context.Context, domain.Baseline) (domain.Baseline, error)
}

func decodeCatalog(w http.ResponseWriter, r *http.Request, target any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(w, domain.Validation("catalog request must be JSON of at most 64 KiB with no unknown fields"))
		return false
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		writeError(w, domain.Validation("body must contain one JSON value"))
		return false
	}
	return true
}
func (h *handler) register(w http.ResponseWriter, r *http.Request) {
	service, ok := h.service.(catalogService)
	if !ok {
		writeError(w, &domain.Error{Code: "unavailable", Message: "catalog registration is unavailable"})
		return
	}
	var result any
	var err error
	var location string
	switch r.Pattern {
	case "POST /v1/projects":
		var p domain.Project
		if !decodeCatalog(w, r, &p) {
			return
		}
		result, err = service.RegisterProject(r.Context(), p)
		location = "/v1/projects"
	case "POST /v1/projects/{project}/components":
		var c domain.Component
		if !decodeCatalog(w, r, &c) {
			return
		}
		if c.Project != "" && c.Project != r.PathValue("project") {
			writeError(w, domain.Validation("project must match the URL"))
			return
		}
		c.Project = r.PathValue("project")
		result, err = service.RegisterComponent(r.Context(), c)
		location = "/v1/projects/" + c.Project + "/components/" + c.ID
	case "POST /v1/projects/{project}/baselines":
		var b domain.Baseline
		if !decodeCatalog(w, r, &b) {
			return
		}
		if b.Project != "" && b.Project != r.PathValue("project") {
			writeError(w, domain.Validation("project must match the URL"))
			return
		}
		b.Project = r.PathValue("project")
		result, err = service.RegisterBaseline(r.Context(), b)
		location = "/v1/projects/" + b.Project + "/baselines"
	}
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Location", location)
	writeJSON(w, http.StatusCreated, result)
}

func (h *handler) onboard(w http.ResponseWriter, r *http.Request) {
	service, ok := h.service.(interface {
		Onboard(context.Context, domain.CatalogManifest, bool) (domain.CatalogReport, error)
	})
	if !ok {
		writeError(w, &domain.Error{Code: "unavailable", Message: "catalog onboarding is unavailable"})
		return
	}
	var m domain.CatalogManifest
	if !decodeCatalog(w, r, &m) {
		return
	}
	out, err := service.Onboard(r.Context(), m, r.URL.Path == "/v1/catalog/apply")
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
