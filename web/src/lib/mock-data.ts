import { Composition, Project, Baseline, Component } from "../types/api";

export const MOCK_PROJECTS: Project[] = [
  { id: "demo", name: "Demo Microservices" },
];

export const MOCK_BASELINES: Baseline[] = [
  {
    id: "staging",
    project: "demo",
    revision: "rev-20260905-1a",
    endpoint: "http://baseline.envy.localhost:8080",
    components: {
      gateway: {
        service_host: "gateway.staging.svc.cluster.local",
        port: 8080,
        image: "envy/gateway:v1",
      },
      "service-a": {
        service_host: "service-a.staging.svc.cluster.local",
        port: 8080,
        image: "envy/service-a:v1",
      },
      "service-b": {
        service_host: "service-b.staging.svc.cluster.local",
        port: 8080,
        image: "envy/service-b:v1",
      },
    },
  },
];

export const MOCK_COMPONENTS: Component[] = [
  {
    id: "gateway",
    project: "demo",
    protocol: "http",
    port: 8080,
    health_path: "/healthz",
    overridable: true,
    repository: "github.com/dblooman/envy-demo-gateway",
  },
  {
    id: "service-a",
    project: "demo",
    protocol: "http",
    port: 8080,
    health_path: "/healthz",
    overridable: true,
    repository: "github.com/dblooman/envy-demo-service-a",
  },
  {
    id: "service-b",
    project: "demo",
    protocol: "http",
    port: 8080,
    health_path: "/healthz",
    overridable: true,
    repository: "github.com/dblooman/envy-demo-service-b",
  },
];

const now = new Date();
const in7Hours = new Date(now.getTime() + 7 * 3600 * 1000).toISOString();
const in5Hours = new Date(now.getTime() + 5 * 3600 * 1000).toISOString();
const in2Hours = new Date(now.getTime() + 2 * 3600 * 1000).toISOString();

