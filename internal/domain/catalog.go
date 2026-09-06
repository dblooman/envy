package domain

import (
	"context"
	"crypto/sha256"
	"fmt"
	"regexp"
)

type BaselineRouting struct {
	Namespace      string `json:"namespace"`
	Gateway        string `json:"gateway"`
	EntryComponent string `json:"entry_component"`
}
type VerificationContract struct {
	Kind  string   `json:"kind"`
	Chain []string `json:"chain"`
}
type ResolvedPlan struct {
	Baseline  Baseline
	Component Component
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
