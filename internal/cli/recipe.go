package cli

import (
	"io"
	"os"
	"strings"

	"github.com/dblooman/envy/internal/client"
	"github.com/dblooman/envy/internal/domain"
	"github.com/spf13/cobra"
)

func (r *runner) recipeCommand(getClient func() (*client.Client, error)) *cobra.Command {
	root := &cobra.Command{Use: "recipe", Short: "Save and recreate exact environment intent", RunE: func(cmd *cobra.Command, args []string) error { return r.help(cmd) }}
	var frontends []string
	export := &cobra.Command{Use: "export ID", Args: exactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		selections := []domain.RecipeSelection{}
		for _, value := range frontends {
			name, revision, ok := strings.Cut(value, "=")
			if !ok {
				return domain.Validation("--frontend requires name=revision")
			}
			selections = append(selections, domain.RecipeSelection{Name: name, Revision: revision})
		}
		c, err := getClient()
		if err != nil {
			return err
		}
		r.result, err = c.ExportRecipe(cmd.Context(), args[0], selections)
		return err
	}}
	export.Flags().StringArrayVar(&frontends, "frontend", nil, "existing frontend-name=exact-revision to save; repeat for selected frontends")
	root.AddCommand(export)
	for _, action := range []string{"validate", "recreate"} {
		var file, name, key string
		cmd := &cobra.Command{Use: action, Args: noArgs(), RunE: func(cmd *cobra.Command, args []string) error {
			if file == "" {
				return domain.Validation("--file is required")
			}
			f, err := os.Open(file)
			if err != nil {
				return domain.Validation("cannot open recipe file")
			}
			defer f.Close()
			data, err := io.ReadAll(io.LimitReader(f, (64<<10)+1))
			if err != nil {
				return err
			}
			recipe, err := domain.DecodeRecipe(data)
			if err != nil {
				return err
			}
			if action == "validate" {
				r.result = map[string]any{"valid": true, "recipe": recipe, "scope": "structural only; catalog, artifacts and capacity checked on recreation"}
				return nil
			}
			c, err := getClient()
			if err != nil {
				return err
			}
			out, err := c.RecreateRecipe(cmd.Context(), recipe, name, key)
			r.result = out
			if len(out.BindingErrors) > 0 {
				r.exitCode = 1
			}
			return err
		}}
		cmd.Flags().StringVar(&file, "file", "", "versioned recipe JSON file")
		if action == "recreate" {
			cmd.Flags().StringVar(&name, "name", "", "new composition display name")
			cmd.Flags().StringVar(&key, "idempotency-key", "", "required stable retry key for this recreation")
		}
		root.AddCommand(cmd)
	}
	return root
}
