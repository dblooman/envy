// Package domain contains Envy's provider-independent catalog and lifecycle types.
package domain

import (
	"context"
	"errors"
	"strings"
	"time"
)

type Error struct {
	Code        string `json:"code"`
	Message     string `json:"message"`
	Retryable   bool   `json:"retryable"`
	Project     string `json:"project,omitempty"`
	Composition string `json:"composition,omitempty"`
}

func (e *Error) Error() string        { return e.Message }
func Validation(message string) error { return &Error{Code: "validation_error", Message: message} }
func NotFound(message string) error   { return &Error{Code: "not_found", Message: message} }

var ErrStaleObservation = errors.New("composition desired state changed")
var ErrNotLeader = errors.New("another reconciler holds the lease")

type Phase string

const (
	PhaseCreated      Phase = "created"
	PhaseProvisioning Phase = "provisioning"
	PhaseReady        Phase = "ready"
	// PhaseCompleted means every required finite workload completed successfully.
	// It is distinct from ready because a completed composition has no endpoint.
	PhaseCompleted  Phase = "completed"
	PhaseSuspended  Phase = "suspended"
	PhaseCancelled  Phase = "cancelled"
	PhaseUpdating   Phase = "updating"
	PhaseFailed     Phase = "failed"
	PhaseDestroying Phase = "destroying"
	PhaseDestroyed  Phase = "destroyed"
)

type Project struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type Component struct {
	Execution        *WorkloadExecution `json:"execution,omitempty"`
	ImagePullSecrets []string           `json:"image_pull_secrets,omitempty"`
	Profile          string             `json:"profile"`
	ReadinessPath    string             `json:"readiness_path"`
	Env              map[string]string  `json:"env,omitempty"`
	ID               string             `json:"id"`
	Project          string             `json:"project"`
	Protocol         string             `json:"protocol"`
	Port             int32              `json:"port"`
	HealthPath       string             `json:"health_path"`
	Overridable      bool               `json:"overridable"`
	Repository       string             `json:"repository,omitempty"`
}
type BaselineBinding struct {
	ServiceHost string `json:"service_host"`
	Port        int32  `json:"port"`
	Image       string `json:"image"`
}
type Baseline struct {
	PubSub       map[string]PubSubTopic     `json:"pubsub,omitempty"`
	Routing      BaselineRouting            `json:"routing"`
	Verification VerificationContract       `json:"verification"`
	ID           string                     `json:"id"`
	Project      string                     `json:"project"`
	Revision     string                     `json:"revision"`
	Endpoint     string                     `json:"endpoint"`
	Components   map[string]BaselineBinding `json:"components"`
}
type ComponentOverride struct {
	Image   string `json:"image,omitempty"`
	BuildID string `json:"build_id,omitempty"`
	Source  *Build `json:"source,omitempty"`
}
type ResourceOverride struct {
	Strategy string `json:"strategy"`
	Source   string `json:"source,omitempty"`
}
type CreateRequest struct {
	MessageIsolation         bool                         `json:"message_isolation,omitempty"`
	ExpectedPreviewRevisions map[string]int64             `json:"expected_preview_revisions,omitempty"`
	ExpectedBaselineRevision string                       `json:"expected_baseline_revision,omitempty"`
	Project                  string                       `json:"project"`
	Baseline                 string                       `json:"baseline"`
	Name                     string                       `json:"name"`
	Overrides                map[string]ComponentOverride `json:"overrides"`
	Resources                map[string]ResourceOverride  `json:"resources,omitempty"`
	TTL                      string                       `json:"ttl,omitempty"`
}
type UpdateRequest struct {
	ExpectedPreviewRevisions map[string]int64             `json:"expected_preview_revisions,omitempty"`
	ExpectedGeneration       int64                        `json:"expected_generation"`
	Overrides                map[string]ComponentOverride `json:"overrides"`
	// IdempotencyKey is supplied by the transport header and is never persisted
	// in desired state or echoed in API responses.
	IdempotencyKey string        `json:"-"`
	Plan           *ResolvedPlan `json:"-"`
}

