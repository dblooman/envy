package cli

import (
	"github.com/dblooman/envy/internal/client"
	"github.com/dblooman/envy/internal/domain"
	"github.com/spf13/cobra"
)

func (r *runner) prPreviewCommand(getClient func() (*client.Client, error)) *cobra.Command {
	root := &cobra.Command{Use: "pr-preview", Short: "Inspect and control label-driven GitHub previews"}
	var project, after string
	var limit int
	root.PersistentFlags().StringVar(&project, "project", "", "filter project")
	root.PersistentFlags().StringVar(&after, "after", "", "pagination cursor")
	root.PersistentFlags().IntVar(&limit, "limit", 20, "page size")
	for _, action := range []string{"list", "get", "stop", "restart", "policy"} {
		var file string
		cmd := &cobra.Command{Use: action + " [id]", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			c, e := getClient()
			if e != nil {
				return e
			}

			switch action {
			case "list":
				r.result, e = c.PRPreviews(cmd.Context(), project, after, limit)
			case "get":
				r.result, e = c.PRPreview(cmd.Context(), args[0])
			case "stop", "restart":
				r.result, e = c.ControlPRPreview(cmd.Context(), args[0], action)
			case "policy":
				var p domain.PRPreviewPolicy
				e = readBuildJSON(file, &p)
				if e == nil {
					r.result, e = c.SavePreviewPolicy(cmd.Context(), p)
				}
			}

			return e
		}}
		if action == "list" || action == "policy" {
			cmd.Args = cobra.NoArgs
			cmd.Use = action
		}

		if action == "policy" {
			cmd.Flags().StringVar(&file, "file", "", "preview policy JSON")
		}

		root.AddCommand(cmd)
	}

	return root
}
