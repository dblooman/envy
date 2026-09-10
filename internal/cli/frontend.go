package cli

import (
	"time"

	"github.com/dblooman/envy/internal/client"
	"github.com/dblooman/envy/internal/domain"
	"github.com/spf13/cobra"
)

func (r *runner) frontendCommand(getClient func() (*client.Client, error)) *cobra.Command {
	root := &cobra.Command{Use: "frontend", Short: "Bind exact frontend revisions to compositions", RunE: func(cmd *cobra.Command, _ []string) error { return r.help(cmd) }}
	for _, action := range []string{"bind", "get", "resolve", "publish", "check", "list"} {
		var k domain.FrontendKey
		var composition, repository, url, status, message, after string
		var version, generation int64
		var timeout time.Duration
		var limit int
		cmd := &cobra.Command{Use: action, Args: noArgs(), RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := getClient()
			if err != nil {
				return err
			}
			switch action {
			case "bind":
				r.result, err = c.BindFrontend(cmd.Context(), k, domain.BindFrontendRequest{Composition: composition, Repository: repository})
			case "get":
				r.result, err = c.FrontendBinding(cmd.Context(), k)
			case "resolve":
				r.result, err = c.ResolveFrontend(cmd.Context(), k, timeout)
			case "publish":
				r.result, err = c.PublishFrontend(cmd.Context(), k, domain.PublishFrontendRequest{ExpectedVersion: version, URL: url})
			case "check":
				r.result, err = c.CheckFrontend(cmd.Context(), k, domain.FrontendCheckRequest{ExpectedVersion: version, CompositionGeneration: generation, Status: status, Message: message})
			case "list":
				r.result, err = c.FrontendBindings(cmd.Context(), composition, after, limit)
			}
			return err
		}}
		if action != "list" {
			cmd.Flags().StringVar(&k.Project, "project", "", "project identifier")
			cmd.Flags().StringVar(&k.Frontend, "frontend", "", "frontend name")
			cmd.Flags().StringVar(&k.Revision, "revision", "", "full lowercase Git commit SHA")
		}
		if action == "bind" || action == "list" {
			cmd.Flags().StringVar(&composition, "composition", "", "explicit composition ID")
		}
		if action == "bind" {
			cmd.Flags().StringVar(&repository, "repository", "", "HTTPS source repository URL")
		}
		if action == "resolve" {
			cmd.Flags().DurationVar(&timeout, "timeout", 60*time.Second, "bounded wait, maximum 5m; 0 performs one check")
		}
		if action == "publish" || action == "check" {
			cmd.Flags().Int64Var(&version, "expected-version", 0, "current frontend binding version")
		}
		if action == "publish" {
			cmd.Flags().StringVar(&url, "url", "", "reported external frontend URL; does not verify hosting")
		}
		if action == "check" {
			cmd.Flags().Int64Var(&generation, "composition-generation", 0, "backend generation used by the browser check")
			cmd.Flags().StringVar(&status, "status", "", "caller-reported passed or failed")
			cmd.Flags().StringVar(&message, "message", "", "summary of the browser checks actually performed")
		}
		if action == "list" {
			cmd.Flags().StringVar(&after, "after", "", "pagination cursor")
			cmd.Flags().IntVar(&limit, "limit", 20, "page size, maximum 100")
		}
		root.AddCommand(cmd)
	}
	return root
}
