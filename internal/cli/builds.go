package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"

	"github.com/dblooman/envy/internal/client"
	"github.com/dblooman/envy/internal/domain"
	"github.com/spf13/cobra"
)

func readBuildJSON(path string, out any) error {
	if path == "" {
		return domain.Validation("--file is required")
	}

	f, err := os.Open(path)
	if err != nil {
		return domain.Validation("cannot read JSON file")
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, (64<<10)+1))
	if err != nil || len(data) > 64<<10 {
		return domain.Validation("JSON file must be at most 64 KiB")
	}

	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err = dec.Decode(out); err != nil {
		return domain.Validation("invalid JSON file")
	}

	var extra any
	if dec.Decode(&extra) != io.EOF {
		return domain.Validation("file must contain one JSON value")
	}

	return nil
}

func (r *runner) sourceCommand(getClient func() (*client.Client, error)) *cobra.Command {
	root := &cobra.Command{Use: "source", Short: "Register repositories, browse revisions, and report CI builds"}
	var project, repository, component, ref, after string
	var limit, page int
	root.PersistentFlags().StringVar(&project, "project", "demo", "registered project")
	root.PersistentFlags().StringVar(&repository, "repository", "", "registered repository ID")
	root.PersistentFlags().StringVar(&component, "component", "", "component ID")
	root.PersistentFlags().StringVar(&ref, "ref", "", "branch name or full commit SHA")
	root.PersistentFlags().StringVar(&after, "after", "", "build or repository pagination cursor")
	root.PersistentFlags().IntVar(&limit, "limit", 20, "page size (1–100)")
	root.PersistentFlags().IntVar(&page, "page", 1, "GitHub page (30 results)")
	for _, action := range []string{"list", "register", "enable", "disable", "branches", "commits", "resolve", "report"} {
		var file string
		cmd := &cobra.Command{Use: action, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := getClient()
			if err != nil {
				return err
			}

			ctx := cmd.Context()
			switch action {
			case "list":
				r.result, err = c.SourceRepositories(ctx, project, after, limit)
			case "register":
				var in domain.SourceRepository
				if err = readBuildJSON(file, &in); err == nil {
					if in.Project == "" {
						in.Project = project
					}

					r.result, err = c.RegisterSourceRepository(ctx, in)
				}
			case "enable", "disable":
				r.result, err = c.EnableSourceRepository(ctx, project, repository, action == "enable")
			case "branches":
				r.result, err = c.SourceBranches(ctx, project, repository, page)
			case "commits":
				r.result, err = c.SourceCommits(ctx, project, repository, ref, page)
			case "resolve":
				r.result, err = c.ResolveRevision(ctx, project, repository, component, ref, after, limit)
			case "report":
				var in domain.BuildReport
				if err = readBuildJSON(file, &in); err == nil {
					r.result, err = c.RecordBuild(ctx, project, repository, in)
				}
			}

			return err
		}}
		if action == "register" || action == "report" {
			cmd.Flags().StringVar(&file, "file", "", "JSON registration or CI build report")
		}

		root.AddCommand(cmd)
	}

	return root
}

func parseBuildOverrides(images, builds []string, component, image string, componentFlag bool) (map[string]domain.ComponentOverride, error) {
	var out map[string]domain.ComponentOverride
	if len(images) > 0 || image != "" || len(builds) == 0 {
		var err error
		out, err = parseOverrides(images, component, image, componentFlag)
		if err != nil {
			return nil, err
		}
	} else {
		if componentFlag {
			return nil, domain.Validation("--component cannot be combined with --build")
		}

		out = map[string]domain.ComponentOverride{}
	}

	for _, item := range builds {
		name, id, ok := strings.Cut(item, "=")
		if !ok || !domain.ValidCatalogID(name) || len(id) != 64 || strings.Trim(id, "0123456789abcdef") != "" {
			return nil, domain.Validation("--build requires component=build_id")
		}

		if _, exists := out[name]; exists {
			return nil, domain.Validation("duplicate component override")
		}

		out[name] = domain.ComponentOverride{BuildID: id}
	}

	if len(out) > domain.MaxOverrides {
		return nil, domain.Validation("at most three overrides are allowed")
	}

	return out, nil
}
