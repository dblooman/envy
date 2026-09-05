I want to design and build an open-source, API-first and MCP-first software delivery platform written primarily in Go.

The platform is NOT an AI agent platform and should not try to run or manage coding agents itself.

Instead, it should provide infrastructure that any external caller can use:

- local developers
- CLI tools
- GitHub Actions / GitLab CI
- Claude Code
- Codex
- GitHub Copilot
- Cursor / remote coding agents
- Linear agents
- internal developer portals
- other automation systems

The core purpose is to make complex software systems easy to compose into temporary, testable environments.

# 1. Product idea

Modern preview environments usually assume something like:

branch / PR
→ clone application
→ deploy frontend/backend
→ create URL

This works reasonably well for stateless frontend applications but becomes expensive and difficult for distributed backend systems.

A large system might contain:

- frontend
- mobile app
- GraphQL gateway
- REST APIs
- gRPC services
- many microservices
- PostgreSQL
- Redis
- Kafka
- Pub/Sub
- SQS
- S3
- scheduled workers
- external dependencies
- infrastructure managed using Terraform
- Kubernetes workloads
- Vercel frontends
- Railway or Render services
- cloud resources

It is wasteful to duplicate an entire staging environment merely to test one changed service.

Instead, the platform should provide **composable virtual environments**.

The fundamental model is:

Environment = Baseline + Component Overrides + Resource Overrides

For example:

baseline:
staging

overrides:
frontend:
branch: feature/search-filter

search-api:
pull_request: 123

Everything that is not overridden continues using the existing staging environment.

Conceptually:

staging:

frontend-main
|
gateway-main
|
+--> search-main
+--> booking-main
+--> user-main

composition:

frontend-feature
|
gateway-main
|
+--> search-feature
+--> booking-main
+--> user-main

Only the changed workloads need to be deployed.

The platform should dynamically route requests belonging to that composition to the overridden workloads while requests for services that have not been overridden continue to use the baseline.

This should work for arbitrary combinations.

For example:

Composition A:

- frontend PR 12
- GraphQL main
- search PR 73
- everything else staging

Composition B:

- frontend PR 20
- GraphQL PR 55
- search PR 73
- booking PR 18
- everything else staging

A developer or agent should be able to create many compositions simultaneously.

# 2. Core principles

Design the product around these principles.

## Bring your own agent

Do NOT implement an agent runtime.

The platform should expose APIs that agents can call.

An agent should be able to say:

create_composition(...)
get_composition(...)
update_composition(...)
run_validation(...)
get_logs(...)
get_endpoints(...)
destroy_composition(...)

The same capabilities should be exposed through:

- HTTP API
- Go SDK eventually
- CLI
- MCP server

## API first

Everything should be possible through the API.

The UI, if we build one later, should only be an API client.

## MCP first

The MCP interface should expose high-level semantic operations rather than Kubernetes primitives.

Bad MCP interface:

kubectl_apply()
helm_upgrade()
create_namespace()

Good MCP interface:

create_composition()
override_component()
clone_database()
get_environment_status()
get_component_logs()
run_test_suite()
destroy_composition()

Agents should not need to understand the implementation platform.

## Provider independent

Do not tightly couple the domain model to Kubernetes.

Initially Kubernetes can be the first supported runtime.

Eventually components may run on:

- Kubernetes
- Railway
- Render
- Vercel
- Cloudflare
- AWS ECS
- Google Cloud Run
- other platforms

The orchestration layer should treat these as providers.

## Open source

The core control plane and Kubernetes implementation should be fully open source and possible to self-host.

Avoid designing the MVP around proprietary SaaS dependencies.

# 3. Core domain model

I think the main domain concepts should be:

## Project

Represents one logical software platform/application.

Example:

restaurant-platform

A project contains components, resources and baseline environments.

## Component

A deployable application workload.

Examples:

frontend
graphql
search-api
booking-service
user-service

Possible metadata:

id
name
repository
default branch
runtime provider
deployment definition
health endpoint
dependency metadata
routing metadata
build metadata

## Resource

Stateful or infrastructure dependency.

Examples:

postgres
redis
kafka
pubsub
s3
elasticsearch

A resource may support strategies such as:

inherit
isolated
clone
snapshot
seed
branch
empty

## Baseline

A known working environment.

Examples:

staging
production
main
release-2026-09

A baseline maps logical components to existing deployed endpoints/workloads/resources.

Example:

baseline: staging

components:
graphql: staging GraphQL service
search: staging search service
booking: staging booking service

resources:
database: staging Postgres
redis: staging Redis

