package api

import (
	"context"
	"net/http"

	"github.com/dblooman/envy/internal/application"
	"github.com/dblooman/envy/internal/domain"
)

type recipeService interface {
	ExportRecipe(context.Context, application.ExportRecipeRequest) (domain.Recipe, error)
	ValidateRecipe(context.Context, domain.Recipe) (domain.Recipe, error)
	RecreateRecipe(context.Context, application.RecreateRecipeRequest) (application.RecreateRecipeResult, error)
}

func (h *handler) exportRecipe(w http.ResponseWriter, r *http.Request) {
	var request application.ExportRecipeRequest
	if !decodeJSON(w, r, &request, catalogJSONError, catalogJSONExtra) {
		return
	}
	recipe, err := h.service.ExportRecipe(r.Context(), request)
	writeResult(w, http.StatusOK, recipe, err)
}

func (h *handler) validateRecipe(w http.ResponseWriter, r *http.Request) {
	var recipe domain.Recipe
	if !decodeJSON(w, r, &recipe, catalogJSONError, catalogJSONExtra) {
		return
	}
	validated, err := h.service.ValidateRecipe(r.Context(), recipe)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"valid": true, "recipe": validated})
}

func (h *handler) recreateRecipe(w http.ResponseWriter, r *http.Request) {
	var request application.RecreateRecipeRequest
	if !decodeJSON(w, r, &request, catalogJSONError, catalogJSONExtra) {
		return
	}
	result, err := h.service.RecreateRecipe(r.Context(), request)
	writeResult(w, http.StatusAccepted, result, err)
}
