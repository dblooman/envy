package api

import (
	"context"
	"net/http"

	"github.com/dblooman/envy/internal/domain"
)

type previewService interface {
	DiscoverPreview(context.Context, string, string, string, domain.PreviewSelection) (domain.PreviewReport, error)
	ApprovePreview(context.Context, string, string, string, domain.PreviewApproval) (domain.PreviewProfile, error)
	InspectPreview(context.Context, string, string, string) (domain.PreviewProfile, error)
}

func previewScope(w http.ResponseWriter, r *http.Request) (string, string, string, bool) {
	project, baseline, component := r.PathValue("project"), r.PathValue("baseline"), r.PathValue("component")
	for _, id := range []string{project, baseline, component} {
		if !domain.ValidCatalogID(id) {
			writeError(w, domain.Validation("invalid preview profile scope"))
			return "", "", "", false
		}
	}
	return project, baseline, component, true
}

func (h *handler) inspectPreview(w http.ResponseWriter, r *http.Request) {
	project, baseline, component, ok := previewScope(w, r)
	if !ok {
		return
	}
	profile, err := h.service.InspectPreview(r.Context(), project, baseline, component)
	writeResult(w, http.StatusOK, profile, err)
}

func (h *handler) discoverPreview(w http.ResponseWriter, r *http.Request) {
	project, baseline, component, ok := previewScope(w, r)
	if !ok {
		return
	}
	var selection domain.PreviewSelection
	if !decodeJSON(w, r, &selection, catalogJSONError, catalogJSONExtra) {
		return
	}
	report, err := h.service.DiscoverPreview(r.Context(), project, baseline, component, selection)
	writeResult(w, http.StatusOK, report, err)
}

func (h *handler) approvePreview(w http.ResponseWriter, r *http.Request) {
	project, baseline, component, ok := previewScope(w, r)
	if !ok {
		return
	}
	var approval domain.PreviewApproval
	if !decodeJSON(w, r, &approval, catalogJSONError, catalogJSONExtra) {
		return
	}
	profile, err := h.service.ApprovePreview(r.Context(), project, baseline, component, approval)
	writeResult(w, http.StatusOK, profile, err)
}
