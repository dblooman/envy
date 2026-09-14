package api

import (
	"context"
	"github.com/dblooman/envy/internal/domain"
	"net/http"
	"strings"
)

type previewService interface {
	DiscoverPreview(context.Context, string, string, string, domain.PreviewSelection) (domain.PreviewReport, error)
	ApprovePreview(context.Context, string, string, string, domain.PreviewApproval) (domain.PreviewProfile, error)
	InspectPreview(context.Context, string, string, string) (domain.PreviewProfile, error)
}

func (h *handler) preview(w http.ResponseWriter, r *http.Request) {
	service, ok := h.service.(previewService)
	if !ok {
		writeError(w, &domain.Error{Code: "unavailable", Message: "preview onboarding unavailable"})
		return
	}
	project, baseline, component := r.PathValue("project"), r.PathValue("baseline"), r.PathValue("component")
	for _, id := range []string{project, baseline, component} {
		if !domain.ValidCatalogID(id) {
			writeError(w, domain.Validation("invalid preview profile scope"))
			return
		}
	}
	var out any
	var err error
	if r.Method == http.MethodGet {
		out, err = service.InspectPreview(r.Context(), project, baseline, component)
	} else if strings.HasSuffix(r.URL.Path, "/discover") {
		var input domain.PreviewSelection
		if !decodeCatalog(w, r, &input) {
			return
		}
		out, err = service.DiscoverPreview(r.Context(), project, baseline, component, input)
	} else {
		var input domain.PreviewApproval
		if !decodeCatalog(w, r, &input) {
			return
		}
		out, err = service.ApprovePreview(r.Context(), project, baseline, component, input)
	}
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
