export type Phase =
  | 'created'
  | 'provisioning'
  | 'updating'
  | 'ready'
  | 'failed'
  | 'destroying'
  | 'destroyed'

export interface ApiError {
  code: string
  message: string
  retryable?: boolean
  project?: string
  composition?: string
}

export interface Condition {
  type: string
  status: boolean
  message: string
}

export interface Operation {
  id: string
  kind: 'create' | 'update' | 'destroy'
  status: string
  error?: ApiError
}

export interface ComponentOverride {
  image: string
}

export interface ComponentObservation {
  source: 'baseline' | 'override'
  status: string
  image: string
  workload_id?: string
}

export interface Endpoint {
  url: string
  ready: boolean
}

export interface Endpoints {
  public: Endpoint
}

export interface CompositionStatus {
  id: string
  phase: Phase
  generation: number
  observed_generation: number
  conditions: Condition[]
  latest_operation: Operation
  last_error?: ApiError
}

export interface Composition extends CompositionStatus {
  project: string
  baseline: string
  baseline_revision: string
  name: string
  overrides: Record<string, ComponentOverride>
  expires_at: string
  created_at: string
  updated_at: string
  components: Record<string, ComponentObservation>
  endpoints: Endpoints
}

export interface Project {
  id: string
  name: string
}

export interface Component {
  id: string
  project: string
  protocol: string
  port: number
  health_path?: string
  overridable?: boolean
  repository?: string
}

export interface BaselineComponent {
  service_host: string
  port: number
  image: string
}

export interface Baseline {
  id: string
  project: string
  revision: string
  endpoint: string
  components: Record<string, BaselineComponent>
}

export interface PageResponse<T> {
  items: T[]
  next_cursor?: string
}

export interface CreateCompositionRequest {
  project: string
  baseline: string
  name: string
  overrides: Record<string, ComponentOverride>
  ttl?: string
  resources?: Record<string, never>
}

export interface UpdateCompositionRequest {
  expected_generation: number
  overrides: Record<string, ComponentOverride>
}
