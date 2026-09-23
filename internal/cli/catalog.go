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

func readJSONFile(path string, out any) error {
	if path == "" {
		return domain.Validation("--file is required")
	}

	f, err := os.Open(path)
	if err != nil {
		return domain.Validation("cannot open JSON file")
	}

	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, (64<<10)+1))
	if err != nil || len(data) > 64<<10 {
		return domain.Validation("JSON file must be at most 64 KiB")
	}

	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return domain.Validation("file must be JSON with no unknown fields")
	}

	var extra any
	if dec.Decode(&extra) != io.EOF {
		return domain.Validation("file must contain one JSON value")
	}

	return nil
}

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
	draft := &cobra.Command{Use: "draft", Short: "Manage private non-secret onboarding preparation", RunE: func(cmd *cobra.Command, _ []string) error { return r.help(cmd) }}
	get := &cobra.Command{Use: "get PROJECT", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient()
		if err != nil {
			return err
		}

		r.result, err = c.GetOnboardingDraft(cmd.Context(), args[0])
		return err
	}}
	var draftFile string
	save := &cobra.Command{Use: "save", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		var input domain.OnboardingDraft
		if err := readJSONFile(draftFile, &input); err != nil {
			return err
		}

		c, err := getClient()
		if err != nil {
			return err
		}

		r.result, err = c.SaveOnboardingDraft(cmd.Context(), input)
		return err
	}}
	save.Flags().StringVar(&draftFile, "file", "", "path to a non-secret onboarding draft JSON file")
	var deleteRevision int64
	deleteCmd := &cobra.Command{Use: "delete PROJECT", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient()
		if err != nil {
			return err
		}

		if err := c.DeleteOnboardingDraft(cmd.Context(), args[0], deleteRevision); err != nil {
			return err
		}

		r.result = map[string]any{"project": args[0], "deleted": true}
		return nil
	}}
	deleteCmd.Flags().Int64Var(&deleteRevision, "revision", 0, "revision to delete (required)")
	draft.AddCommand(get, save, deleteCmd)
	root.AddCommand(draft)

	return root
}