export const INITIAL_MOCK_COMPOSITIONS: Composition[] = [
  {
    id: "01999999-0000-7000-8000-000000000001",
    project: "demo",
    baseline: "staging",
    baseline_revision: "rev-20260905-1a",
    name: "feature-pricing-v2",
    generation: 2,
    observed_generation: 2,
    phase: "ready",
    overrides: {
      "service-b": { image: "envy/service-b:v2" },
    },
    components: {
      gateway: {
        source: "baseline",
        status: "healthy",
        image: "envy/gateway:v1",
        workload_id: "gateway-v1-baseline",
      },
      "service-a": {
        source: "baseline",
        status: "healthy",
        image: "envy/service-a:v1",
        workload_id: "service-a-v1-baseline",
      },
      "service-b": {
        source: "override",
        status: "healthy",
        image: "envy/service-b:v2",
        workload_id: "cmp-01999999-0000-service-b",
      },
    },
    conditions: [
      {
        type: "WorkloadsReady",
        status: true,
        message: "All override pods running and healthy",
      },
      {
        type: "RoutesConfigured",
        status: true,
        message: "VirtualService and Envoy baggage routing active",
      },
      {
        type: "RouteVerified",
        status: true,
        message: "Synthetic probe verified through ingress",
      },
    ],
    latest_operation: {
      id: "op-01999999-0001",
      kind: "update",
      status: "completed",
    },
    endpoints: {
      public: {
        url: "http://cmp-01999999-0000-7000-8000-000000000001.envy.localhost:8080",
        ready: true,
      },
    },
    created_at: new Date(now.getTime() - 3600 * 1000).toISOString(),
    updated_at: new Date(now.getTime() - 600 * 1000).toISOString(),
    expires_at: in7Hours,
  },
  {
    id: "01999999-0000-7000-8000-000000000002",
    project: "demo",
    baseline: "staging",
    baseline_revision: "rev-20260905-1a",
    name: "cart-checkout-v3",
    generation: 1,
    observed_generation: 0,
    phase: "provisioning",
    overrides: {
      "service-b": { image: "envy/service-b:v3" },
    },
    components: {
      gateway: {
        source: "baseline",
        status: "healthy",
        image: "envy/gateway:v1",
        workload_id: "gateway-v1-baseline",
      },
      "service-a": {
        source: "baseline",
        status: "healthy",
        image: "envy/service-a:v1",
        workload_id: "service-a-v1-baseline",
      },
      "service-b": {
        source: "override",
        status: "container_creating",
        image: "envy/service-b:v3",
        workload_id: "cmp-01999999-0002-service-b",
      },
    },
    conditions: [
      {
        type: "WorkloadsReady",
        status: false,
        message: "Pulling image envy/service-b:v3",
      },
      {
        type: "RoutesConfigured",
        status: true,
        message: "Mesh routing rules staged",
      },
      {
        type: "RouteVerified",
        status: false,
        message: "Awaiting workload readiness probe",
      },
    ],
    latest_operation: {
      id: "op-01999999-0002",
      kind: "create",
      status: "running",
    },
    endpoints: {
      public: {
        url: "http://cmp-01999999-0000-7000-8000-000000000002.envy.localhost:8080",
        ready: false,
      },
    },
    created_at: new Date(now.getTime() - 120 * 1000).toISOString(),
    updated_at: new Date(now.getTime() - 120 * 1000).toISOString(),
    expires_at: in5Hours,
  },
  {
    id: "01999999-0000-7000-8000-000000000003",
    project: "demo",
    baseline: "staging",
    baseline_revision: "rev-20260905-1a",
    name: "quick-debug-hotfix",
    generation: 3,
    observed_generation: 3,
    phase: "ready",
    overrides: {
      "service-b": { image: "envy/service-b:v3" },
    },
    components: {
      gateway: {
        source: "baseline",
        status: "healthy",
        image: "envy/gateway:v1",
        workload_id: "gateway-v1-baseline",
      },
      "service-a": {
        source: "baseline",
        status: "healthy",
        image: "envy/service-a:v1",
        workload_id: "service-a-v1-baseline",
      },
      "service-b": {
        source: "override",
        status: "healthy",
        image: "envy/service-b:v3",
        workload_id: "cmp-01999999-0003-service-b",
      },
    },
    conditions: [
      { type: "WorkloadsReady", status: true, message: "Deployment ready" },
      {
        type: "RoutesConfigured",
        status: true,
        message: "VirtualService updated",
      },
      {
        type: "RouteVerified",
        status: true,
        message: "Routing verification 200 OK",
      },
    ],
    latest_operation: {
      id: "op-01999999-0003",
      kind: "update",
      status: "completed",
    },
    endpoints: {
      public: {
        url: "http://cmp-01999999-0000-7000-8000-000000000003.envy.localhost:8080",
        ready: true,
      },
    },
    created_at: new Date(now.getTime() - 7200 * 1000).toISOString(),
    updated_at: new Date(now.getTime() - 1800 * 1000).toISOString(),
    expires_at: in2Hours,
  },
];

// Explicit scenarios for exploring lifecycle semantics without a cluster.
INITIAL_MOCK_COMPOSITIONS.push(
  ...(
    [
      "updating",
      "failed",
      "destroyed",
      "completed",
      "suspended",
      "cancelled",
    ] as const
  ).map((phase, index): Composition => ({
    ...INITIAL_MOCK_COMPOSITIONS[0],
    id: `simulated-${phase}`,
    name: `Simulated ${phase} preview`,
    phase,
    generation: 2,
    observed_generation: phase === "updating" ? 1 : 2,
    endpoints: { public: { url: "", ready: false } },
    verification_level: "none",
    latest_operation: {
      id: `simulated-operation-${index}`,
      kind: "update",
      status: phase === "failed" ? "failed" : "succeeded",
    },
    last_error:
      phase === "failed"
        ? {
            code: "simulated_startup_failure",
            message: "Simulated: pricing did not pass its startup check.",
          }
        : undefined,
    components: Object.fromEntries(
      Object.entries(INITIAL_MOCK_COMPOSITIONS[0].components).map(
        ([name, c]) => [
          name,
          {
            ...c,
            status:
              phase === "failed" && c.source === "override"
                ? "failed"
                : c.status,
            execution_state:
              phase === "completed"
                ? "succeeded"
                : phase === "suspended"
                  ? "suspended"
                  : phase === "cancelled"
                    ? "cancelled"
                    : undefined,
          },
        ],
      ),
    ),
  })),
);
