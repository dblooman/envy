// Package cli provides the JSON-only delivery command over the private REST client.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/dblooman/envy/internal/client"
	"github.com/dblooman/envy/internal/domain"
)

const usage = "delivery installation check --file installation.json; delivery recipe export|validate|recreate [flags]; delivery source list|register|enable|disable|branches|commits|resolve|report [flags]; delivery frontend bind|get|resolve|publish|check|list [flags]; delivery catalog validate|apply --file application.json; delivery composition create|list|get|inspect|wait|endpoints|update|destroy|logs|events [id] [flags]; use --help after a command for its flags"

type runner struct {
	getenv    func(string) string
	result    any
	exitCode  int
	helpShown bool
}

func (r *runner) help(cmd *cobra.Command) error {
	r.helpShown = true
	r.exitCode = 0
	if cmd.Name() == "delivery" || cmd.Name() == "composition" {
		r.result = map[string]string{"usage": usage}
		return nil
	}
	flags := map[string]string{}
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		if f.Name == "help" {
			return
		}
		flags["--"+f.Name] = f.Usage
	})
	r.result = map[string]any{
		"usage":   usage,
		"command": cmd.Name(),
		"flags":   flags,
	}
	return nil
}

func exactArgs(n int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) != n {
			return domain.Validation("put the composition ID before flags; unexpected or missing positional arguments")
		}
		return nil
	}
}

func noArgs() cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) != 0 {
			return domain.Validation("put the composition ID before flags; unexpected or missing positional arguments")
		}
		return nil
	}
}

