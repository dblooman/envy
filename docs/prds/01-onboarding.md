# PRD 01: Application onboarding and preview planning

Status: proposed. Priority: first delivery.
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

- [ ] **ONB-A1 / ONB-01, ONB-03:** A permitted named synthetic Deployment prefills
  supported values. A denied read and unsupported field produce distinct blockers
  without broader discovery or automatic mutation.
- [ ] **ONB-A2 / ONB-02:** Save partial preparation, restart and resume it. Reject
  concurrent stale saves and verify installation/author separation and no secrets.
- [ ] **ONB-A3 / ONB-04, ONB-05:** Review a plan, change the source/profile/build
  reference or consume capacity elsewhere, and reject stale creation. Retry an
  accepted request idempotently. A valid plan creates exactly one preview.
- [ ] **ONB-A4 / ONB-03, ONB-06:** The composite synthetic fixture exposes a broken
  dependency and missing propagation evidence. The UI cannot label reachability
  alone as routing proof; the external acceptance path can supply scoped evidence.
- [ ] **ONB-A5 / ONB-07:** Complete planning and creation through each client;
  cancel before create and during a wait. Existing bundle registration still works.

## Rollout and exclusions

Ship draft persistence and the plan API before client use; older servers must
show these features as unavailable. Existing approved profiles and the advanced
registration flow remain usable. Measure first-preview completion and time spent
at each blocker using local product/controller observations, without mandatory
external analytics or claiming an unmeasured onboarding-time target.

No automatic source edits, arbitrary script execution, image builds, infrastructure
provisioning or unrestricted Kubernetes import. Use [PRD 02](02-diagnostics.md)
for runtime diagnosis and [PRD 03](03-dependencies.md) for enforceable prerequisites.
