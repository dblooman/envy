export type Kind = "shared" | "override" | "control" | "action";
export type DiagramNode = {
  id: string;
  title: string;
  detail: string;
  kind: Kind;
  label: string;
};
export type DiagramEdge = {
  from: string;
  to: string;
  label: string;
  kind?: Kind;
  feedback?: boolean;
};
export type Diagram = {
  title: string;
  description: string;
  nodes: DiagramNode[];
  edges: DiagramEdge[];
};

const node = (
  id: string,
  title: string,
  detail: string,
  kind: Kind,
  label: string,
): DiagramNode => ({ id, title, detail, kind, label });
const edge = (
  from: string,
  to: string,
  label: string,
  kind: Kind = "control",
  feedback = false,
): DiagramEdge => ({ from, to, label, kind, feedback });

const services = [
  node(
    "gateway",
    "gateway · v1",
    "Shared entry service",
    "shared",
    "Baseline workload",
  ),
  node(
    "a",
    "service-a · v1",
    "Forwards request baggage",
    "shared",
    "Baseline workload",
  ),
  node(
    "b1",
    "service-b · v1",
    "Baseline namespace",
    "shared",
    "Baseline workload",
  ),
  node(
    "b2",
    "service-b · v2",
    "Composition namespace",
    "override",
    "Override workload",
  ),
  node(
    "data",
    "Database & cache",
    "Preview writes affect this data",
    "shared",
    "Shared dependencies",
  ),
];
const composition: Diagram = {
  title: "One application, two request paths",
  description:
    "Both URLs reuse gateway and service-a. The selected mesh selects service-b v2 only when the request carries the matching composition ID. Both service-b versions connect to the same shared data.",
  nodes: [
    node(
      "baseline",
      "Baseline URL",
      "Ingress removes baggage",
      "control",
      "Mesh ingress",
    ),
    node(
      "preview",
      "Preview URL",
      "Sets composition=cmp-123",
      "override",
      "Mesh ingress",
    ),
    ...services,
  ],
  edges: [
    edge("baseline", "gateway", "Baseline request", "shared"),
    edge("preview", "gateway", "Preview request", "override"),
    edge("gateway", "a", "Forward baggage", "shared"),
    edge("a", "b1", "No matching context", "shared"),
    edge("a", "b2", "composition=cmp-123", "override"),
    edge("b1", "data", "Read / write", "shared"),
    edge("b2", "data", "Read / write", "override"),
  ],
};