// NewRootCmd constructs the delivery root cobra command.
func NewRootCmd(r *runner) *cobra.Command {
	rootCmd := &cobra.Command{
		Use:           "delivery",
		Short:         "Envy delivery CLI",
		SilenceErrors: true,
		SilenceUsage:  true,
		CompletionOptions: cobra.CompletionOptions{
			DisableDefaultCmd: true,
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return r.help(cmd)
		},
	}

	rootCmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		_ = r.help(cmd)
	})

	apiURL := r.getenv("ENVY_API_URL")
	if apiURL == "" {
		apiURL = "http://127.0.0.1:8081"
	}
	var tokenFile string
	rootCmd.PersistentFlags().StringVar(&apiURL, "api-url", apiURL, "REST API URL")
	rootCmd.PersistentFlags().StringVar(&tokenFile, "token-file", r.getenv("ENVY_API_TOKEN_FILE"), "API token file; takes precedence over ENVY_API_TOKEN")

	getClient := func() (*client.Client, error) {
		token := r.getenv("ENVY_API_TOKEN")
		if tokenFile != "" {
			data, err := os.ReadFile(tokenFile)
			if err != nil {
				return nil, fmt.Errorf("read API token file: %w", err)
			}
			token = strings.TrimSpace(string(data))
		}
		return client.NewWithIdentity(apiURL, token, nil, "cli", os.Getenv("ENVY_TASK_ID"))
	}

	rootCmd.AddCommand(r.catalogCommand(getClient))
	rootCmd.AddCommand(r.previewProfileCommand(getClient))
	rootCmd.AddCommand(installationCommand(r))
	rootCmd.AddCommand(r.recipeCommand(getClient))
	rootCmd.AddCommand(r.sourceCommand(getClient))
	rootCmd.AddCommand(r.frontendCommand(getClient))
	compositionCmd := &cobra.Command{
		Use:           "composition",
		Short:         "Manage compositions",
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return domain.Validation(usage)
		},
	}

	// create
	var createOverrides, updateOverrides, createBuilds, updateBuilds []string
	var project, baseline, name, ttl, key, createImage, createComponent string
	var createInheritAll bool
	createCmd := &cobra.Command{
		Use:           "create",
		Short:         "Create a composition",
		SilenceErrors: true,
		SilenceUsage:  true,
		Args:          noArgs(),
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(name) == "" {
				return domain.Validation("create requires --name")
			}
			if !createInheritAll && strings.TrimSpace(createImage) == "" && len(createOverrides) == 0 && len(createBuilds) == 0 {
				return domain.Validation("create requires --image, --override, --build, or --inherit-all")
			}
			c, err := getClient()
			if err != nil {
				return err
			}
			overrides := map[string]domain.ComponentOverride{}
			if createInheritAll {
				if strings.TrimSpace(createImage) != "" || len(createOverrides) != 0 || len(createBuilds) != 0 || cmd.Flags().Changed("component") {
					return domain.Validation("--inherit-all cannot be combined with override selection flags")
				}
			} else {
				var err error
				overrides, err = parseBuildOverrides(createOverrides, createBuilds, createComponent, createImage, cmd.Flags().Changed("component"))
				if err != nil {
					return err
				}
			}
			guardsRaw, _ := cmd.Flags().GetStringToString("expected-preview-revision")
			guards, err := previewGuards(guardsRaw)
			if err != nil {
				return err
			}
			res, err := c.Create(cmd.Context(), domain.CreateRequest{ExpectedPreviewRevisions: guards,
				Project:   project,
				Baseline:  baseline,
				Name:      name,
				Overrides: overrides,
				TTL:       ttl,
			}, key)
			if err != nil {
				return err
			}
			r.result = res
			r.exitCode = 0
			return nil
		},
	}
	createCmd.Flags().StringVar(&project, "project", "demo", "registered project")
	createCmd.Flags().StringVar(&baseline, "baseline", "staging", "registered baseline")
	createCmd.Flags().StringVar(&name, "name", "", "composition name (required)")
	createCmd.Flags().StringVar(&ttl, "ttl", "", "expiry duration; server default when omitted")
	createCmd.Flags().StringVar(&key, "idempotency-key", "", "stable create retry key")
	createCmd.Flags().StringVar(&createImage, "image", "", "direct prebuilt image; alternatively use --build")
	createCmd.Flags().StringVar(&createComponent, "component", "service-b", "registered override component")

	createCmd.Flags().StringArrayVar(&createOverrides, "override", nil, "component=image; repeat for up to three components")

	createCmd.Flags().StringArrayVar(&createBuilds, "build", nil, "component=build_id; repeat for published builds")
	createCmd.Flags().BoolVar(&createInheritAll, "inherit-all", false, "create a preview URL that inherits the complete baseline")

	createCmd.Flags().StringToString("expected-preview-revision", nil, "component=approved revision; repeat or comma separate")

	// update
	var updateImage, updateComponent string
	var inheritAll bool
	var generation int64
	updateCmd := &cobra.Command{
		Use:           "update <id>",
		Short:         "Update a composition override",
		SilenceErrors: true,
		SilenceUsage:  true,
		Args:          exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !inheritAll && strings.TrimSpace(updateImage) == "" && len(updateOverrides) == 0 && len(updateBuilds) == 0 {
				return domain.Validation("update requires --image, --override, --build, or --inherit-all")
			}
			if generation < 1 {
				return domain.Validation("--expected-generation must be positive")
			}
			c, err := getClient()
			if err != nil {
				return err
			}
			overrides := map[string]domain.ComponentOverride{}
			if inheritAll {
				if strings.TrimSpace(updateImage) != "" || len(updateOverrides) != 0 || len(updateBuilds) != 0 || cmd.Flags().Changed("component") {
					return domain.Validation("--inherit-all cannot be combined with override selection flags")
				}
			} else {
				var err error
				overrides, err = parseBuildOverrides(updateOverrides, updateBuilds, updateComponent, updateImage, cmd.Flags().Changed("component"))
				if err != nil {
					return err
				}
			}
			guardsRaw, _ := cmd.Flags().GetStringToString("expected-preview-revision")
			guards, err := previewGuards(guardsRaw)
			if err != nil {
				return err
			}
			res, err := c.Update(cmd.Context(), args[0], domain.UpdateRequest{ExpectedPreviewRevisions: guards,
				ExpectedGeneration: generation,
				Overrides:          overrides,
			})
			if err != nil {
				return err
			}
			r.result = res
			r.exitCode = 0
			return nil
		},
	}
	updateCmd.Flags().StringVar(&updateImage, "image", "", "direct prebuilt image; alternatively use --build")
	updateCmd.Flags().StringVar(&updateComponent, "component", "service-b", "registered override component")
	updateCmd.Flags().StringToString("expected-preview-revision", nil, "component=captured revision")
	updateCmd.Flags().Int64Var(&generation, "expected-generation", 0, "current desired generation (required)")

	updateCmd.Flags().StringArrayVar(&updateOverrides, "override", nil, "complete component=image set; repeat for every override")

	updateCmd.Flags().StringArrayVar(&updateBuilds, "build", nil, "component=build_id; retain every overridden component")
	updateCmd.Flags().BoolVar(&inheritAll, "inherit-all", false, "remove every override and route the preview URL through the baseline")

	// get
	getCmd := &cobra.Command{
		Use:           "get <id>",
		Short:         "Get composition details",
		SilenceErrors: true,
		SilenceUsage:  true,
		Args:          exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := getClient()
			if err != nil {
				return err
			}
			res, err := c.Get(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			r.result = res
			r.exitCode = 0
			return nil
		},
	}

	// inspect (alias for get)
	inspectCmd := &cobra.Command{
		Use:           "inspect <id>",
		Short:         "Inspect a composition (alias for get)",
		SilenceErrors: true,
		SilenceUsage:  true,
		Args:          exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := getClient()
			if err != nil {
				return err
			}
			res, err := c.Get(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			r.result = res
			r.exitCode = 0
			return nil
		},
	}

	// wait
	var timeout time.Duration
	waitCmd := &cobra.Command{
		Use:           "wait <id>",
		Short:         "Wait for a composition to reach a terminal or ready phase",
		SilenceErrors: true,
		SilenceUsage:  true,
		Args:          exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if timeout <= 0 || timeout > 60*time.Second {
				return domain.Validation("--timeout must be positive and at most 60s")
			}
			c, err := getClient()
			if err != nil {
				return err
			}
			composition, err := c.Wait(cmd.Context(), args[0], timeout)
			r.result = composition
			if err != nil {
				return err
			}
			switch composition.Phase {
			case domain.PhaseReady, domain.PhaseDestroyed:
				r.exitCode = 0
			case domain.PhaseFailed:
				r.exitCode = 1
			default:
				r.exitCode = 2
			}
			return nil
		},
	}
	waitCmd.Flags().DurationVar(&timeout, "timeout", 30*time.Second, "bounded wait duration, maximum 60s")

	// endpoints
	endpointsCmd := &cobra.Command{
		Use:           "endpoints <id>",
		Short:         "Get composition endpoints",
		SilenceErrors: true,
		SilenceUsage:  true,
		Args:          exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := getClient()
			if err != nil {
				return err
			}
			res, err := c.Endpoints(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			r.result = res
			r.exitCode = 0
			return nil
		},
	}

	// destroy
	destroyCmd := &cobra.Command{
		Use:           "destroy <id>",
		Short:         "Destroy a composition",
		SilenceErrors: true,
		SilenceUsage:  true,
		Args:          exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := getClient()
			if err != nil {
				return err
			}
			res, err := c.Destroy(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			r.result = res
			r.exitCode = 0
			return nil
		},
	}

	// logs
	var logsComponent string
	var logOptions domain.LogOptions
	var since time.Duration
	logsCmd := &cobra.Command{
		Use:           "logs <id>",
		Short:         "Fetch composition logs",
		SilenceErrors: true,
		SilenceUsage:  true,
		Args:          exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if since < 0 || since > 24*time.Hour || since%time.Second != 0 || logOptions.TailLines < 1 || logOptions.MaxBytes < 1 {
				return domain.Validation("log limits must be positive; --since must use whole seconds up to 24h")
			}
			logOptions.SinceSeconds = int64(since / time.Second)
			c, err := getClient()
			if err != nil {
				return err
			}
			res, err := c.Logs(cmd.Context(), args[0], logsComponent, logOptions)
			if err != nil {
				return err
			}
			r.result = res
			r.exitCode = 0
			return nil
		},
	}
	logsCmd.Flags().StringVar(&logsComponent, "component", "service-b", "logical component; inherited logs are shared-baseline logs")
	logsCmd.Flags().Int64Var(&logOptions.TailLines, "tail-lines", 200, "maximum lines per pod, 1–1000")
	logsCmd.Flags().Int64Var(&logOptions.MaxBytes, "max-bytes", 65536, "total log byte cap, 1–262144")
	logsCmd.Flags().DurationVar(&since, "since", 0, "lookback duration in whole seconds, maximum 24h")
	logsCmd.Flags().BoolVar(&logOptions.Previous, "previous", false, "read last terminated container instance")

	// events
	var eventsAfter string
	var eventsLimit int
	eventsCmd := &cobra.Command{
		Use:           "events <id>",
		Short:         "Fetch composition lifecycle events",
		SilenceErrors: true,
		SilenceUsage:  true,
		Args:          exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := getClient()
			if err != nil {
				return err
			}
			res, err := c.Events(cmd.Context(), args[0], eventsAfter, eventsLimit)
			if err != nil {
				return err
			}
			r.result = res
			r.exitCode = 0
			return nil
		},
	}
	eventsCmd.Flags().StringVar(&eventsAfter, "after", "", "next_cursor from preceding page")
	eventsCmd.Flags().IntVar(&eventsLimit, "limit", 20, "page size, 1–100")

	// list
	var listProject, listAfter string
	var listLimit int
	listCmd := &cobra.Command{
		Use:           "list",
		Short:         "List compositions",
		SilenceErrors: true,
		SilenceUsage:  true,
		Args:          noArgs(),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := getClient()
			if err != nil {
				return err
			}
			res, err := c.List(cmd.Context(), listProject, listAfter, listLimit)
			if err != nil {
				return err
			}
			r.result = res
			r.exitCode = 0
			return nil
		},
	}
	listCmd.Flags().StringVar(&listProject, "project", "", "optional project filter")
	listCmd.Flags().StringVar(&listAfter, "after", "", "next_cursor from preceding page")
	listCmd.Flags().IntVar(&listLimit, "limit", 20, "page size, 1–100")

	compositionCmd.AddCommand(
		createCmd,
		updateCmd,
		getCmd,
		inspectCmd,
		waitCmd,
		endpointsCmd,
		destroyCmd,
		logsCmd,
		eventsCmd,
		listCmd,
	)

	rootCmd.AddCommand(compositionCmd)

	return rootCmd
}

