# PRD 06: Reliability, scale and installation bundles

Status: proposed. Priority: operational foundation for fleet support.
Contract: [Shared boundaries](product-boundaries.md).
Evidence baseline: [reviewed commit](README.md#evidence-baseline).

## Problem, users and current state

Operators need measured capacity, diagnosable recovery and clear installation
ownership. A successful small preview does not establish fleet-scale throughput,
high availability or safe replacement of supporting infrastructure.

Implemented evidence:

- [Reconciliation queue](../../internal/reconciler/queue.go) and
  [tests](../../internal/reconciler/queue_test.go) already support independent
  baseline work, recovery scans and shutdown. [Lease implementation](../../internal/persistence/postgres/lease.go)
  protects the single active controller.
- [Kubernetes watches](../../internal/providers/kubernetes/watch.go) feed cached
  observations; guarded live ownership checks remain necessary for mutation.
- [Mesh capacity acceptance](../../tests/e2e/mesh_capacity_test.go) creates twenty
  previews and verifies traffic and cleanup. It is not a published scale envelope.
- The [operator chart](../../deploy/helm/envy/Chart.yaml) and
  [quickstart lifecycle](../../deploy/helm/envy-quickstart/README.md) already exist.
  The quickstart bundles a pinned mesh, persistent database and sample application
  for a dedicated evaluation cluster; it is not a general production installer.

Existing [operations guidance](../operations.md) describes backup/restore and
uninstall. The controller lacks a dedicated operational metrics contract; recovery
and supported capacity need systematic qualification, not a reconciler rewrite.

## Milestones and journeys

**M1: observable recovery.** Add bounded controller metrics and runnable recovery
scenarios. An operator can tell whether work is queued, backing off, blocked on an
API or stuck in cleanup, then follow the corresponding runbook.

**M2: measured capacity.** Produce repeatable reports for increasing preview/route
load using declared hardware, mesh and resource settings. Keep current product
limits until a separate reviewed change is justified by those reports.

**M3: replaceable non-production reference bundles.** Package an optional baseline
of existing supported components with a documented bring-your-own path and tested
replacement/retention procedures. Keep this distinct from the local quickstart.

## Requirements and interfaces

- **REL-01 — M1:** Publish controller queue delay/depth, reconcile outcomes and
  duration, provider request failures/throttling, route convergence, cleanup age,
  leadership state and observation freshness. Use bounded labels; do not put
  composition IDs, credentials or unbounded error strings into metric labels.
  Detailed per-composition evidence remains in the existing authorized API.
- **REL-02 — M1:** Test controller restart, lost notifications, stale caches, database
  loss, leadership transitions and temporary Kubernetes/mesh unavailability.
  Losing authority stops new mutations; recovery reuses owned identities and does
  not duplicate Jobs, overwrite another controller or report unobserved cleanup.
- **REL-03 — M1:** Provide tested upgrade, backup/restore and uninstall runbooks.
  Preserve installation identity and secrets ownership. Restore must reconcile
  database state with potentially newer resources through explicit operator review;
  missing restored rows never authorize orphan deletion or automatic recreation of
  previously cancelled finite work. Helm rollback is not database rollback.
- **REL-04 — M2:** Benchmark at one and twenty active previews, then configurable
  higher composition counts using explicitly raised test-installation capacity.
  Keep at most the current three selected components per composition. Record p50/
  p95 time to ready, queue delay, route updates/size, API requests, controller CPU/
  memory and cleanup latency, including create/update/delete bursts and failures.
  Publish environment details and raw results; set no unsupported universal latency
  promise from a single machine or mesh.
- **REL-05 — M1/M2:** Bound per-domain work and retries so a failing baseline does
  not starve healthy domains. Preserve aggregate-route serialization where shared
  ownership requires it. Qualify any sharding or concurrency change with ownership
  and leadership tests before enabling it; fleet extends this isolation per target.
- **REL-06 — M3:** The first reference bundle composes the existing Envy chart
  with optional pinned PostgreSQL and Istio components. Each is selected explicitly
  or supplied externally; installation must reject ambiguous ownership. Existing
  Cilium/Linkerd bring-your-own profiles remain supported. External identity,
  DNS/TLS, cloud grants and telemetry remain operator-owned; no telemetry backend
  or cloud resource provisioning is added to this bundle.
- **REL-07 — M3:** Version a resource/ownership inventory, supported configuration
  and lifecycle procedures alongside the bundle. Test installation, upgrade,
  explicit replacement and uninstall in bundled and bring-your-own configurations.
  Persist data/credentials according to documented retention, never transfer
  ownership of an existing mesh or database implicitly, and require drain/review
  where the current installation policy demands it. Disabling a bundle option is
  not automatic data migration or permission to delete a borrowed resource.

M1 introduces an operator-accessible metrics surface and stable metric semantics,
without a bundled collector/backend. M3 adds packaging/configuration and ownership
metadata, not an infrastructure reconciliation subsystem. Existing standard-chart
defaults remain external; do not silently install dependencies during upgrade.

## Acceptance

- [ ] **REL-A1 / REL-01, REL-05:** Drive normal, throttled and failing reconciliation
  and show useful bounded metrics. One unhealthy domain cannot prevent unrelated
  domains from progressing. Metrics expose no credential or unbounded-ID labels.
- [ ] **REL-A2 / REL-02:** Crash at intent/ensure/observe/cleanup boundaries; lose
  leadership and database/API access. Recover ownership, route state and execution
  identity without duplicated finite work or false absence claims.
- [ ] **REL-A3 / REL-03:** Restore an older backup against newer live resources,
  including a cancelled Job. Demonstrate explicit review and safe recovery. Test
  retain-on-uninstall and completed-cleanup uninstall, preserving borrowed resources.
- [ ] **REL-A4 / REL-04, REL-05:** Capture repeatable reports for each supported mesh
  and declared capacity point. Detect routing isolation/correctness failures under
  load. Publish measured limits and limitations before proposing higher defaults.
- [ ] **REL-A5 / REL-06, REL-07:** Render and install fully supplied, fully bundled
  and mixed configurations in disposable environments. Upgrade and replace using
  documented procedures; test interrupted cleanup and retained database credentials.
  Verify another application's database/mesh is neither adopted nor removed.

## Rollout and exclusions

Metrics and runbooks can land before other PRDs. Job/worker recovery gates expand
as [PRD 04](04-workloads.md) delivers those kinds. M1/M2 plus team controls gate
supported fleet operation; M3 bundles are optional and do not block a fleet using
operator-supplied infrastructure.

Acceptance reports must name revisions, environment and recovery limitations.
No HA/service-level claim, arbitrary component-limit increase, automatic mesh or
database upgrades, cloud provisioning or hidden resource ownership is introduced
by writing the PRD or packaging a reference configuration.
