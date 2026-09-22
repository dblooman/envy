// Package protocol defines the observable request path used by the demo and its verifier.
package protocol

type Hop struct {
	Service               string `json:"service"`
	Version               string `json:"version"`
	Composition           string `json:"composition"`
	WorkloadID            string `json:"workload_id"`
	DeploymentComposition string `json:"deployment_composition"`
}

type Offer struct {
	Name        string `json:"name"`
	Price       string `json:"price"`
	Description string `json:"description"`
	Service     string `json:"service"`
	Version     string `json:"version"`
}

type Response struct {
	Chain []Hop  `json:"chain"`
	Offer *Offer `json:"offer,omitempty"`
}
