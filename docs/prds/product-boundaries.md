# Shared product boundaries

Status: proposed product contract, 23 September 2026.
Applies to every PRD in the [suite](README.md). Current implementation evidence
uses the [reviewed commit](README.md#evidence-baseline).

## Audience and ownership

The platform owner installs Envy, supplies infrastructure and approves execution
contracts. Developers select approved applications and revisions. CI and agents
act through scoped credentials and the same application API. The first team model
is one trusted organisation; separate cloud projects or accounts do not imply
mutually untrusted workload hosting.

| Model | Envy's responsibility | Operator or external responsibility |
| --- | --- | --- |
| Opinionated core | Durable intent, composition lifecycle, approved execution, prerequisite gates, routing, evidence, permissions and owned-resource cleanup | Approve application behaviour and infrastructure access |
| Replaceable defaults | Opt-in reference bundles and synthetic dependencies, documented versions and lifecycle procedures | Select bundled or existing infrastructure and operate it |
| Bring your own | Validate bindings and report readiness and limitations | Real databases/brokers, IAM grants, cloud resources, production-grade infrastructure and telemetry backends |
| External execution | Accept immutable inputs and scoped, revision-bound results | Builds, database migrations, application tests and frontend hosting |

The existing Pub/Sub provider is an intentional ownership exception: Envy owns
its preview isolation resources. It does not own baseline topics/subscriptions or
become a generic cloud provisioning system. Envy's own schema migration remains
part of installing/upgrading Envy; application database migrations remain external.

## Common requirements

- **C-01 — Shared semantics.** REST owns application behaviour. CLI, MCP and UI
  use the same permissions, lifecycle, conflicts and evidence model. Convenience
  integrations must not bypass admission or claim readiness independently.
- **C-02 — Replaceability.** Every optional bundle documents resources, pinned
  versions, configuration, credentials, retention, backup needs, replacement and
  uninstall ownership. Disabling a bundle must not delete borrowed resources.
  Replacement is an explicit operator procedure, not a promise of live migration.
- **C-03 — Integration contracts.** Use a small supported adapter set with versioned
  contracts, capabilities and compatibility checks. Reject unsupported operations.
  Do not create an arbitrary plugin runtime or execute caller-provided scripts.
- **C-04 — Truthful evidence.** Separate declared configuration, operator
  attestations, observed infrastructure state and application verification.
  Include observation scope and freshness. Unknown, unreachable, stale and
  unsupported must not mean ready. Routing context is never authorization.
- **C-05 — Durable execution.** Persist intent before side effects; retain
  idempotency, execution identity, generation fencing and cleanup ownership.
  Cancellation of a client wait does not cancel workload execution. Observed
  absence is required before cleanup is complete.
- **C-06 — Compatibility.** New fields and migrations preserve existing HTTP
  clients and single-installation operation. Existing clients may ignore new
  optional details; clients consuming new kinds must understand their terminal
  states. Roll out readers/migrations before writers. Explicitly migrate policy
  enforcement rather than silently changing access or approving legacy evidence.
- **C-07 — Trust and confidentiality.** Project authorization applies to reads,
  logs, evidence and mutations. Secret payloads must not enter catalog metadata,
  plans, telemetry URLs, fleet projections or checked-in examples. Approved
  Kubernetes Secret handling remains a scoped runtime responsibility. Project
  roles and network policies do not establish hostile-tenant isolation.
- **C-08 — Bounded scope.** Keep current component limits until qualified by
  measurements. Account for init/sidecar resources, overlapping rollouts and
  concurrent executions. Do not infer isolation of shared application state,
  provision application databases or execute their migrations.

## Defaults and integration scope

No new hosted service, subscription billing, image builder, test runner or
frontend host is part of this programme. Existing CI can publish artifacts and
tests can report outcomes. Envy continues to perform its own bounded readiness
and routing verification; that is distinct from owning application test execution.

Dependency v1 accepts operator-provided bindings and synthetic fixtures. A
synthetic HTTP dependency is a connectivity test, not database or broker protocol
certification. Diagnostics use existing telemetry backends. Reference bundles do
not add a telemetry backend in this phase.

Fleet v1 manages independent compositions in cluster-local installations. Use
local synthetic clusters and GKE across separate projects for acceptance. Preserve
Kubernetes portability without declaring every cloud qualified. No common Google
Cloud fleet, shared identity pool or cross-cluster application network is required.

## Architecture decisions to preserve or revisit

| Existing decision | Treatment when implementing this suite |
| --- | --- |
| [ADR 001: Product boundary](../adr/001-product-boundary.md) | Preserve external builds, agents and application test execution; integration is not ownership |
| [ADR 002: Canonical state](../adr/002-canonical-state.md) | Preserve PostgreSQL authority locally; fleet needs an additional ADR assigning coordinator request state versus local execution state |
| [ADR 003: Provider boundaries](../adr/003-provider-boundaries.md) | Extend narrow interfaces for concrete binding/target capabilities, without a universal plugin framework |
| [ADR 004: Routing](../adr/004-routing.md) | Fleet keeps routing domains cluster-local; a later cross-cluster composition proposal would require a separate decision |
| [ADR 006: Lifecycle](../adr/006-lifecycle.md) | Extend with explicit worker, schedule and revocation ordering; retain durable cleanup |
| [ADR 007: Sharing](../adr/007-sharing-semantics.md) | Preserve honest shared-state guarantees; bindings make approved modes visible rather than magically isolating data |
| [ADR 008: Surfaces](../adr/008-delivery-surface.md) | Preserve authoritative REST and thin adapters; historical feature counts are not a current capability inventory |
| [ADR 009: Mesh profiles](../adr/009-mesh-profiles.md) | Preserve current profiles per installation; fleet extends singleton configuration with target-local capabilities |

Requirements are proposed changes, not retroactive changes to accepted ADRs.
Implementation PRs must add or amend the relevant decision record before claiming
a changed architecture is supported.

## Common acceptance

- [ ] **C-A1 / C-01, C-06:** Exercise each new lifecycle through REST, CLI, MCP
  and UI; test existing HTTP payloads and an upgraded installation with old records.
- [ ] **C-A2 / C-02, C-03:** Install using supplied infrastructure and the optional
  reference configuration; reject incompatible adapters; preserve borrowed
  resources during replacement and uninstall.
- [ ] **C-A3 / C-04, C-07:** Permission denial, missing observations and stale
  evidence remain distinguishable. Inspect plans, logs metadata and exports for
  credentials; verify unauthorized users cannot retrieve project data.
- [ ] **C-A4 / C-05:** Interrupt after intent commit and after provider mutation;
  retry without duplicate execution. Test cancellation, TTL and deletion during
  work, including unavailable cleanup dependencies.
- [ ] **C-A5 / C-08:** Exhaust admission capacity during creation and replacement;
  preserve unrelated previews and baseline resources. Demonstrate the declared
  isolation mode and explicitly record unverified protocol/cloud assumptions.

Apply these gates to the milestone introducing the relevant capability. A docs-only
change validates the requirements and links; it does not check these delivery boxes.
