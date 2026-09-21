# Workload preview use cases

This document describes reusable deployment-preview use cases for HTTP services,
background workloads and web applications. It is product planning material;
implemented contracts are documented separately.

The central conclusion is that a deployment preview is not always "run one HTTP
container with a different image." A product plan needs distinct, explicit
workload classes, each with its own safety, readiness, identity, dependency, and
cleanup requirements.

## Decisions already made

- The first advanced workload pilot is an HTTP API with an application
  container, approved proxy sidecars and approved init containers.
- Previews use shared **non-production** external dependencies. They must never
  imply that databases, topics, caches, or third-party APIs are isolated.
- A preview must use a dedicated non-production workload identity. Envy may
  project a pre-approved Kubernetes ServiceAccount annotation, but must not copy
  a production identity or create cloud IAM resources.
- Baseline migration and initialization Jobs do not run automatically in a
  preview.
- Support remains opt-in and fail-closed. A workload is blocked until its
  profile and installation policy describe its allowed shape.

## Use-case inventory

| Workload class                           | Representative behavior                                                | Preview objective                                                         | Current direction                                                    |
| ---------------------------------------- | ---------------------------------------------------------------------- | ------------------------------------------------------------------------- | -------------------------------------------------------------------- |
| Simple HTTP service                      | One application container behind an HTTP Service                       | Validate a changed image through ordinary preview routing                 | Existing `http-small` profile                                        |
| Derived HTTP deployment                  | One deployed application container with captured runtime configuration | Reuse a real deployment's probes, settings, and approved dependencies     | Existing `deployment` profile                                        |
| Composite HTTP API                       | Application container plus explicit proxy sidecars and init containers | Validate a production-like API pod without accepting arbitrary Pod shapes | `deployment-composite` pilot                                         |
| Database initialization or migration Job | One-off schema setup, grants, seed data, or migrations                 | Ensure preview compatibility without mutating shared data unexpectedly    | Explicitly unsupported in first pilot                                |
| Long-running worker/projector            | Continuously consumes messages or projects data                        | Validate asynchronous processing with bounded, attributable side effects  | Future workload class                                                |
| Scheduled CronJob                        | Starts recurring work on a schedule                                    | Validate job configuration without repeatedly operating on shared systems | Future workload class                                                |
| On-demand batch Job                      | Runs once from an explicit user or workflow action                     | Validate an isolated, observable task with a defined completion result    | Future workload class                                                |
| Frontend/web application                 | Static or web-server deployment pointing at preview APIs               | Validate browser behavior against a composed backend preview              | Existing frontend bindings; deployment coupling needs product design |

## Requirements by workload class

### 1. Simple HTTP service

This is the lowest-complexity service shape: one application container, one
Service, ordinary HTTP routing, and platform-managed mesh injection.

**Requirements**

- An immutable image override and an HTTP Service target.
- Bounded CPU and memory requests and limits.
- Liveness/readiness behavior that can establish preview readiness.
- Namespace-local Service, routing, NetworkPolicy, ResourceQuota, and cleanup.
- No application Kubernetes API token, host access, persistent volumes, or
  unapproved Pod features.

**Success criteria**

- The preview receives traffic at its composition endpoint.
- Readiness proves the new Pod is reachable.
- Composition deletion and TTL remove all Envy-owned resources.

### 2. Derived HTTP deployment

This extends the simple service case by discovering the deployed workload rather
than reconstructing its configuration from catalog fields.

**Requirements**

- Discover exactly one ready Deployment selected by the registered Service.
- Capture source Deployment provenance, selected application container,
  immutable dependency identities, template, and contract fingerprint.
- Require review and approval of discovered configuration before it can be used.
- Copy approved ConfigMaps and Secrets into the composition namespace at the
  captured source version; never persist Secret payloads in Envy's API,
  database, logs, or artifacts.
- Require explicit replacements for namespace-sensitive public configuration.
- Fail visibly when the baseline changes incompatibly or a recorded dependency
  version can no longer be copied.

**Success criteria**

- Existing compositions retain their captured configuration despite routine
  baseline changes.
- A new approved discovery is required for contract-affecting changes.
- A source dependency rotation cannot silently change an existing preview.

### 3. Composite HTTP API

A composite HTTP workload combines an application container with database or
messaging proxies and init containers. It can require workload identity, mounted
configuration and shared external dependencies.

**Requirements**

- An explicit `deployment-composite` component profile and operator-owned
  installation policy.
- Policy fields for:
  - the application container name;
  - allowlisted regular sidecar names;
  - allowlisted init-container names;
  - the destination preview ServiceAccount name;
  - exact approved destination ServiceAccount annotations; and
  - an operator-visible warning describing shared dependencies.
- Discovery must reject all containers not on the allowlist and reject
  privileged containers, host ports, unsafe security settings, unsupported Pod
  fields, missing resource limits, or an unapproved source identity.
- Dependency discovery and immutable rewrites must scan application, sidecar,
  and init containers, including environment references and volumes.
- ResourceQuota calculation must include all regular containers and the
  effective maximum init-container request/limit, as well as rollout and mesh
  overhead.
