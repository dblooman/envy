export type Phase =
  | "created"
  | "provisioning"
  | "updating"
  | "ready"
  | "failed"
  | "destroying"
  | "destroyed";

export interface ApiError {
  code: string;
  message: string;
  retryable?: boolean;
  project?: string;
  composition?: string;
}

export interface Condition {
  type: string;
  status: boolean;
  message: string;
}

export interface Operation {
  id: string;
  kind: "create" | "update" | "destroy";
  status: string;
  error?: ApiError;
  initiator?: Principal;
}

export interface Principal {
  kind: "human" | "service" | "shared" | "anonymous" | "system" | "unknown";
  id: string;
  display_name?: string;
  email?: string;
}
export interface Session {
  principal: Principal;
  auth_mode: "token" | "proxy" | "none" | "dev" | "password" | "google";
  channel: string;
  capabilities: string[];
}
export interface Installation {
  id: string;
  version: string;
  auth_mode: string;
  default_ttl: string;
  max_ttl: string;
  max_compositions: number;
  audit_retention?: string;
}
export interface Activity {
  id: string;
  occurred_at: string;
  actor: Principal;
  channel: string;
  task?: string;
  action: string;
  outcome: string;
  project?: string;
  resource_type: string;
  resource_id: string;
  composition?: string;
  operation?: string;
  generation_from?: number;
  generation_to?: number;
  changes?: unknown;
}
export interface CompositionRevision {
  composition: string;
  project: string;
  generation: number;
  baseline: string;
  baseline_revision: string;
  overrides: Record<string, ComponentOverride>;
  created_at: string;
  actor: Principal;
  channel: string;
  operation?: string;
}

export interface ComponentOverride {
  image?: string;
  build_id?: string;
  source?: PublishedBuild;
}

export interface ComponentObservation {
  source: "baseline" | "override";
  status: string;
  image: string;
  workload_id?: string;
}

export interface Endpoint {
  url: string;
  ready: boolean;
}

export interface Endpoints {
  public: Endpoint;
}

export interface CompositionStatus {
  verification_level?: "none" | "reachability" | "routing";
  id: string;
  phase: Phase;
  generation: number;
  observed_generation: number;
  conditions: Condition[];
  latest_operation: Operation;
  last_error?: ApiError;
}

export interface MessageSubscription {
  instance?: string;
  name: string;
  topic: string;
  component: string;
  subscription_env: string;
  filter: string;
  retention: string;
  expires_at: string;
  ready: boolean;
  backlog_may_be_lost?: boolean;
}
export interface PubSubTopic {
  topic: string;
  publishers: string[];
  consumers: Record<
    string,
    {
      subscription: string;
      component: string;
      subscription_env: string;
      filter?: string;
    }
  >;
}
export interface Composition extends CompositionStatus {
  pr_preview_id?: string;
  message_isolation?: boolean;
  message_subscriptions?: MessageSubscription[];
  project: string;
  baseline: string;
  baseline_revision: string;
  name: string;
  overrides: Record<string, ComponentOverride>;
  expires_at: string;
  created_at: string;
  updated_at: string;
  components: Record<string, ComponentObservation>;
  endpoints: Endpoints;
}

export interface Project {
  id: string;
  name: string;
}

export interface Component {
  image_pull_secrets?: string[];
  profile?: "http-small" | "deployment" | "deployment-composite";
  readiness_path?: string;
  env?: Record<string, string>;
  id: string;
  project: string;
  protocol: string;
  port: number;
  health_path?: string;
  overridable?: boolean;
  repository?: string;
}

export interface BaselineComponent {
  service_host: string;
  port: number;
  image: string;
}

export interface Baseline {
  pubsub?: Record<string, PubSubTopic>;
  routing?: {
    namespace: string;
    gateway: string;
    gateway_namespace?: string;
    gateway_section_name?: string;
    entry_component: string;
  };
  verification?: {
    kind: "envy-chain" | "http";
    chain?: string[];
    path?: string;
    expected_status?: number;
  };
  id: string;
  project: string;
  revision: string;
  endpoint: string;
  components: Record<string, BaselineComponent>;
}

export interface PageResponse<T> {
  items: T[];
  next_cursor?: string;
}

export interface CreateCompositionRequest {
  expected_preview_revisions?: Record<string, number>;
  message_isolation?: boolean;
  project: string;
  baseline: string;
  name: string;
  overrides: Record<string, ComponentOverride>;
  ttl?: string;
  resources?: Record<string, never>;
}

export interface UpdateCompositionRequest {
  expected_generation: number;
  overrides: Record<string, ComponentOverride>;
}

