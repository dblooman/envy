package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strconv"

	"github.com/dblooman/envy/internal/client"
	"github.com/dblooman/envy/internal/domain"
	"github.com/spf13/cobra"
)

func (r *runner) previewProfileCommand(getClient func() (*client.Client, error)) *cobra.Command {
	root := &cobra.Command{Use: "preview-profile", Short: "Discover, approve and inspect deployment-derived preview configuration"}
	for _, action := range []string{"discover", "approve", "inspect"} {
		var project, baseline, component, file string
		cmd := &cobra.Command{Use: action, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
			c, err := getClient()
			if err != nil {
				return err
			}

			if action == "inspect" {
				r.result, err = c.InspectPreview(cmd.Context(), project, baseline, component)
				return err
			}

			var input any
			selection := domain.PreviewSelection{}
			approval := domain.PreviewApproval{}
			if action == "approve" {
				input = &approval
				if file == "" {
					return domain.Validation("--file containing the reviewed approval is required")
				}
			} else {
				input = &selection
			}

			if file != "" {
				f, e := os.Open(file)
				if e != nil {
					return domain.Validation("cannot open preview input")
				}
				defer f.Close()
				data, e := io.ReadAll(io.LimitReader(f, (64<<10)+1))
				if e != nil || len(data) > 64<<10 {
					return domain.Validation("preview input exceeds 64 KiB")
				}

				dec := json.NewDecoder(bytes.NewReader(data))
				dec.DisallowUnknownFields()
				if dec.Decode(input) != nil {
					return domain.Validation("invalid preview input JSON")
				}

				var extra any
				if dec.Decode(&extra) != io.EOF {
					return domain.Validation("expected one JSON value")
				}
			}

			if action == "approve" {
				r.result, err = c.ApprovePreview(cmd.Context(), project, baseline, component, approval)
			} else {
				r.result, err = c.DiscoverPreview(cmd.Context(), project, baseline, component, selection)
			}

			return err
		}}
		cmd.Flags().StringVar(&project, "project", "", "registered project")
		cmd.Flags().StringVar(&baseline, "baseline", "", "registered baseline")
		cmd.Flags().StringVar(&component, "component", "", "registered component")
		if action != "inspect" {
			cmd.Flags().StringVar(&file, "file", "", "JSON selection or reviewed approval")
		}

		root.AddCommand(cmd)
	}

	return root
}

func previewGuards(values map[string]string) (map[string]int64, error) {
	if len(values) == 0 {
		return nil, nil
	}

	out := map[string]int64{}
	for key, value := range values {
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil || n < 1 || !domain.ValidCatalogID(key) {
			return nil, domain.Validation("--expected-preview-revision requires component=positive-revision")
		}

		out[key] = n
	}

	return out, nil
}