// Run emits one JSON result on stdout, or a structured error on stderr. It
// returns a process exit code, allowing tests to exercise the real command parser.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	r := &runner{getenv: getenv}
	cmd := NewRootCmd(r)
	cmd.SetArgs(args)
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)

	err := cmd.ExecuteContext(ctx)
	if err != nil {
		var public *domain.Error
		code := 1
		if !errors.As(err, &public) {
			public = &domain.Error{Code: "client_error", Message: err.Error()}
		}
		if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
			public = &domain.Error{Code: "cancelled", Message: "command cancelled"}
			code = 130
		}
		_ = json.NewEncoder(stderr).Encode(map[string]any{"error": public})
		return code
	}
	if r.result != nil {
		if err := json.NewEncoder(stdout).Encode(r.result); err != nil {
			_ = json.NewEncoder(stderr).Encode(map[string]any{"error": &domain.Error{Code: "output_error", Message: "could not write JSON output"}})
			return 1
		}
	}
	return r.exitCode
}

func parseOverrides(values []string, component, image string, componentFlag bool) (map[string]domain.ComponentOverride, error) {
	out := map[string]domain.ComponentOverride{}
	if len(values) == 0 {
		out[component] = domain.ComponentOverride{Image: image}
	} else {
		if image != "" || componentFlag {
			return nil, domain.Validation("--override cannot be combined with --image or --component")
		}
		for _, value := range values {
			name, image, ok := strings.Cut(value, "=")
			if !ok || !domain.ValidCatalogID(name) || strings.TrimSpace(image) == "" {
				return nil, domain.Validation("--override requires component=image")
			}
			if _, exists := out[name]; exists {
				return nil, domain.Validation("duplicate override component")
			}
			out[name] = domain.ComponentOverride{Image: image}
		}
	}
	if len(out) < 1 || len(out) > domain.MaxOverrides {
		return nil, domain.Validation("one to three overrides are required")
	}
	for name, override := range out {
		if !domain.ValidCatalogID(name) || override.Image == "" || len(override.Image) > 512 || strings.ContainsAny(override.Image, " \t\r\n") {
			return nil, domain.Validation("invalid override component or image")
		}
	}
	return out, nil
}
