# PRD 01: Application onboarding and preview planning

Status: first milestone merged and locally accepted in [PR #24](https://github.com/dblooman/envy/pull/24); installation acceptance remains separate. Priority: first delivery.
Contract: [Shared boundaries](product-boundaries.md).
Evidence baseline: [reviewed commit](README.md#evidence-baseline).

## Problem, users and current state

A platform owner can already register and prepare an application, but repeated
manual input and fragmented readiness information make the first preview hard to
review. A developer should reuse that approved contract without understanding its
mesh configuration or Pod layout.

Implemented evidence:

- [Onboarding service](../../internal/application/onboarding.go) and
  [its tests](../../internal/application/onboarding_test.go) normalise, validate
  and register catalog bundles.
- [Guided dashboard tests](../../web/src/components/catalog/ApplicationOnboarding.test.tsx)
  cover registration, resuming saved preparation, edits, blockers and approval
  conflicts. This workflow already exists; M1 extends it.
- [Discovery](../../internal/providers/kubernetes/preview.go) and
  [connectivity findings](../../internal/providers/kubernetes/connectivity.go)
  inspect supported workload/configuration shapes. The
  [readiness helper](../../integrations/onboarding/readiness.py) combines checks
  and operator evidence; it is not an execution enforcement boundary.

Saved catalog and approved profiles survive reloads today; unsaved preparation
edits do not. Discovery is not unrestricted cluster browsing, and HTTP health does
not prove downstream context propagation.

## First-milestone implementation and evidence

The first milestone adds a [revision-checked, non-secret draft](../../internal/domain/onboarding_draft.go)
and [installation/project/author-scoped persistence](../../internal/persistence/postgres/onboarding_draft.go).
The [read-only plan](../../internal/application/onboarding_plan.go) shares
creation's catalog, build, messaging and profile checks, showing selected and
inherited workloads, immutable images, declared dependencies, destination,
lifetime, a resource estimate and actionable blockers. The [dashboard](../../web/src/components/compositions/CreateCompositionView.tsx),
[CLI](../../internal/cli/cli.go) and [MCP tools](../../internal/mcp/server.go)
consume the REST contract. Creation still rechecks the source contract and
capacity. Named source-read permissions and distinct forbidden/missing-resource
errors are reported during discovery. Drafts and plans are review aids, never
approval or reservations.
In shared-token and anonymous access modes, the common principal is the draft
author; per-person isolation requires an identity-bearing access mode.

Local synthetic acceptance now covers named read-only discovery, missing
permissions and unsupported shapes, draft restart and scope, stale source and
approval contracts, capacity races, idempotent creation, client planning and
cancellation, and a broken composite dependency. The reviewed plan is a
point-in-time aid: it is not a capacity reservation, and creation checks the
current contract again. External dependency protocol readiness, cloud IAM and
live source-cluster permissions remain installation acceptance gates. HTTP
reachability alone is never routing proof.

## First milestone and journey

M1 adds resumable, non-secret preparation drafts and a common preview planning
view on top of existing registration/discovery/approval. An operator selects named
baseline resources, reviews discovered values and resolves blockers. A developer
selects published revisions, reviews overrides and shared dependencies, and creates
a preview from the reviewed contract. No infrastructure is provisioned by planning.

Later integration consumes typed dependency observations from PRD 03 and target
capabilities from PRD 07; neither blocks this first milestone.

## Requirements and interfaces

- **ONB-01:** Read only operator-selected, permitted baseline resources. Prefill
  supported fields from discovery, distinguish discovered values from edits, and
  report required named permissions. Do not introduce cluster-wide enumeration
  or accept an unsupported manifest by dropping its fields.
- **ONB-02:** Persist non-secret preparation drafts scoped to installation,
  project and author. Resume the last accepted save after reload/restart. Use
  revision checks for concurrent edits; a draft is not a registered or approved
  profile. Installation switching must not reuse another installation's draft.
- **ONB-03:** Give each blocker a stable code, affected component, evidence source
  and next action. Distinguish missing permission, unsupported shape, stale
  approval and unknown external readiness. Show unresolved shared side effects.
- **ONB-04:** Expose a read-only preview plan through REST and all clients. Show
  selected immutable builds, inherited components, approved profile revisions,
  declared dependencies, destination identity, resource demand, lifetime and
  verification level. Never include Secret values or count a draft as approval.
- **ONB-05:** Explicit operator actions register and approve. Creation rechecks
  the reviewed profile/catalog/build revisions, policy and capacity. An edited or
  stale plan requires review again; no silent rebase or automatic grant changes.
- **ONB-06:** Capture known propagation/connectivity findings and link them to
  the application acceptance procedure. Missing instrumentation means routing
  proof is unknown; a 200 response must not certify selected downstream workloads.
- **ONB-07:** Developers and agents can use approved contracts without source
  cluster credentials. Retain the advanced configuration-bundle workflow and
  expose equivalent planning and preparation through CLI/MCP. Cancelling
  preparation leaves no preview resources; cancelling a wait leaves any already
  accepted creation intact and visible.

Minimum additions are a versioned draft record and a non-mutating plan/report
contract referencing existing catalog, build and approval identities. Plans are
review evidence, not capacity reservations. Keep create's server-side checks as
the final authority. Permission enforcement follows the currently active access
mode until PRD 05 introduces scoped roles; do not imply roles exist before then.

## Acceptance

- [x] **ONB-A1 / ONB-01, ONB-03:** A permitted named synthetic Deployment prefills
  supported values. A denied read and unsupported field produce distinct blockers
  without broader discovery or automatic mutation.
- [x] **ONB-A2 / ONB-02:** Save partial preparation, restart and resume it. Reject
  concurrent stale saves and verify installation/author separation and no secrets.
- [x] **ONB-A3 / ONB-04, ONB-05:** Review a plan, change the source/profile/build
  reference or consume capacity elsewhere, and reject stale creation. Retry an
  accepted request idempotently. A valid plan creates exactly one preview.
- [x] **ONB-A4 / ONB-03, ONB-06:** The composite synthetic fixture exposes a broken
  dependency and missing propagation evidence. The UI cannot label reachability
  alone as routing proof; the external acceptance path can supply scoped evidence.
- [x] **ONB-A5 / ONB-07:** Complete planning and creation through each client;
  cancel before create and during a wait. Existing bundle registration still works.

The acceptance evidence is exercised by the [named source-read tests](../../internal/providers/kubernetes/preview_read_test.go),
[composite discovery tests](../../internal/providers/kubernetes/composite_discovery_test.go),
[draft persistence tests](../../internal/persistence/postgres/onboarding_draft_test.go),
[reviewed-plan tests](../../internal/application/preview_test.go),
[capacity and retry tests](../../internal/persistence/postgres/store_test.go),
[REST tests](../../internal/api/onboarding_plan_test.go),
[CLI tests](../../internal/cli/onboarding_plan_test.go),
[MCP protocol tests](../../internal/mcp/server_test.go),
[dashboard creation tests](../../web/src/components/compositions/OnboardingCreation.test.tsx),
and the [operator-readiness tests](../../integrations/onboarding/test_readiness.py).
The operator's scoped business-scenario evidence is an assertion recorded by
the external acceptance helper, not automatically verified routing proof.

## Rollout and exclusions

Ship draft persistence and the plan API before client use; older servers must
show these features as unavailable. Existing approved profiles and the advanced
registration flow remain usable. Measure first-preview completion and time spent
at each blocker using local product/controller observations, without mandatory
external analytics or claiming an unmeasured onboarding-time target.

No automatic source edits, arbitrary script execution, image builds, infrastructure
provisioning or unrestricted Kubernetes import. Use [PRD 02](02-diagnostics.md)
for runtime diagnosis and [PRD 03](03-dependencies.md) for enforceable prerequisites.
