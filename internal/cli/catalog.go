package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"

	"github.com/dblooman/envy/internal/client"
	"github.com/dblooman/envy/internal/domain"
	"github.com/spf13/cobra"
)

func (r *runner) catalogCommand(getClient func() (*client.Client, error)) *cobra.Command {
	root := &cobra.Command{Use: "catalog", Short: "Validate and register existing application infrastructure", RunE: func(cmd *cobra.Command, args []string) error { return r.help(cmd) }}
	for _, action := range []string{"validate", "apply"} {
		var file string
		cmd := &cobra.Command{Use: action, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
			if file == "" {
				return domain.Validation("--file is required")
			}

			f, err := os.Open(file)
			if err != nil {
				return domain.Validation("cannot open configuration file")
			}
			defer f.Close()
			data, err := io.ReadAll(io.LimitReader(f, (64<<10)+1))
			if err != nil || len(data) > 64<<10 {
				return domain.Validation("configuration must be at most 64 KiB")
			}

			var m domain.CatalogManifest
			dec := json.NewDecoder(bytes.NewReader(data))
			dec.DisallowUnknownFields()
			if err = dec.Decode(&m); err != nil {
				return domain.Validation("configuration must be JSON with no unknown fields")
			}

			var extra any
			if dec.Decode(&extra) != io.EOF {
				return domain.Validation("configuration must contain one JSON value")
			}

			c, err := getClient()
			if err != nil {
				return err
			}

			r.result, err = c.Onboard(cmd.Context(), m, action == "apply")
			return err
		}}
		cmd.Flags().StringVar(&file, "file", "", "path to an envy/v1 JSON configuration")
		root.AddCommand(cmd)
	}

	return root
}