- The resulting preview must use the composition-owned ServiceAccount with only
  the approved non-production annotation values.
- Readiness must account for the full Pod, rather than reporting success before
  an essential proxy or init container is operational.

**Explicit non-goals for the initial composite pilot**

- Arbitrary sidecars or init containers.
- Creating cloud service accounts, IAM bindings, databases, topics, or caches.
- Copying a production identity.
- Executing database initialization/migration Jobs.
- Claiming external dependency isolation.

**Success criteria**

- An allowlisted composite pod can be discovered, approved, created, observed,
  updated, and deleted on a disposable local cluster.
- Its copied dependencies are immutable and its ServiceAccount annotation is
  projected exactly as policy permits.
- An unallowlisted or unsafe Pod is rejected before preview creation.

### 4. Database initialization and migration Jobs

Backend charts frequently include Jobs that initialize a database, create
accounts/grants, apply migrations, or seed data. Treating these as ordinary
sidecars would make them run automatically for every preview and risks shared
data corruption.

**Requirements before product support**

- A first-class Job lifecycle: create, observe completion/failure, collect
  diagnostics, retry policy, timeout, cancellation, and cleanup.
- Explicit execution policy: disabled, manual approval, once per baseline
  revision, or once per composition.
- A declared target database and a safety policy that distinguishes:
  - preview-dedicated database/schema;
  - approved shared non-production database; and
  - prohibited production target.
- Idempotency and concurrency requirements for every supported Job.
- A durable record of the Job's inputs, source provenance, outcome, and
  resulting preview compatibility state.
- Clear ordering relative to HTTP services and workers.

**Product decision needed**

Choose whether Envy will ever execute migrations against shared dependencies.
The conservative default is **no**: migrations require a dedicated preview
database/schema or an externally completed prerequisite.

### 5. Long-running workers and projectors

Workers continuously consume queues, streams, or change feeds and may write
derived state. They are not reachable through HTTP readiness alone, and
duplicating them for every preview can create duplicate consumers and side
effects.

**Requirements before product support**

- A `worker` workload profile with no public HTTP endpoint requirement.
- Explicit consumer-isolation mode per dependency: disabled, named
  preview-specific consumer/group, read-only/replay, or shared consumer only
  when safe.
- A safe default of zero replicas until an operator or composition policy enables
  execution.
- Health semantics appropriate to a worker: process readiness, subscription
  readiness, lag/connection observability, and failure reporting.
- Attribution of writes, messages, and external calls to the composition.
- Idempotency, duplicate-delivery, retry, dead-letter, and cleanup rules.
- Ordering and dependency declarations for any HTTP API or projector that
  relies on the worker's output.

**Product decision needed**

Determine whether preview workers should process real shared events at all. A
safe first model is replay or a preview-specific topic/subscription, not a
second consumer of the production-like stream.

### 6. Scheduled CronJobs

CronJobs add time-based execution. Creating one per preview can multiply a
scheduled operation, and merely deleting it may leave active Jobs running.

**Requirements before product support**

- A `scheduled-job` profile that captures the CronJob template, schedule,
  concurrency policy, deadline, history limits, and Job template.
- Default suspension on preview creation.
- Explicit activation mode: never, manual single run, manual schedule enable,
  or tightly bounded preview schedule.
- A one-shot run API that records parameters, created Job identity, completion,
  logs, and result.
- Cleanup that suspends the CronJob, deletes owned active Jobs according to
  policy, and waits for termination when required.
- Guardrails for schedules with high frequency, overlapping runs, shared
  dependencies, and externally visible side effects.
- Quota accounting for concurrent Jobs and their Pod templates.

**Product decision needed**

The implemented initial mode creates a suspended CronJob and reports the
composition as suspended. Automatic activation remains blocked until Envy has a
durable execution gate: Kubernetes CronJobs do not natively enforce a maximum
execution count while the Envy controller is unavailable. A separate one-shot
run API and the durable scheduler remain future work.

### 7. On-demand batch Jobs

Some backend operations are neither migrations nor recurring schedules. They
run once in response to an explicit command, often with an image, configuration,
identity, and dependency set similar to a service.

**Requirements before product support**

- A `job` profile with explicit trigger, timeout, retry/backoff, completion,
  and cleanup semantics.
- Input validation and durable audit records.
- A user-visible result status and diagnostics without leaking Secret values.
- Composition namespace, ServiceAccount, dependency-copy, and quota behavior
  consistent with other preview workloads.
- A policy for whether a failed Job blocks the overall composition or produces a
  separately observable degraded state.

### 8. Frontend/web application

The web client is simpler to deploy than the backend, but preview correctness
depends on how it locates the composed API and how browser-facing configuration
is built or injected.

**Requirements**

- An immutable frontend build or image and a preview deployment/binding.
- Explicit public API origin, authentication redirect origin, cookie/domain,
  CORS, and Content Security Policy behavior.
- A mapping from a frontend revision to the target backend composition or
  preview endpoint.
