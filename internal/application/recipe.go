package application

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"

	"github.com/dblooman/envy/internal/domain"
)

type ExportRecipeRequest struct {
	Composition string                   `json:"composition"`
	Frontends   []domain.RecipeSelection `json:"frontends,omitempty"`
}

type RecreateRecipeRequest struct {
	Recipe         domain.Recipe `json:"recipe"`
	Name           string        `json:"name"`
	IdempotencyKey string        `json:"idempotency_key"`
}

type RecreateRecipeResult struct {
	Composition   domain.Composition           `json:"composition"`
	Bindings      []domain.FrontendBindingView `json:"bindings"`
	BindingErrors []string                     `json:"binding_errors"`
}

func (s *Service) ExportRecipe(ctx context.Context, req ExportRecipeRequest) (domain.Recipe, error) {
	var out domain.Recipe
	if req.Composition == "" || len(req.Frontends) > 20 {
		return out, domain.Validation("composition and at most twenty frontends are required")
	}

	c, err := s.Get(ctx, req.Composition)
	if err != nil {
		return out, err
	}

	out = domain.Recipe{MessageIsolation: c.MessageIsolation, APIVersion: domain.RecipeVersion, Project: c.Project, Baseline: c.Baseline, BaselineRevision: c.BaselineRevision, TTL: c.ExpiresAt.Sub(c.CreatedAt).String(), Overrides: map[string]domain.ComponentOverride{}, Frontends: []domain.RecipeFrontend{}}
	for component, override := range c.Overrides {
		if override.BuildID != "" {
			out.Overrides[component] = domain.ComponentOverride{BuildID: override.BuildID}
		} else {
			out.Overrides[component] = domain.ComponentOverride{Image: override.Image}
		}
	}

	for _, selection := range req.Frontends {
		view, e := s.FrontendBinding(ctx, domain.FrontendKey{Project: c.Project, Frontend: selection.Name, Revision: selection.Revision})
		if e != nil {
			return out, e
		}

		if view.Binding.Composition != c.ID {
			return out, domain.Validation("selected frontend is bound to a different composition")
		}

		out.Frontends = append(out.Frontends, domain.RecipeFrontend{Name: selection.Name, Revision: selection.Revision, Repository: view.Binding.Repository})
	}

	return out, domain.ValidateRecipe(out)
}

func (s *Service) ValidateRecipe(_ context.Context, recipe domain.Recipe) (domain.Recipe, error) {
	if err := domain.ValidateRecipe(recipe); err != nil {
		return recipe, err
	}

	return recipe, nil
}

func (s *Service) RecreateRecipe(ctx context.Context, req RecreateRecipeRequest) (RecreateRecipeResult, error) {
	out := RecreateRecipeResult{Bindings: []domain.FrontendBindingView{}, BindingErrors: []string{}}
	if err := domain.ValidateRecipe(req.Recipe); err != nil {
		return out, err
	}

	if strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.IdempotencyKey) == "" {
		return out, domain.Validation("recreate requires a new name and stable idempotency key")
	}

	c, err := s.Create(ctx, domain.CreateRequest{MessageIsolation: req.Recipe.MessageIsolation, Project: req.Recipe.Project, Baseline: req.Recipe.Baseline, ExpectedBaselineRevision: req.Recipe.BaselineRevision, Name: req.Name, Overrides: req.Recipe.Overrides, TTL: req.Recipe.TTL}, req.IdempotencyKey)
	if err != nil {
		return out, err
	}

	out.Composition = c
	for _, frontend := range req.Recipe.Frontends {
		hash := sha256.Sum256([]byte(c.ID + "/" + frontend.Name + "/" + frontend.Repository))
		name := fmt.Sprintf("recipe-%x", hash[:20])
		view, e := s.BindFrontend(ctx, domain.FrontendKey{Project: req.Recipe.Project, Frontend: name, Revision: frontend.Revision}, domain.BindFrontendRequest{Composition: c.ID, Repository: frontend.Repository})
		if e != nil {
			out.BindingErrors = append(out.BindingErrors, fmt.Sprintf("%s@%s: %v", frontend.Name, frontend.Revision, e))
			continue
		}

		out.Bindings = append(out.Bindings, view)
	}

	return out, nil
}