## Composition

The central abstraction.

A Composition represents a temporary virtual environment layered on top of a baseline.

Example:

composition:
name: search-filter-test

baseline: staging

components:
frontend:
source:
repository: company/frontend
ref: feature/search-filter

    search-api:
      source:
        repository: company/search
        ref: refs/pull/182/head

resources:
search-db:
strategy: clone
source: staging

Everything not explicitly overridden inherits from staging.

Composition state might include:

CREATED
PLANNING
PROVISIONING
READY
UPDATING
FAILED
DESTROYING
DESTROYED

## Component Override

Represents a different version of a component within a composition.

Possible sources:

Git branch
Git commit
GitHub PR
GitLab MR
pre-built image

For MVP, we do NOT necessarily need to build container images ourselves.

Allow the caller or CI system to supply:

image: ghcr.io/company/search:sha123

Later providers may integrate with source repositories/build systems.

## Resource Override

Represents composition-specific infrastructure.

Example:

database:
strategy: clone

or:

pubsub-topic:
strategy: isolated

## Route Context

Each composition should have a globally unique ID.

Example:

cmp_01ABCXYZ

Requests associated with a composition should carry a routing context.

Prefer standards where possible.

For HTTP use W3C baggage.

Example:

baggage: composition=cmp_01ABCXYZ

For gRPC use metadata.

Later investigate propagation through:

Kafka headers
Google Pub/Sub attributes
SQS message attributes
Temporal workflow metadata

# 4. Main technical concept: shared baseline + selective routing

The initial Kubernetes implementation should NOT duplicate the whole environment.

Assume we have:

search-api baseline
booking-api baseline
graphql baseline

Composition overrides:

search-api PR 123

Deploy:

search-api-cmp-123

Then traffic carrying:

composition=cmp-123

should resolve:

search-api
→ search-api-cmp-123

while:

booking-api
→ baseline booking-api

Normal traffic without the composition context should continue hitting the baseline.

This effectively creates a logical environment over shared infrastructure.

# 5. Kubernetes MVP

Start with Kubernetes as the first runtime provider.

Prefer NOT to build our own service mesh initially.

Investigate an architecture using:

- Kubernetes
- Istio
- Kubernetes Gateway API where useful
- W3C baggage propagation
- OpenTelemetry

The control plane should generate routing configuration.

For example:

composition cmp-123

overrides:
search-api:
deployment: search-api-cmp-123

should result in routing conceptually equivalent to:

if composition == cmp-123:
search-api -> search-api-cmp-123
else:
search-api -> search-api-baseline

Determine the cleanest way to implement this with modern Istio APIs.

Consider:

- VirtualService
- HTTPRoute / Gateway API
- EnvoyFilter only if absolutely necessary

Avoid deep dependency on deprecated Istio features.

The application must propagate routing context between services.

Use OpenTelemetry propagation wherever possible rather than requiring application teams to manually copy headers.

Document the limitations clearly.

# 6. Kubernetes architecture

I expect roughly:

API / MCP / CLI
|
v
Go Control Plane
|
+---- PostgreSQL metadata store
|
+---- Kubernetes Provider
|
+---- composition workloads
+---- services
+---- routing rules
+---- secrets/config
+---- lifecycle
+---- garbage collection

Potential Kubernetes-native objects:

Option A:
Keep the control-plane state in PostgreSQL and use ordinary Kubernetes resources.

Option B:
Create CRDs such as:

Composition
Component
Baseline

Do NOT automatically assume CRDs are the correct design.

Evaluate the trade-offs.

My initial preference is:

- control plane owns canonical state
- provider reconciles Kubernetes
- possibly introduce CRDs later

But challenge this if there is a strong architectural reason.

# 7. Control-plane architecture

Backend language: Go.

Please use idiomatic Go and keep the architecture simple.

Potential modules:

cmd/
server/
cli/
mcp/

internal/
domain/
application/
providers/
api/
persistence/
reconciliation/
routing/

pkg/
sdk/ later

Do not over-engineer hexagonal architecture, but maintain clean boundaries.

Important interfaces might include:

type RuntimeProvider interface {
Plan(...)
Apply(...)
Status(...)
Destroy(...)
}

type ResourceProvider interface {
Provision(...)
Status(...)
Destroy(...)
}

type RoutingProvider interface {
ApplyCompositionRoutes(...)
RemoveCompositionRoutes(...)
}

Potential providers:

KubernetesRuntimeProvider
IstioRoutingProvider

Later:

VercelRuntimeProvider
RailwayRuntimeProvider
NeonResourceProvider
AWSResourceProvider