- Browser-level verification beyond HTTP Pod readiness.
- Independent cleanup and a clear policy for sharing a frontend across multiple
  backend compositions.

## Cross-cutting product requirements

Every supported workload class should share these guarantees:

1. **Opt-in contracts:** profiles and installation policy must define the
   supported shape. Unsupported Kubernetes fields fail closed with actionable
   blockers.
2. **Immutable provenance:** preview creation uses captured source identity,
   template, dependency versions, and approved transformations.
3. **No Secret leakage:** Secret payloads remain only in Kubernetes and
   transient controller memory.
4. **Least privilege:** source reads use named, namespace-scoped `get`
   permissions; preview identity is dedicated and non-production.
5. **Explicit external effects:** shared databases, messaging, caches, and
   third-party APIs must be declared and visibly acknowledged.
6. **Correct resource accounting:** quotas include every Pod or Job resource
   that Envy creates, including init containers, injected proxies, and rollout
   overlap.
7. **Observable lifecycle:** every workload reports provisioning, readiness or
   completion, failure reason, diagnostics, and cleanup state.
8. **Deterministic cleanup:** TTL and explicit deletion remove Envy-owned
   namespaces and workload resources without deleting baseline-owned resources.
9. **Local acceptance coverage:** each new workload class needs a synthetic,
   generic disposable-cluster fixture before validation against an external
   service.

## Planning questions for the next product requirements set

1. Which workload classes are in the next release: composite HTTP only, or
   composite HTTP plus manual Jobs, workers, and suspended CronJobs?
2. Must migration Jobs require a dedicated preview database/schema, or are they
   permanently external prerequisites?
3. What messaging isolation modes can Envy offer safely, and which should be
   mandatory for workers/projectors?
4. Should a composition be considered ready only when every selected workload is
   ready/completed, or can it expose partial/degraded states?
5. Who owns provisioning and revoking cloud IAM, databases, topics, and
   subscriptions: Envy, an installation operator, or an external platform
   workflow?
6. What maximum workload count and resource budget should apply when a preview
   includes APIs, workers, and Jobs together?
7. What approval surface is required for shared dependency effects and manual
   execution of a Job or schedule?

## Current implementation status

See [Composite HTTP previews](composite-previews.md) for the generic installation
policy, native-sidecar handling, approval workflow and current boundaries.

`http-small`, derived single-container `deployment`, the narrow
`deployment-composite` HTTP pilot, and finite Kubernetes Jobs are implemented.
Jobs use image-only baseline bindings and `verification.kind: none`, so they do
not create a Service, ingress route or public endpoint. A Job receives a
durable execution ID before creation, has bounded timeout and retry settings,
reports pending/running/succeeded/failed state, and remains inspectable through
the existing component-log path. Deleting its composition cancels active work
by deleting its owned namespace. Scheduled Jobs use the same endpoint-free
contract and create a suspended CronJob; automatic execution remains fail-closed
until Envy has a durable execution gate. Workers, projectors and automatic
migrations remain future milestones.

## Delivery roadmap

The domain now records provider-neutral workload kinds (`http`, `worker`, `job`
and `scheduled-job`), per-workload execution states, bounded finite-work
settings, and dependency edges. These are additive catalog and API fields: an
existing HTTP component continues to mean `http`. The shared model rejects
cycles, requires a timeout and bounded retry count for finite work, and requires
scheduled work to forbid overlapping runs. Runtime state now has a durable
execution-identity slot for providers to populate before work begins, so the
provider milestones can recover without duplicating a run.

The Kubernetes provider executes finite Jobs and can provision suspended
CronJobs. Workers remain fail-closed until their provider lifecycle, RBAC and
synthetic acceptance coverage land. The following sequence is the implementation
order.

| Milestone | Delivery | Required acceptance |
| --- | --- | --- |
| Finite Jobs | Implemented: Kubernetes Job creation, bounded retry/timeout, completion, logs, deletion cancellation and stable execution IDs | Provider and reconciler coverage passes; live synthetic acceptance still needs a disposable cluster |
| Scheduled Jobs | Implemented foundation: suspended CronJob creation, bounded template settings, ownership and cleanup. Automatic execution requires a durable Envy-owned run gate. | Suspended creation and cleanup coverage passes; build and test the durable scheduler before enabling schedules. |
| Workers | Endpoint-free Deployments with process readiness and Pub/Sub isolation bindings | Two isolated workers process synthetic inputs, recover from restart and stop before subscription cleanup |
| Frontend lifecycle | Existing frontend bindings report backend terminal state and hosted verification | A synthetic browser path reaches its current backend; stale and destroyed bindings cannot report current |
| CI adoption | GitHub workflow handles ready, completed, failed and cancelled compositions | Create, update, closure and late-report rejection pass for endpoint-free compositions |

Automatic execution is gated by all declared dependencies and operator-provided
bindings. Envy owns its Pub/Sub isolation resources; databases and other broker
resources remain operator-provided prerequisites. Database migrations, schema
preparation and grants are explicitly outside this lifecycle. Synthetic fixtures
remain the acceptance boundary until an installation can verify cloud identity
and real dependency protocols.
