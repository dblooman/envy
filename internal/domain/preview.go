package domain

import (
	"context"
)

// PreviewSelection identifies infrastructure, never supplies Kubernetes manifests.
type PreviewSelection struct {
	Deployment    string                       `json:"deployment,omitempty"`
	Container     string                       `json:"container,omitempty"`
	Env           map[string]string            `json:"env,omitempty"`
	ConfigMapKeys map[string]map[string]string `json:"config_map_keys,omitempty"`
}
type PreviewDependency struct {
	Kind            string `json:"kind"`
	Name            string `json:"name"`
	UID             string `json:"uid"`
	ResourceVersion string `json:"resource_version"`
}
type PreviewSource struct {
	Namespace       string `json:"namespace"`
	Deployment      string `json:"deployment"`
	UID             string `json:"uid"`
	ResourceVersion string `json:"resource_version"`
	Generation      int64  `json:"generation"`
	Container       string `json:"container"`
}

// PreviewTemplate is a provider-owned serialized template, without Secret data.
// It is internal execution state; Kubernetes Go types stay in the provider.
type PreviewSnapshot struct {
	MeshBudget   map[string]string   `json:"mesh_budget"`
	Source       PreviewSource       `json:"source"`
	Dependencies []PreviewDependency `json:"dependencies"`
	TemplateJSON string              `json:"template_json"`
	Selection    PreviewSelection    `json:"selection"`
	Contract     string              `json:"contract"`
	Revision     int64               `json:"revision"`
}

// ConnectivityFinding never includes Secret values or credential-bearing addresses.
type ConnectivityFinding struct {
	Location    string `json:"location"`
	Hostname    string `json:"hostname"`
	Message     string `json:"message"`
	Replacement string `json:"replacement,omitempty"`
}
type PreviewReport struct {
	Connectivity    []ConnectivityFinding `json:"connectivity,omitempty"`
	Source          PreviewSource         `json:"source"`
	Selection       PreviewSelection      `json:"selection"`
	Inspection      string                `json:"inspection"`
	Contract        string                `json:"contract"`
	Dependencies    []PreviewDependency   `json:"dependencies"`
	Configuration   map[string]any        `json:"configuration"`
	Blockers        []string              `json:"blockers"`
	Warnings        []string              `json:"warnings"`
	SourceReadRules []map[string]any      `json:"source_read_rules"`
	Snapshot        PreviewSnapshot       `json:"-"`
}
type PreviewApproval struct {
	Selection           PreviewSelection `json:"selection"`
	Inspection          string           `json:"inspection"`
	ExpectedRevision    int64            `json:"expected_revision"`
	ConfirmConnectivity bool             `json:"confirm_connectivity"`
}
type PreviewProfile struct {
	Project      string              `json:"project"`
	Baseline     string              `json:"baseline"`
	Component    string              `json:"component"`
	Revision     int64               `json:"revision"`
	Selection    PreviewSelection    `json:"selection"`
	SourceUID    string              `json:"source_uid"`
	Contract     string              `json:"contract"`
	Dependencies []PreviewDependency `json:"dependencies"`
}
type PreviewProvenance struct {
	Revision int64         `json:"revision"`
	Source   PreviewSource `json:"source"`
}
type PreviewDiscoverer interface {
	DiscoverPreview(context.Context, Baseline, Component, PreviewSelection) (PreviewReport, error)
}

func (c *Composition) RefreshPreviewProvenance() {
	c.PreviewProfiles = nil
	if c.Runtime.Plan == nil {
		return
	}

	for component := range c.Overrides {
		if snapshot, ok := c.Runtime.Plan.Previews[component]; ok {
			if c.PreviewProfiles == nil {
				c.PreviewProfiles = map[string]PreviewProvenance{}
			}

			c.PreviewProfiles[component] = PreviewProvenance{Revision: snapshot.Revision, Source: snapshot.Source}
		}
	}
}