export const diagrams: Record<string, Diagram> = {
  composition,
  multi: {
    title: "Two overrides, one request path",
    description:
      "The preview starts at gateway v2, reuses service-a v1, and selects service-b v2. Applications forward composition context at each HTTP hop. The database and cache remain shared.",
    nodes: [
      node(
        "preview",
        "Preview URL",
        "Sets composition=cmp-84f1a",
        "override",
        "Mesh ingress",
      ),
      node(
        "gateway2",
        "gateway · v2",
        "Composition namespace",
        "override",
        "Override workload",
      ),
      services[1],
      services[3],
      services[4],
    ],
    edges: [
      edge("preview", "gateway2", "Preview request", "override"),
      edge("gateway2", "a", "Forward baggage", "override"),
      edge("a", "b2", "composition=cmp-84f1a", "override"),
      edge("b2", "data", "Read / write", "override"),
    ],
  },
  control: {
    title: "How Envy configures a preview",
    description:
      "The API records desired state in PostgreSQL. The reconciler reads that state and configures Kubernetes workloads and mesh routes. Application requests travel through the mesh, not through the Envy API or database.",
    nodes: [
      node(
        "cli",
        "Developer / CI",
        "Envy CLI or HTTP client",
        "action",
        "Client",
      ),
      node("agent", "Coding agent", "Envy MCP server", "action", "Client"),
      node(
        "api",
        "Envy REST API",
        "Validate and accept intent",
        "control",
        "Control plane",
      ),
      node(
        "db",
        "PostgreSQL",
        "Desired state and audit events",
        "control",
        "Control plane",
      ),
      node(
        "worker",
        "Reconciler",
        "Converge and verify readiness",
        "control",
        "Control plane",
      ),
      node(
        "pods",
        "Kubernetes",
        "Create override workloads",
        "override",
        "Data plane",
      ),
      node(
        "routes",
        "Mesh",
        "Configure ingress and routing",
        "shared",
        "Data plane",
      ),
    ],
    edges: [
      edge("cli", "api", "Authenticated HTTP"),
      edge("agent", "api", "Authenticated HTTP"),
      edge("api", "db", "Persist intent"),
      edge("db", "worker", "Read desired state"),
      edge("worker", "pods", "Reconcile workloads", "override"),
      edge("worker", "routes", "Reconcile routes", "shared"),
    ],
  },
  workflow: {
    title: "Build, verify, and iterate",
    description:
      "The agent supplies an image, creates or updates a composition, and waits for readiness before testing its URL. Failed tests lead to log inspection and another image revision. After the final test, request teardown and report the outcome.",
    nodes: [
      node(
        "build",
        "Build an image",
        "Make it available to the cluster",
        "action",
        "01 · Agent / CI",
      ),
      node(
        "create",
        "Create or update",
        "Apply the image override",
        "control",
        "02 · Envy MCP",
      ),
      node(
        "wait",
        "Wait for readiness",
        "wait_for_composition",
        "control",
        "03 · Envy MCP",
      ),
      node(
        "test",
        "Test the preview URL",
        "Run HTTP or browser checks",
        "action",
        "04 · Agent",
      ),
      node(
        "logs",
        "Inspect logs & fix",
        "get_component_logs",
        "action",
        "Retry · Agent + MCP",
      ),
      node(
        "cleanup",
        "Destroy & report",
        "destroy_composition",
        "control",
        "05 · Envy MCP + agent",
      ),
    ],
    edges: [
      edge("build", "create", "Published image"),
      edge("create", "wait", "Composition ID"),
      edge("wait", "test", "Ready URL"),
      edge("test", "logs", "Checks fail", "action"),
      edge("logs", "build", "Build a new revision", "action", true),
      edge("test", "cleanup", "Testing complete"),
    ],
  },
  "agent-summary": {
    title: "An agent’s preview loop",
    description:
      "Build an image, wait for a ready composition, test its URL, then request cleanup. If a test fails, inspect logs and repeat with a new image.",
    nodes: [
      node(
        "image",
        "Build image",
        "Agent or existing CI workflow",
        "action",
        "01 · Prepare",
      ),
      node(
        "preview",
        "Create & wait",
        "Envy API through MCP",
        "control",
        "02 · Provision",
      ),
      node(
        "test",
        "Test preview URL",
        "HTTP checks or browser tests",
        "action",
        "03 · Verify",
      ),
      node(
        "done",
        "Destroy & report",
        "Record the actual test results",
        "control",
        "04 · Finish",
      ),
    ],
    edges: [
      edge("image", "preview", "Image reference"),
      edge("preview", "test", "Ready URL"),
      edge("test", "done", "Testing complete"),
    ],
  },
  frontend: {
    title: "Connect a frontend to a backend preview",
    description:
      "Bind an exact frontend commit to a composition. The build adapter resolves the ready backend URL, passes it to the frontend build, and records the hosting URL. Browser tests call that backend through its preview ingress.",
    nodes: [
      node(
        "revision",
        "Frontend commit",
        "Exact Git revision",
        "action",
        "Source",
      ),
      node(
        "binding",
        "Frontend binding",
        "Revision → composition ID",
        "control",
        "Envy API",
      ),
      node(
        "build",
        "Frontend build",
        "Receives resolved API URL",
        "action",
        "Build adapter",
      ),
      node(
        "browser",
        "Deployed frontend",
        "Browser calls the preview API",
        "action",
        "External hosting",
      ),
      node(
        "preview",
        "Preview ingress",
        "Sets composition context",
        "override",
        "Backend composition",
      ),
      services[0],
      services[1],
      services[3],
    ],
    edges: [
      edge("revision", "binding", "Bind revision"),
      edge("binding", "build", "Resolve ready API URL"),
      edge("build", "browser", "Deploy assets"),
      edge("browser", "preview", "API request", "override"),
      edge("preview", "gateway", "Preview request", "override"),
      edge("gateway", "a", "Forward baggage", "shared"),
      edge("a", "b2", "Matching context", "override"),
    ],
  },
};
