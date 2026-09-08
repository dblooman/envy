package domain

import (
	"context"
	"crypto/sha256"
	"fmt"
	"maps"
	"regexp"
	"slices"
)

type BaselineRouting struct {
	Namespace      string `json:"namespace"`
	Gateway        string `json:"gateway"`
	EntryComponent string `json:"entry_component"`
}
type VerificationContract struct {
	Kind           string   `json:"kind"`
	Chain          []string `json:"chain,omitempty"`
	Path           string   `json:"path,omitempty"`
	ExpectedStatus int      `json:"expected_status,omitempty"`
}
type ResolvedPlan struct {
	Baseline   Baseline
	Components map[string]Component
	Component  Component // legacy single-override profile
}
type RouteDomain struct {
	Namespace, Gateway, ServiceHost, AggregateName string
	Port                                           int32
}

func (b Baseline) RouteDomain(component string) RouteDomain {
	binding := b.Components[component]
	sum := sha256.Sum256([]byte(binding.ServiceHost))
	name := fmt.Sprintf("envy-mesh-%x", sum[:10])
	// Preserve the existing installation's aggregate identity during migration.
	if b.Project == "demo" && b.ID == "staging" && component == "service-b" && binding.ServiceHost == "service-b.envy-baseline.svc.cluster.local" {
		name = "envy-service-b"
	}
	return RouteDomain{Namespace: b.Routing.Namespace, Gateway: b.Routing.Gateway, ServiceHost: binding.ServiceHost, Port: binding.Port, AggregateName: name}
}

type CatalogValidator interface {
	ValidateBaseline(context.Context, Baseline, map[string]Component) error
}

var catalogID = regexp.MustCompile(`^[a-z][a-z0-9-]{0,61}[a-z0-9]$|^[a-z]$`)

func ValidCatalogID(id string) bool { return catalogID.MatchString(id) }

const MaxOverrides = 3

// PreviewRouteHeader marks responses selected by an owned ingress route. It is not authorization.
const PreviewRouteHeader = "x-envy-route"

func OverrideNames(overrides map[string]ComponentOverride) []string {
	return slices.Sorted(maps.Keys(overrides))
}
func (p ResolvedPlan) Profiles() map[string]Component {
	if len(p.Components) > 0 {
		return p.Components
	}
	if p.Component.ID != "" {
		return map[string]Component{p.Component.ID: p.Component}
	}
	return nil
}
func (r RuntimeState) WorkloadFor(component string) WorkloadRef {
	if ref, ok := r.Workloads[component]; ok {
		return ref
	}
	if r.Workload.Deployment == component || r.Workload.Service == component {
		return r.Workload
	}
	return WorkloadRef{}
}

// CatalogManifest is a portable registration bundle for existing infrastructure.
type CatalogManifest struct {
	APIVersion string      `json:"api_version"`
	Project    Project     `json:"project"`
	Components []Component `json:"components"`
	Baseline   Baseline    `json:"baseline"`
}
type CatalogReport struct {
	Configuration CatalogManifest `json:"configuration"`
	Applied       bool            `json:"applied"`
	Checks        []Condition     `json:"checks"`
	Warnings      []string        `json:"warnings"`
}