# 8. Desired-state reconciliation

The platform should behave like a control plane rather than a collection of imperative scripts.

User declares:

Composition desired state

Control plane calculates:

actual state
vs
desired state

and reconciles them.

For example:

user updates search-api image

cmp-123:

old:
search-api: image sha111

desired:
search-api: image sha222

The platform updates only the relevant workload.

Do NOT destroy and recreate the entire composition.

Think carefully about:

- reconciliation loops
- idempotency
- retries
- partial failures
- eventual consistency
- optimistic locking
- operation tracking

A composition should survive the API server restarting.

# 9. API design

Create an initial REST API.

Potential resources:

POST /v1/projects
GET /v1/projects/:id

POST /v1/projects/:id/components
GET /v1/projects/:id/components

POST /v1/projects/:id/baselines

POST /v1/compositions
GET /v1/compositions/:id
PATCH /v1/compositions/:id
DELETE /v1/compositions/:id

GET /v1/compositions/:id/endpoints
GET /v1/compositions/:id/status
GET /v1/compositions/:id/events

Potential create request:

{
"project": "restaurant-platform",
"baseline": "staging",
"name": "search-filter",
"overrides": {
"search-api": {
"image": "ghcr.io/company/search:abc123"
}
}
}

Response:

{
"id": "cmp_123",
"status": "provisioning"
}

Eventually:

GET /v1/compositions/cmp_123

returns:

{
"id": "cmp_123",
"status": "ready",

"components": {
"search-api": {
"source": "override",
"status": "ready"
},

    "booking-api": {
      "source": "baseline"
    }

},

"endpoints": {
"public": "https://cmp-123.preview.example.com"
}
}

# 10. CLI

Build a CLI suitable for humans and agents.

Potential commands:

delivery project list

delivery component list

delivery composition create

delivery composition get cmp_123

delivery composition update cmp_123
--component search-api=ghcr.io/...:sha

delivery composition logs cmp_123 search-api

delivery composition destroy cmp_123

Support JSON output everywhere.

Example:

delivery composition create
--baseline staging
--override search-api=ghcr.io/foo/search:sha123
--output json

Agents should never need to scrape human-readable CLI output.

# 11. MCP server

Implement an MCP server as a first-class interface.

Initial tools:

list_projects

list_components

get_component

list_baselines

create_composition

get_composition

update_composition

wait_for_composition

destroy_composition

get_composition_endpoints

get_component_logs

Potential future tools:

run_test_suite

get_metrics

get_traces

clone_database

get_composition_events

Design tool responses so an LLM can interpret them easily.

Avoid giant payloads.

Return stable IDs and structured statuses.

# 12. GitHub integration

GitHub integration should be optional.

The platform must work without GitHub.

Eventually support:

PR opened
→ optionally create composition

PR updated
→ update relevant override

PR closed
→ destroy composition

But avoid hard-wiring:

1 PR = 1 environment

because one of the main goals is to combine arbitrary refs.

For example:

POST /compositions

{
"overrides": {
"frontend": "github://foo/frontend/pull/123",
"graphql": "github://foo/graphql/pull/98",
"search": "github://foo/search/pull/991"
}
}

The Composition is the primary object, not the PR.

# 13. Cross-repository compositions

Support compositions containing components from different repositories.

Example:

restaurant-platform

frontend:
repo company/web

graphql:
repo company/graphql

search:
repo company/search

Composition:

frontend PR 123
graphql PR 88
search branch feature/foo

Everything else staging.

This is a core use case, not an edge case.

# 14. Observability

A caller or coding agent needs to be able to inspect the environment.

Eventually support:

logs
metrics
traces
Kubernetes events
deployment failures
health checks

For MVP:

Provide component status and logs.

Potential API:

GET /compositions/:id/components/:component/logs

The provider can abstract Kubernetes logs.

Do not expose Kubernetes object names as the primary interface.

The caller should ask for:

component = search-api

not:

pod = search-api-cmp-123-7bd875c44f-whatever

# 15. Validation and test execution

Eventually a Composition should expose validations.

Example:

tests:
search-e2e:
command: ...
smoke:
command: ...

Then:

run_validation(
composition=cmp_123,
test="search-e2e"
)

Potential implementation:

Kubernetes Job
external CI provider
local execution

For MVP this can be minimal.

Design the abstraction now without building everything.

# 16. Stateful resource model

Do NOT attempt full resource cloning in the first implementation.

But design the model so it can grow.

Potential strategies:

inherit
isolated
clone
snapshot
branch
seed
empty

