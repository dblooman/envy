package client

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"

	"github.com/dblooman/envy/internal/domain"
)

type RecreateResult struct {
	Composition   domain.Composition           `json:"composition"`
	Bindings      []domain.FrontendBindingView `json:"bindings"`
	BindingErrors []string                     `json:"binding_errors"`
}

// ExportRecipe is read-only and works with tombstones. Frontends are selected
// explicitly: exporting all historical bindings would choose intent for the caller.
func (c *Client) ExportRecipe(ctx context.Context, id string, selections []domain.RecipeSelection) (domain.Recipe, error) {
	var out domain.Recipe
	if len(selections) > 20 {
		return out, domain.Validation("select at most twenty frontends")
	}
	composition, err := c.Get(ctx, id)
	if err != nil {
		return out, err
	}
	out = domain.Recipe{APIVersion: domain.RecipeVersion, Project: composition.Project, Baseline: composition.Baseline, BaselineRevision: composition.BaselineRevision, TTL: composition.ExpiresAt.Sub(composition.CreatedAt).String(), Overrides: map[string]domain.ComponentOverride{}, Frontends: []domain.RecipeFrontend{}}
	for component, o := range composition.Overrides {
		if o.BuildID != "" {
			out.Overrides[component] = domain.ComponentOverride{BuildID: o.BuildID}
		} else {
			out.Overrides[component] = domain.ComponentOverride{Image: o.Image}
		}
	}
	for _, s := range selections {
		view, err := c.FrontendBinding(ctx, domain.FrontendKey{Project: out.Project, Frontend: s.Name, Revision: s.Revision})
		if err != nil {
			return out, err
		}
		if view.Binding.Composition != id {
			return out, domain.Validation("selected frontend is bound to a different composition")
		}
		out.Frontends = append(out.Frontends, domain.RecipeFrontend{Name: s.Name, Revision: s.Revision, Repository: view.Binding.Repository})
	}
	return out, domain.ValidateRecipe(out)
}

// RecreateRecipe uses the authoritative create and bind routes. A required key
// makes binding retries recoverable without creating another composition.
func (c *Client) RecreateRecipe(ctx context.Context, recipe domain.Recipe, name, key string) (RecreateResult, error) {
	out := RecreateResult{Bindings: []domain.FrontendBindingView{}, BindingErrors: []string{}}
	if err := domain.ValidateRecipe(recipe); err != nil {
		return out, err
	}
	if strings.TrimSpace(name) == "" || strings.TrimSpace(key) == "" {
		return out, domain.Validation("recreate requires a new name and stable idempotency key")
	}
	composition, err := c.Create(ctx, domain.CreateRequest{Project: recipe.Project, Baseline: recipe.Baseline, ExpectedBaselineRevision: recipe.BaselineRevision, Name: name, Overrides: recipe.Overrides, TTL: recipe.TTL}, key)
	if err != nil {
		return out, err
	}
	out.Composition = composition
	for _, f := range recipe.Frontends {
		// Fixed length, deterministic, and independent of the caller's display name.
		hash := sha256.Sum256([]byte(composition.ID + "/" + f.Name + "/" + f.Repository))
		bindingName := fmt.Sprintf("recipe-%x", hash[:20])
		view, err := c.BindFrontend(ctx, domain.FrontendKey{Project: recipe.Project, Frontend: bindingName, Revision: f.Revision}, domain.BindFrontendRequest{Composition: composition.ID, Repository: f.Repository})
		if err != nil {
			out.BindingErrors = append(out.BindingErrors, fmt.Sprintf("%s@%s: %v", f.Name, f.Revision, err))
			continue
		}
		out.Bindings = append(out.Bindings, view)
	}
	return out, nil
}
