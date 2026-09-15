package mcp

import (
	"context"

	"github.com/dblooman/envy/internal/client"
	"github.com/dblooman/envy/internal/domain"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type ExportRecipeInput struct {
	ID        string                   `json:"id"`
	Frontends []domain.RecipeSelection `json:"frontends,omitempty" jsonschema:"Explicit frontend names and revisions already bound to this composition. Omit for backend-only export."`
}
type RecreateRecipeInput struct {
	Recipe         domain.Recipe `json:"recipe"`
	Name           string        `json:"name"`
	IdempotencyKey string        `json:"idempotency_key"`
}
type ValidateRecipeInput struct {
	Recipe domain.Recipe `json:"recipe"`
}
type RecipeValidation struct {
	Valid bool   `json:"valid"`
	Scope string `json:"scope"`
}

func addRecipeTools(s *sdk.Server, c *client.Client) {
	sdk.AddTool(s, &sdk.Tool{Name: "export_recipe", Description: "Read exact desired builds and selected frontend revisions from a composition, including a tombstone. Excludes runtime state and credentials; direct tags are rejected."}, func(ctx context.Context, _ *sdk.CallToolRequest, in ExportRecipeInput) (*sdk.CallToolResult, domain.Recipe, error) {
		out, err := c.ExportRecipe(ctx, in.ID, in.Frontends)
		if err != nil {
			return nil, out, err
		}

		return textResult("Recipe exported. Save the structured result outside the running environment."), out, nil
	})
	sdk.AddTool(s, &sdk.Tool{Name: "validate_recipe", Description: "Validate recipe structure locally without allocating resources. Catalog access, artifacts, baseline revision and capacity are checked on recreation."}, func(_ context.Context, _ *sdk.CallToolRequest, in ValidateRecipeInput) (*sdk.CallToolResult, RecipeValidation, error) {
		err := domain.ValidateRecipe(in.Recipe)
		out := RecipeValidation{Valid: err == nil, Scope: "structural only"}
		if err != nil {
			return nil, out, err
		}

		return textResult("Recipe structure is valid; runtime availability has not been checked."), out, nil
	})
	sdk.AddTool(s, &sdk.Tool{Name: "recreate_recipe", Description: "Create a new composition through REST using a recipe and required idempotency key, then create fresh frontend bindings. Inspect binding_errors; retry with the same key after partial failure. Does not wait, build frontends or reuse old evidence."}, func(ctx context.Context, _ *sdk.CallToolRequest, in RecreateRecipeInput) (*sdk.CallToolResult, client.RecreateResult, error) {
		out, err := c.RecreateRecipe(ctx, in.Recipe, in.Name, in.IdempotencyKey)
		if err != nil {
			return nil, out, err
		}

		message := "Composition " + out.Composition.ID + " accepted; wait for readiness and rebuild selected frontends."
		if len(out.BindingErrors) > 0 {
			message += " Some bindings failed; inspect binding_errors and retry with the same key."
		}

		return textResult(message), out, nil
	})
}