Example:

resources:

postgres:
strategy: clone
source: staging

redis:
strategy: inherit

pubsub:
strategy: isolated

Eventually providers might implement:

Postgres local clone
Neon database branches
Cloud SQL clones
AWS RDS snapshots
Kafka topics
Google Pub/Sub
S3 buckets

The Composition should reference logical resources rather than provider-specific identifiers.

# 17. Async systems

One of the hardest problems is maintaining composition context when requests become asynchronous.

Examples:

HTTP
→ API
→ Kafka
→ worker

or:

HTTP
→ API
→ Pub/Sub
→ worker

Research and design how composition context could travel as:

Kafka record headers
Pub/Sub attributes
SQS attributes

Conceptually:

composition=cmp_123

The downstream consumer should then participate in the same virtual environment.

Do not implement this initially unless there is a very small proof-of-concept.

Document it as an important future capability.

# 18. External ingress

There needs to be some mechanism for starting a request inside a composition.

Possible forms:

composition-specific hostname:

cmp-123.preview.example.com

which automatically injects:

composition=cmp_123

or explicit header:

baggage: composition=cmp_123

Potentially both.

An automated browser or mobile app can therefore call the composition-specific endpoint without manually managing headers.

Investigate the cleanest architecture.

# 19. Mobile testing use case

Keep this future scenario in mind.

An agent changes:

iOS application
GraphQL
Go backend
database migration

The platform creates the backend composition and returns:

{
"graphql_url": "https://cmp-123.preview.example.com/graphql"
}

The agent then boots an iOS simulator with:

GRAPHQL_ENDPOINT=<composition URL>

and tests the entire feature.

The platform itself does NOT need to operate the simulator.

Its responsibility is to give the external agent a stable, isolated system under test.

# 20. Security

Consider from the beginning:

tenant/project isolation
authentication
RBAC eventually
secret handling
network isolation
sandbox environments
resource quotas
maximum composition lifetime
automatic cleanup

Composition workloads must not automatically receive unrestricted production credentials.

For MVP assume:

single trusted Kubernetes cluster
single organisation
development/staging only

But avoid architectural decisions that make multi-tenancy impossible.

# 21. Composition TTL

Every Composition should support expiry.

Example:

ttl: 8h

The control plane should garbage collect expired compositions.

This is important because agents may create many temporary environments and fail to destroy them.

# 22. Cost awareness

Eventually provide a cost/resource view.

At minimum track:

created workloads
CPU/memory requests
created resources
lifetime

One of the product advantages should be that selective overrides are cheaper than cloning whole environments.

Do not build detailed billing initially.

# 23. Developer experience

The basic happy path should become:

delivery composition create
--baseline staging
--override search-api=image:abc123

Wait until READY.

Then:

delivery composition endpoint cmp_123

returns something usable.

No Kubernetes commands should be necessary.

The end user should never need to understand:

namespaces
Istio VirtualServices
Services
Deployments
Envoy
routing configuration

unless debugging the platform itself.

# 24. Suggested MVP scope

Do NOT try to build all of the above initially.

I want the first meaningful MVP to prove one thing:

**A user can create a virtual environment containing one overridden service while every other service continues using staging.**

Build a demo system containing:

gateway
service-a
service-b

Baseline versions:

gateway v1
service-a v1
service-b v1

Create:

service-b v2

The baseline flow should return:

gateway-v1
service-a-v1
service-b-v1

Create composition:

baseline = staging
override service-b = v2

Requests through the composition should return:

gateway-v1
service-a-v1
service-b-v2

Normal baseline traffic should still return:

gateway-v1
service-a-v1
service-b-v1

This proves the key concept.

# 25. MVP architecture constraints

For the initial proof of concept use:

Go
PostgreSQL if persistent storage is actually necessary
Kubernetes
Istio
Docker
OpenTelemetry propagation

Prefer:

kind or k3d

for local development.

The entire demo should be runnable locally.

Potential setup:

make dev

which creates:

local Kubernetes cluster
Istio
demo services
baseline
control plane

Then:

delivery composition create ...

should provision the composition.

# 26. Suggested implementation phases

Please refine these phases rather than blindly following them.

## Phase 0 — Architecture

Produce:

architecture.md

domain-model.md

api.md

routing.md

ADR documents for major decisions.

Decide:

- composition representation
- reconciliation model
- provider interfaces
- Istio routing strategy
- ingress strategy
- persistence strategy
- how routing context propagates

Do this before writing large amounts of code.

## Phase 1 — Demo services

Create three tiny Go HTTP services.