export interface ComponentLogStream {
  pod: string;
  workload_id: string;
  container: string;
  text: string;
  truncated: boolean;
  error?: ApiError;
}

export interface ComponentLogs {
  id: string;
  project: string;
  component: string;
  source: "override" | "shared-baseline";
  composition_filtered: false;
  message: string;
  streams: ComponentLogStream[];
  truncated: boolean;
  partial: boolean;
}

export interface LifecycleEvent {
  id: string;
  composition: string;
  project: string;
  generation: number;
  type: string;
  phase: Phase;
  occurred_at: string;
  operation: Operation;
  conditions: Condition[];
  error?: ApiError;
}

export interface FrontendBindingView {
  binding: {
    project: string;
    frontend: string;
    revision: string;
    composition: string;
    repository: string;
    version: number;
    url?: string;
    check?: {
      composition_generation: number;
      status: "passed" | "failed";
      message: string;
      reported_at: string;
    };
    created_at: string;
    updated_at: string;
  };
  composition_phase: string;
  composition_generation: number;
  expires_at: string;
  ready: boolean;
  verification_level: string;
  check_state: "not_reported" | "current" | "stale";
}
export interface FrontendResolution {
  project: string;
  frontend: string;
  revision: string;
  composition: string;
  composition_generation: number;
  binding_version: number;
  api_url: string;
  expires_at: string;
  verification_level: string;
}

export interface SourceRepository {
  project: string;
  id: string;
  github_repository: string;
  installation_id: number;
  enabled: boolean;
  images: Record<string, string>;
}
export interface PublishedBuild {
  id: string;
  project: string;
  repository: string;
  github_repository: string;
  component: string;
  revision: string;
  image: string;
  run_id: string;
  run_url: string;
  attempt: number;
  built_at: string;
}
export interface GitCommit {
  sha: string;
  message: string;
}
export interface GitBranch {
  name: string;
  sha: string;
}
export interface GitPage<T> {
  items: T[];
  page: number;
  has_more: boolean;
}
export interface RevisionResolution {
  repository: SourceRepository;
  commit: GitCommit;
  builds: PublishedBuild[];
  next_cursor?: string;
  ci_url: string;
}

export interface RecipeFrontend {
  name: string;
  revision: string;
  repository: string;
}
export interface Recipe {
  message_isolation?: boolean;
  api_version: "envy/recipe-v1";
  project: string;
  baseline: string;
  baseline_revision: string;
  ttl: string;
  overrides: Record<string, ComponentOverride>;
  frontends: RecipeFrontend[];
}
export interface RecreateRecipeResult {
  composition: Composition;
  bindings: FrontendBindingView[];
  binding_errors: string[];
}

export interface CatalogManifest {
  api_version: "envy/v1";
  project: Project;
  components: Component[];
  baseline: Baseline;
}
export interface CatalogReport {
  configuration: CatalogManifest;
  checks: Condition[];
  warnings: string[];
  applied: boolean;
}
export interface PreviewSelection {
  deployment?: string;
  container?: string;
  env?: Record<string, string>;
  config_map_keys?: Record<string, Record<string, string>>;
}
export interface PreviewDependency {
  kind: string;
  name: string;
  uid: string;
  resource_version: string;
}
export interface PreviewProfile {
  project: string;
  baseline: string;
  component: string;
  revision: number;
  selection: PreviewSelection;
  source_uid: string;
  contract: string;
  dependencies: PreviewDependency[];
}
export interface PreviewReport {
  composite_policy_key?: string;
  composite_policy?: {
    revision: number;
    application_container: string;
    sidecars?: string[];
    init_containers?: string[];
    native_sidecars?: string[];
    source_service_account: string;
    service_account: string;
    service_account_annotations?: Record<string, string>;
    shared_dependencies: string[];
    max_pod_cpu: string;
    max_pod_memory: string;
  };
  source: {
    namespace: string;
    deployment: string;
    uid: string;
    resource_version: string;
    generation: number;
    container: string;
  };
  selection: PreviewSelection;
  inspection: string;
  contract: string;
  dependencies: PreviewDependency[];
  configuration: Record<string, unknown>;
  blockers: string[];
  warnings: string[];
  source_read_rules: Record<string, unknown>[];
  connectivity?: {
    location: string;
    hostname: string;
    message: string;
    replacement?: string;
  }[];
}
export interface PreviewApproval {
  selection: PreviewSelection;
  inspection: string;
  expected_revision: number;
  confirm_connectivity: boolean;
}
