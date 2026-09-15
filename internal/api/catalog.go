package api

import (
	"context"
	"net/http"

	"github.com/dblooman/envy/internal/domain"
)

type catalogService interface {
	RegisterProject(context.Context, domain.Project) (domain.Project, error)
	RegisterComponent(context.Context, domain.Component) (domain.Component, error)
	RegisterBaseline(context.Context, domain.Baseline) (domain.Baseline, error)
}

type onboardingService interface {
	Onboard(context.Context, domain.CatalogManifest, bool) (domain.CatalogReport, error)
}

func (h *handler) registerProject(w http.ResponseWriter, r *http.Request) {
	var project domain.Project
	if !decodeJSON(w, r, &project, catalogJSONError, catalogJSONExtra) {
		return
	}

	registered, err := h.service.RegisterProject(r.Context(), project)
	w.Header().Set("Location", "/v1/projects")
	writeResult(w, http.StatusCreated, registered, err)
}

func (h *handler) registerComponent(w http.ResponseWriter, r *http.Request) {
	var component domain.Component
	if !decodeJSON(w, r, &component, catalogJSONError, catalogJSONExtra) {
		return
	}

	if project := r.PathValue("project"); component.Project != "" && component.Project != project {
		writeError(w, domain.Validation("project must match the URL"))
		return
	} else {
		component.Project = project
	}

	registered, err := h.service.RegisterComponent(r.Context(), component)
	w.Header().Set("Location", "/v1/projects/"+component.Project+"/components/"+component.ID)
	writeResult(w, http.StatusCreated, registered, err)
}

func (h *handler) registerBaseline(w http.ResponseWriter, r *http.Request) {
	var baseline domain.Baseline
	if !decodeJSON(w, r, &baseline, catalogJSONError, catalogJSONExtra) {
		return
	}

	if project := r.PathValue("project"); baseline.Project != "" && baseline.Project != project {
		writeError(w, domain.Validation("project must match the URL"))
		return
	} else {
		baseline.Project = project
	}

	registered, err := h.service.RegisterBaseline(r.Context(), baseline)
	w.Header().Set("Location", "/v1/projects/"+baseline.Project+"/baselines")
	writeResult(w, http.StatusCreated, registered, err)
}

func (h *handler) validateCatalog(w http.ResponseWriter, r *http.Request) {
	h.onboardCatalog(w, r, false)
}

func (h *handler) applyCatalog(w http.ResponseWriter, r *http.Request) {
	h.onboardCatalog(w, r, true)
}

func (h *handler) onboardCatalog(w http.ResponseWriter, r *http.Request, apply bool) {
	var manifest domain.CatalogManifest
	if !decodeJSON(w, r, &manifest, catalogJSONError, catalogJSONExtra) {
		return
	}

	report, err := h.service.Onboard(r.Context(), manifest, apply)
	writeResult(w, http.StatusOK, report, err)
}