gateway
service-a
service-b

Each response should expose:

service name
version
composition context observed

Make context propagation visible.

## Phase 2 — Kubernetes baseline

Deploy:

gateway v1
service-a v1
service-b v1

Install Istio.

Demonstrate normal calls.

## Phase 3 — Manual routing experiment

Before building the platform, manually deploy:

service-b v2

and prove Istio can route:

composition=foo
→ service-b-v2

while other traffic goes to service-b-v1.

This is an important technical spike.

Do not build the control plane until this works.

## Phase 4 — Go control plane

Implement:

projects
components
baselines
compositions

Persist state.

Create a reconciliation worker.

## Phase 5 — Kubernetes provider

Control plane should deploy overridden workloads automatically.

## Phase 6 — Routing provider

Control plane should generate/remove routing configuration automatically.

## Phase 7 — CLI

Create and inspect compositions.

## Phase 8 — MCP

Expose the same high-level API as MCP tools.

## Phase 9 — Lifecycle

Add:

updates
TTL
garbage collection
health
events

# 27. Repository structure

Please propose a monorepo structure.

Something approximately like:

/
cmd/
server/
delivery/
mcp/

internal/
api/
domain/
application/
persistence/
reconciler/
providers/
kubernetes/
istio/

demo/
gateway/
service-a/
service-b/

deploy/
helm/
local/

docs/
architecture.md
domain-model.md
routing.md
api.md
adr/

examples/

go.mod
Makefile

Improve this if needed.

# 28. Engineering principles

Use:

Go latest stable version
context.Context correctly
structured logging
OpenTelemetry
clear error wrapping
small interfaces
dependency injection through constructors
table-driven tests
integration tests for Kubernetes functionality

Avoid:

large framework dependencies
magic
overly generic abstractions
premature plugin systems
massive interfaces
reflection-heavy dependency injection
inventing a new workflow language
building our own service mesh
building an agent framework

Prefer standard library where practical.

# 29. Testing strategy

I want strong automated testing because the platform itself will eventually be used by agents.

Include:

unit tests
provider contract tests
API tests
Kubernetes integration tests
routing integration tests

The critical integration test should:

1. deploy baseline
2. verify service-b v1
3. create composition
4. deploy service-b v2
5. send request using composition context
6. verify service-b v2
7. send normal request
8. verify service-b v1
9. destroy composition
10. verify cleanup

This test should be runnable against a local kind cluster.

# 30. Future roadmap

Do not implement these initially, but make sure the architecture can evolve toward:

multiple overridden services
cross-repository changes
GitHub PR integration
GitLab integration
Vercel provider
Railway provider
Cloudflare provider
database branching
Neon support
Terraform / OpenTofu integration
AWS resources
GCP resources
Kafka context routing
Pub/Sub context routing
SQS context routing
test execution
Playwright
mobile testing integration
observability
traffic replay
shadow traffic
production-like datasets
PII sanitisation
composition templates
UI / developer portal
multi-cluster
multi-tenancy
RBAC
policy engine
cost controls

# 31. Important architectural question

Please think particularly deeply about where the product's responsibility stops.

The product should NOT become:

a CI server
a source-control platform
a coding agent platform
a Kubernetes replacement
a service mesh
a Terraform replacement
an observability platform

It should orchestrate and compose these systems.

Its core value is:

**Given a desired logical version of a distributed software system, construct that version cheaply from a shared baseline plus temporary overrides, expose it through a stable interface, and clean it up afterwards.**

# 32. First task

Do NOT start by implementing everything.

Start by acting as a senior staff engineer / architect.

Produce:

1. A concise product definition.
2. The core domain model.
3. A system architecture diagram using Mermaid.
4. A request-routing architecture diagram.
5. The proposed Go package structure.
6. The Kubernetes/Istio implementation strategy.
7. The desired-state reconciliation design.
8. The REST API design.
9. The MCP interface.
10. The local-development architecture.
11. Key technical risks and unknowns.
12. ADRs for the most important architectural decisions.
13. A sequenced implementation plan broken into small milestones.
14. Explicitly identify what we should NOT build in the MVP.

Then identify the smallest possible end-to-end vertical slice that proves the architecture.

After producing the plan, start implementing only that vertical slice.

The first success criterion should be:

**Using one command or API call, create a composition that overrides service-b with v2 while gateway and service-a continue using the shared baseline, then demonstrate through an automated integration test that composition traffic sees service-b-v2 while baseline traffic continues seeing service-b-v1.**

Optimise the architecture around making that capability extremely reliable and simple.
