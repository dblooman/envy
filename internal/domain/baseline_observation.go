package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"time"
)

// BaselineObservation describes only supported, read-only execution identity.
// It is not a snapshot of application configuration or external dependencies.
type BaselineObservation struct {
	Installation string                                  `json:"installation"`
	Project      string                                  `json:"project"`
	Baseline     string                                  `json:"baseline"`
	State        string                                  `json:"state"`
	Fingerprint  string                                  `json:"fingerprint,omitempty"`
	ObservedAt   time.Time                               `json:"observed_at"`
	Components   map[string]BaselineComponentObservation `json:"components"`
	Error        *Error                                  `json:"error,omitempty"`
}

type BaselineServicePort struct {
	Name       string `json:"name,omitempty"`
	Protocol   string `json:"protocol"`
	Port       int32  `json:"port"`
	TargetPort string `json:"target_port"`
}

type BaselineComponentObservation struct {
	ServiceUID           string                `json:"service_uid,omitempty"`
	ServiceSelector      map[string]string     `json:"service_selector,omitempty"`
	ServicePorts         []BaselineServicePort `json:"service_ports,omitempty"`
	ExecutionState       string                `json:"execution_state"`
	DeploymentUID        string                `json:"deployment_uid,omitempty"`
	DeploymentGeneration int64                 `json:"deployment_generation,omitempty"`
	TemplateFingerprint  string                `json:"template_fingerprint,omitempty"`
	DeclaredImages       map[string]string     `json:"declared_images,omitempty"`
	ActualImageIDs       map[string][]string   `json:"actual_image_ids,omitempty"`
	ImageIdentity        string                `json:"image_identity"`
}

// CalculateFingerprint excludes observation time, Deployment generation, Pod
// count and status. A replica-only rollout therefore cannot invalidate proof.
func (o *BaselineObservation) CalculateFingerprint() {
	type execution struct {
		ServiceUID          string
		ServiceSelector     map[string]string
		ServicePorts        []BaselineServicePort
		ExecutionState      string
		DeploymentUID       string
		TemplateFingerprint string
		DeclaredImages      map[string]string
		ActualImageIDs      map[string][]string
	}
	components := map[string]execution{}
	for name, value := range o.Components {
		ports := append([]BaselineServicePort(nil), value.ServicePorts...)
		sort.Slice(ports, func(i, j int) bool {
			if ports[i].Port != ports[j].Port {
				return ports[i].Port < ports[j].Port
			}

			return ports[i].Name < ports[j].Name
		})
		components[name] = execution{value.ServiceUID, value.ServiceSelector, ports, value.ExecutionState, value.DeploymentUID, value.TemplateFingerprint, value.DeclaredImages, value.ActualImageIDs}
	}

	body, _ := json.Marshal(struct {
		Installation string
		Project      string
		Baseline     string
		Components   map[string]execution
	}{o.Installation, o.Project, o.Baseline, components})
	sum := sha256.Sum256(body)
	o.Fingerprint = hex.EncodeToString(sum[:])
}
