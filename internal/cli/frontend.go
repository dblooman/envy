package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/dblooman/envy/internal/client"
	"github.com/dblooman/envy/internal/domain"
)

type FrontendResult struct {
	CompositionID string            `json:"composition_id"`
	PublicURL     string            `json:"public_url"`
	GraphQLURL    string            `json:"graphql_url"`
	Phase         domain.Phase      `json:"phase"`
	Environment   map[string]string `json:"environment"`
	FrontendURL   string            `json:"frontend_url,omitempty"`
}

func detectGitContext(getenv func(string) string) (commitSHA, branch, pr string) {
	// 1. Cloudflare Pages environment variables
	if sha := getenv("CF_PAGES_COMMIT_SHA"); sha != "" {
		commitSHA = sha
	}
	if b := getenv("CF_PAGES_BRANCH"); b != "" {
		branch = b
	}

	// 2. GitHub Actions environment variables
	if commitSHA == "" {
		commitSHA = getenv("GITHUB_SHA")
	}
	if branch == "" {
		if head := getenv("GITHUB_HEAD_REF"); head != "" {
			branch = head
		} else {
			branch = getenv("GITHUB_REF_NAME")
		}
	}
	if pr == "" {
		pr = getenv("GITHUB_PR_NUMBER")
	}

	// 3. Fallback to local git CLI if still empty
	if commitSHA == "" {
		if out, err := exec.Command("git", "rev-parse", "HEAD").Output(); err == nil {
			commitSHA = strings.TrimSpace(string(out))
		}
	}
	if branch == "" {
		if out, err := exec.Command("git", "branch", "--show-current").Output(); err == nil {
			branch = strings.TrimSpace(string(out))
		}
	}

	return commitSHA, branch, pr
}

func newFrontendCmd(r *runner, getClient func() (*client.Client, error)) *cobra.Command {
	frontendCmd := &cobra.Command{
		Use:   "frontend",
		Short: "Frontend preview integration and Cloudflare Pages build adapter",
	}

	var project, commitSHA, branch, pr, recordURL string
	var waitReady bool
	var timeout time.Duration

	runCmd := &cobra.Command{
		Use:                "run [flags] [-- <build-command> [args...]]",
		Short:              "Inject preview API/GraphQL URLs into frontend build environment; execute build command",
		SilenceErrors:      true,
		SilenceUsage:       true,
		DisableFlagParsing: false,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := getClient()
			if err != nil {
				return err
			}

			// Auto-detect git context
			detectedSHA, detectedBranch, detectedPR := detectGitContext(r.getenv)
			if commitSHA == "" {
				commitSHA = detectedSHA
			}
			if branch == "" {
				branch = detectedBranch
			}
			if pr == "" {
				pr = detectedPR
			}

			// Project resolution
			if project == "" {
				if cfg, err := loadProjectConfig(""); err == nil && cfg.Project.ID != "" {
					project = cfg.Project.ID
				} else {
					project = r.getenv("ENVY_PROJECT")
				}
			}

			if commitSHA == "" && branch == "" && pr == "" {
				return domain.Validation("could not detect commit SHA, branch, or PR number; pass explicitly via --commit-sha or --branch")
			}

			ctx := cmd.Context()
			comp, err := c.Lookup(ctx, project, commitSHA, branch, pr)
			if err != nil {
				return domain.NotFound(fmt.Sprintf("no active composition found for commit %s (branch: %s, pr: %s); ensure backend preview composition is created before running frontend build: %v", commitSHA, branch, pr, err))
			}

			if comp.ID == "" {
				return domain.NotFound(fmt.Sprintf("no active composition found for commit %s (branch: %s); ensure backend preview composition is created before running frontend build", commitSHA, branch))
			}

			// Wait for readiness if requested
			if waitReady && comp.Phase != domain.PhaseReady {
				readyComp, waitErr := c.Wait(ctx, comp.ID, timeout)
				if waitErr != nil {
					return fmt.Errorf("waiting for composition %s readiness: %w", comp.ID, waitErr)
				}
				comp = readyComp
			}

			endpointURL := ""
			if ep, ok := comp.Endpoints["public"]; ok {
				endpointURL = ep.URL
			}
			if endpointURL == "" {
				return fmt.Errorf("composition %s has no public endpoint allocated", comp.ID)
			}

			// GraphQL endpoint default
			graphQLURL := endpointURL
			if !strings.HasSuffix(graphQLURL, "/graphql") {
				graphQLURL = strings.TrimRight(graphQLURL, "/") + "/graphql"
			}

			// Record frontend URL if passed or available in CF_PAGES_URL
			if recordURL == "" {
				recordURL = r.getenv("CF_PAGES_URL")
			}
			if recordURL != "" {
				_, _ = c.Update(ctx, comp.ID, domain.UpdateRequest{
					ExpectedGeneration: comp.Generation,
					FrontendURL:        &recordURL,
				})
			}

			envVars := map[string]string{
				"VITE_GRAPHQL_URL":          graphQLURL,
				"NEXT_PUBLIC_GRAPHQL_URL":   graphQLURL,
				"GRAPHQL_URL":               graphQLURL,
				"VITE_API_URL":              endpointURL,
				"NEXT_PUBLIC_API_URL":       endpointURL,
				"API_URL":                   endpointURL,
				"ENVY_COMPOSITION_ID":       comp.ID,
				"ENVY_COMPOSITION_URL":      endpointURL,
				"ENVY_COMPOSITION_GRAPHQL":  graphQLURL,
			}

			// If a child command was provided after --
			cmdArgs := cmd.Flags().Args()
			if len(cmdArgs) > 0 {
				subCmd := exec.CommandContext(ctx, cmdArgs[0], cmdArgs[1:]...)
				subCmd.Stdin = os.Stdin
				subCmd.Stdout = os.Stdout
				subCmd.Stderr = os.Stderr

				envList := os.Environ()
				for k, v := range envVars {
					envList = append(envList, fmt.Sprintf("%s=%s", k, v))
				}
				subCmd.Env = envList

				execErr := subCmd.Run()
				if execErr != nil {
					if exitErr, ok := execErr.(*exec.ExitError); ok {
						r.exitCode = exitErr.ExitCode()
						return nil
					}
					return execErr
				}
				r.exitCode = 0
				return nil
			}

			// No child command: output structured result
			r.result = FrontendResult{
				CompositionID: comp.ID,
				PublicURL:     endpointURL,
				GraphQLURL:    graphQLURL,
				Phase:         comp.Phase,
				Environment:   envVars,
				FrontendURL:   recordURL,
			}
			r.exitCode = 0
			return nil
		},
	}

	runCmd.Flags().StringVar(&project, "project", "", "registered Envy project ID")
	runCmd.Flags().StringVar(&commitSHA, "commit-sha", "", "commit SHA to look up")
	runCmd.Flags().StringVar(&branch, "branch", "", "branch name to look up")
	runCmd.Flags().StringVar(&pr, "pr", "", "pull request number to look up")
	runCmd.Flags().StringVar(&recordURL, "record-frontend-url", "", "external frontend URL to record against composition")
	runCmd.Flags().BoolVar(&waitReady, "wait", true, "wait until composition is ready before executing build command")
	runCmd.Flags().DurationVar(&timeout, "timeout", 60*time.Second, "maximum wait timeout")

	frontendCmd.AddCommand(runCmd)
	return frontendCmd
}