type ComponentObservation struct {
	Source         string         `json:"source"`
	Status         string         `json:"status"`
	Image          string         `json:"image"`
	WorkloadID     string         `json:"workload_id,omitempty"`
	ExecutionID    string         `json:"execution_id,omitempty"`
	ExecutionState ExecutionState `json:"execution_state,omitempty"`
}
type Endpoint struct {
	URL   string `json:"url"`
	Ready bool   `json:"ready"`
}
type Condition struct {
	Type    string `json:"type"`
	Status  bool   `json:"status"`
	Message string `json:"message"`
}
type Operation struct {
	ID        string     `json:"id"`
	Kind      string     `json:"kind"`
	Status    string     `json:"status"`
	Error     *Error     `json:"error,omitempty"`
	Initiator *Principal `json:"initiator,omitempty"`
}
type Composition struct {
	PRPreviewID          string                          `json:"pr_preview_id,omitempty"`
	MessageIsolation     bool                            `json:"message_isolation"`
	MessageSubscriptions []MessageSubscription           `json:"message_subscriptions,omitempty"`
	PreviewProfiles      map[string]PreviewProvenance    `json:"preview_profiles,omitempty"`
	VerificationLevel    string                          `json:"verification_level,omitempty"`
	ID                   string                          `json:"id"`
	Project              string                          `json:"project"`
	Baseline             string                          `json:"baseline"`
	BaselineRevision     string                          `json:"baseline_revision"`
	Name                 string                          `json:"name"`
	Overrides            map[string]ComponentOverride    `json:"overrides"`
	Generation           int64                           `json:"generation"`
	ObservedGeneration   int64                           `json:"observed_generation"`
	Phase                Phase                           `json:"phase"`
	ExpiresAt            time.Time                       `json:"expires_at"`
	CreatedAt            time.Time                       `json:"created_at"`
	UpdatedAt            time.Time                       `json:"updated_at"`
	Components           map[string]ComponentObservation `json:"components"`
	Endpoints            map[string]Endpoint             `json:"endpoints"`
	Conditions           []Condition                     `json:"conditions"`
	LatestOperation      Operation                       `json:"latest_operation"`
	LastError            *Error                          `json:"last_error,omitempty"`
	DeletionRequested    bool                            `json:"-"`
	Runtime              RuntimeState                    `json:"-"`
}
type RuntimeState struct {
	// Executions is keyed by component. It persists provider execution identity
	// before work begins, which prevents recovery from creating a duplicate Job.
	Executions         map[string]ExecutionRef
	MessagingObserved  map[string]bool
	Plan               *ResolvedPlan
	DeletionReason     string
	ProvisionStartedAt time.Time
	OwnershipToken     string
	Workloads          map[string]WorkloadRef
	// PublishedOverrides is the last override selection whose aggregate routes
	// were durably published. It deliberately differs from Composition.Overrides
	// while an update is preparing new workloads or retiring old ones.
	PublishedOverrides map[string]ComponentOverride
	// RetiringWorkloads survives restarts after a component leaves desired state.
	// Step 2B populates and drains this inventory before component deletion.
	RetiringWorkloads    map[string]WorkloadRef
	RetirementDrainUntil *time.Time
	Workload             WorkloadRef
	RoutingActive        bool
	RoutesRemoved        bool
	DrainUntil           *time.Time
	Attempts             int
	NextAttemptAt        time.Time
}
type WorkloadSpec struct {
	DesiredComponents                                            []string
	BaselineNamespace                                            string
	MessagingEnv                                                 map[string]string
	Preview                                                      *PreviewSnapshot
	Previews                                                     map[string]PreviewSnapshot
	CompositionID, ProjectID, ComponentID, Image, OwnershipToken string
	Profile                                                      Component
	WorkloadCount                                                int
}
type WorkloadRef struct {
	Kind                                                                                    WorkloadKind
	Namespace, NamespaceUID, Deployment, DeploymentUID, Service, ServiceUID, OwnershipToken string
	DeploymentGeneration                                                                    int64
	Image                                                                                   string
	ExecutionFingerprint                                                                    string
}
type WorkloadObservation struct {
	State                      ExecutionState
	Ready, Failed              bool
	Message, Image, WorkloadID string
}

func NamespaceForID(id string) string { return "envy-" + strings.ReplaceAll(id, "_", "-") }

type RouteEntry struct {
	MessageIsolation                                     bool
	Domain                                               RouteDomain
	CompositionID, Host, DestinationHost, OwnershipToken string
	Port                                                 int32
}
type RouteSnapshot struct {
	Domains                     []RouteDomain
	MeshEntries, IngressEntries []RouteEntry
	// Ownership remains available while deleting, even after route intent is
	// removed. It prevents cleanup from trusting installation labels alone.
	OwnedCompositions map[string]string
}
type RouteObservation struct {
	Ready   bool
	Message string
}
type RuntimeProvider interface {
	Ensure(context.Context, WorkloadSpec) (WorkloadRef, error)
	Observe(context.Context, WorkloadRef) (WorkloadObservation, error)
	Delete(context.Context, WorkloadRef) error
	DeleteWorkload(context.Context, WorkloadRef) error
	WorkloadAbsent(context.Context, WorkloadRef) (bool, error)
}
type RoutingProvider interface {
	Reconcile(context.Context, RouteSnapshot) (RouteObservation, error)
}
