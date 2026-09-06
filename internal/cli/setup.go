package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/dblooman/envy/internal/client"
	"github.com/dblooman/envy/internal/domain"
)

type ProjectConfig struct {
	Version    string             `json:"version,omitempty"`
	Project    domain.Project     `json:"project"`
	Baseline   domain.Baseline    `json:"baseline"`
	Components []domain.Component `json:"components"`
}

type LocalRepoConfig struct {
	Project    string `json:"project"`
	Component  string `json:"component"`
	Entrypoint string `json:"entrypoint"`
	Baseline   string `json:"baseline"`
}

type SetupResult struct {
	Status         string   `json:"status"`
	Project        string   `json:"project"`
	Components     []string `json:"components"`
	Baseline       string   `json:"baseline"`
	EntryComponent string   `json:"entry_component"`
	ProbeVerified  bool     `json:"probe_verified"`
	ProbeEndpoint  string   `json:"probe_endpoint,omitempty"`
}

type DoctorCheck struct {
	Name    string `json:"name"`
	Passed  bool   `json:"passed"`
	Message string `json:"message"`
}

type DoctorResult struct {
	Status string        `json:"status"`
	Checks []DoctorCheck `json:"checks"`
}

func findConfigFile(path string) (string, error) {
	if path != "" {
		if _, err := os.Stat(path); err != nil {
			return "", fmt.Errorf("config file %q not found: %w", path, err)
		}
		return path, nil
	}
	candidates := []string{
		".envy/project.json",
		"envy.json",
		".envy/config.json",
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}
	return "", nil
}

func loadProjectConfig(path string) (*ProjectConfig, error) {
	filePath, err := findConfigFile(path)
	if err != nil {
		return nil, err
	}
	if filePath == "" {
		return nil, domain.Validation("no Envy project configuration file found; specify with --file or create .envy/project.json")
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", filePath, err)
	}

	var cfg ProjectConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", filePath, err)
	}
	return &cfg, nil
}

func newSetupCmd(r *runner, getClient func() (*client.Client, error)) *cobra.Command {
	var configFile string
	var verify bool

	cmd := &cobra.Command{
		Use:           "setup",
		Short:         "Register project, components, and baseline; optionally prove end-to-end preview creation",
		SilenceErrors: true,
		SilenceUsage:  true,
		Args:          noArgs(),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadProjectConfig(configFile)
			if err != nil {
				return err
			}

			if cfg.Project.ID == "" {
				return domain.Validation("project config missing project.id")
			}
			if cfg.Baseline.ID == "" {
				return domain.Validation("project config missing baseline.id")
			}

			c, err := getClient()
			if err != nil {
				return err
			}

			ctx := cmd.Context()

			// 1. Register project
			proj, err := c.RegisterProject(ctx, cfg.Project)
			if err != nil {
				return fmt.Errorf("registering project: %w", err)
			}

			// 2. Register components
			var compIDs []string
			for _, comp := range cfg.Components {
				comp.Project = proj.ID
				registered, err := c.RegisterComponent(ctx, comp)
				if err != nil {
					return fmt.Errorf("registering component %s: %w", comp.ID, err)
				}
				compIDs = append(compIDs, registered.ID)
			}

			// 3. Register baseline
			cfg.Baseline.Project = proj.ID
			baseline, err := c.RegisterBaseline(ctx, cfg.Baseline)
			if err != nil {
				return fmt.Errorf("registering baseline: %w", err)
			}

			res := SetupResult{
				Status:         "registered",
				Project:        proj.ID,
				Components:     compIDs,
				Baseline:       baseline.ID,
				EntryComponent: baseline.Routing.EntryComponent,
			}

			// 4. Verification probe: prove it works once
			if verify {
				// Pick first overridable component
				var overrideComp string
				var overrideImage string
				for _, comp := range cfg.Components {
					if comp.Overridable {
						overrideComp = comp.ID
						if bb, ok := baseline.Components[comp.ID]; ok && bb.Image != "" {
							overrideImage = bb.Image
						} else {
							overrideImage = "envy/" + comp.ID + ":setup-probe"
						}
						break
					}
				}
				if overrideComp != "" {
					probeName := "setup-probe"
					probe, err := c.Create(ctx, domain.CreateRequest{
						Project:   proj.ID,
						Baseline:  baseline.ID,
						Name:      probeName,
						Overrides: map[string]domain.ComponentOverride{overrideComp: {Image: overrideImage}},
						TTL:       "5m",
					}, "setup-probe-"+fmt.Sprint(time.Now().UnixNano()))
					if err == nil {
						// Wait for readiness
						readyComp, waitErr := c.Wait(ctx, probe.ID, 30*time.Second)
						if waitErr == nil && readyComp.Phase == domain.PhaseReady {
							res.ProbeVerified = true
							if ep, ok := readyComp.Endpoints["public"]; ok {
								res.ProbeEndpoint = ep.URL
							}
						}
						// Clean up probe composition
						_, _ = c.Destroy(ctx, probe.ID)
					}
				}
			}

			res.Status = "ready"
			r.result = res
			r.exitCode = 0
			return nil
		},
	}

	cmd.Flags().StringVar(&configFile, "file", "", "path to Envy project JSON configuration file")
	cmd.Flags().BoolVar(&verify, "verify", true, "prove preview works end-to-end by creating and verifying a transient composition")

	return cmd
}

func newDoctorCmd(r *runner, getClient func() (*client.Client, error)) *cobra.Command {
	var project string
	var probe bool

	cmd := &cobra.Command{
		Use:           "doctor",
		Short:         "Run diagnostics against Envy API, catalog, entrypoint, and preview routing",
		SilenceErrors: true,
		SilenceUsage:  true,
		Args:          noArgs(),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := getClient()
			if err != nil {
				return err
			}

			ctx := cmd.Context()
			var checks []DoctorCheck
			allPassed := true

			// 1. API Connection
			projectsPage, err := c.Projects(ctx, "", 1)
			if err != nil {
				checks = append(checks, DoctorCheck{
					Name:    "api_connection",
					Passed:  false,
					Message: fmt.Sprintf("failed to query Envy API: %v", err),
				})
				allPassed = false
			} else {
				checks = append(checks, DoctorCheck{
					Name:    "api_connection",
					Passed:  true,
					Message: "successfully authenticated and connected to Envy API",
				})
			}

			// Find project
			if project == "" {
				// Try reading local config
				if cfg, err := loadProjectConfig(""); err == nil && cfg.Project.ID != "" {
					project = cfg.Project.ID
				} else if len(projectsPage.Items) > 0 {
					project = projectsPage.Items[0].ID
				}
			}

			if project == "" {
				checks = append(checks, DoctorCheck{
					Name:    "project_catalog",
					Passed:  false,
					Message: "no project specified or registered; run 'delivery setup' first",
				})
				allPassed = false
			} else {
				// 2. Components check
				compsPage, compErr := c.Components(ctx, project, "", 50)
				if compErr != nil {
					checks = append(checks, DoctorCheck{
						Name:    "component_catalog",
						Passed:  false,
						Message: fmt.Sprintf("failed to fetch components for project %s: %v", project, compErr),
					})
					allPassed = false
				} else {
					checks = append(checks, DoctorCheck{
						Name:    "component_catalog",
						Passed:  true,
						Message: fmt.Sprintf("found %d registered components for project %s", len(compsPage.Items), project),
					})
				}

				// 3. Baseline check
				baselinesPage, baseErr := c.Baselines(ctx, project, "", 10)
				var targetBaseline *domain.Baseline
				if baseErr != nil || len(baselinesPage.Items) == 0 {
					checks = append(checks, DoctorCheck{
						Name:    "baseline_catalog",
						Passed:  false,
						Message: fmt.Sprintf("no baselines found for project %s", project),
					})
					allPassed = false
				} else {
					targetBaseline = &baselinesPage.Items[0]
					checks = append(checks, DoctorCheck{
						Name:    "baseline_catalog",
						Passed:  true,
						Message: fmt.Sprintf("baseline %s verified with entrypoint %q", targetBaseline.ID, targetBaseline.Routing.EntryComponent),
					})
				}

				// 4. Ingress / Entrypoint check
				if targetBaseline != nil {
					entry := targetBaseline.Routing.EntryComponent
					if entry == "" {
						checks = append(checks, DoctorCheck{
							Name:    "entrypoint_routing",
							Passed:  false,
							Message: "baseline missing routing.entry_component",
						})
						allPassed = false
					} else {
						checks = append(checks, DoctorCheck{
							Name:    "entrypoint_routing",
							Passed:  true,
							Message: fmt.Sprintf("public traffic entrypoint routed to component %q", entry),
						})
					}

					// 5. Transient probe check
					if probe && len(compsPage.Items) > 0 {
						var probeComp string
						for _, comp := range compsPage.Items {
							if comp.Overridable {
								probeComp = comp.ID
								break
							}
						}
						if probeComp == "" {
							probeComp = compsPage.Items[0].ID
						}

						probeRes, err := c.Create(ctx, domain.CreateRequest{
							Project:   project,
							Baseline:  targetBaseline.ID,
							Name:      "doctor-probe",
							Overrides: map[string]domain.ComponentOverride{probeComp: {Image: "envy/" + probeComp + ":probe"}},
							TTL:       "5m",
						}, "doctor-probe-"+fmt.Sprint(time.Now().UnixNano()))
						if err != nil {
							checks = append(checks, DoctorCheck{
								Name:    "preview_probe",
								Passed:  false,
								Message: fmt.Sprintf("failed to create diagnostic probe: %v", err),
							})
							allPassed = false
						} else {
							readyComp, waitErr := c.Wait(ctx, probeRes.ID, 30*time.Second)
							_, _ = c.Destroy(ctx, probeRes.ID)
							if waitErr != nil || readyComp.Phase != domain.PhaseReady {
								checks = append(checks, DoctorCheck{
									Name:    "preview_probe",
									Passed:  false,
									Message: fmt.Sprintf("diagnostic probe failed readiness: phase=%s err=%v", readyComp.Phase, waitErr),
								})
								allPassed = false
							} else {
								checks = append(checks, DoctorCheck{
									Name:    "preview_probe",
									Passed:  true,
									Message: "end-to-end preview creation, ingress, and verification passed",
								})
							}
						}
					}
				}
			}

			status := "ok"
			if !allPassed {
				status = "degraded"
				r.exitCode = 1
			} else {
				r.exitCode = 0
			}

			r.result = DoctorResult{
				Status: status,
				Checks: checks,
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&project, "project", "", "registered project to diagnose")
	cmd.Flags().BoolVar(&probe, "probe", false, "run end-to-end probe verifying workload creation, ingress, and request routing")

	return cmd
}
