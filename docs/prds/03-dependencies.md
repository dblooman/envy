# PRD 03: Dependency bindings and isolation contracts

Status: proposed. Priority: before workers and automatic schedules.
Contract: [Shared boundaries](product-boundaries.md).
Evidence baseline: [reviewed commit](README.md#evidence-baseline).

## Problem, users and current state

An approved Pod shape does not establish database access, safe message consumption
or isolation of external writes. Platform owners need a durable readiness handoff;
developers need to know what is shared and why execution is blocked.

Implemented evidence:

- [Workload declarations](../../internal/domain/workload.go) include component
  dependency edges and cycle validation. They are not external binding resources.
- [Pub/Sub reconciliation tests](../../internal/reconciler/messaging_test.go)
  cover isolation before publishers and cleanup retries. This existing provider
  owns preview subscriptions independently of consumer compute.
- [Preview discovery](../../internal/providers/kubernetes/preview.go) captures
  approved named configuration dependencies. [Composite acceptance](../../tests/e2e/composite_test.go)
  exercises a synthetic dependency proxy and outage/recovery.

Current composite policy and operator readiness reports declare assumptions;
there is no generic external dependency readiness controller. Identity annotations
alone do not establish IAM authorization or dependency-protocol compatibility.

## First milestone and journey

M1 introduces scoped bindings and an operator-report adapter with synthetic
acceptance. An operator supplies approved endpoint/credential references and
time-bounded readiness evidence. A developer selects a contract; execution waits
until every required binding is usable. Existing Pub/Sub observations integrate
with this view while retaining their current resource ownership.

## Requirements and interfaces

- **DEP-01:** Give bindings durable identity and revision, installation/project
  scope, dependency kind, permitted consumers, access/isolation mode, credential
  references, binding lifetime, resource owner and cleanup owner. Endpoints and
  references must be approved metadata, never inline credentials or
  credential-bearing URLs.
- **DEP-02:** Represent pending, ready, unavailable, unknown, expired and revoked
  observations, with source, observed time, evidence validity deadline and observed
  binding revision. Expired evidence means unknown readiness; binding lifetime
  expiry is a separate terminal condition. Declared policy and operator attestations
  remain visibly different from protocol-tested evidence; a synthetic check never
  certifies a real service.
- **DEP-03:** Provide authenticated operator reporting for M1, including report
  identity and expected revision. Duplicate reports are safe; late, expired or
  wrong-revision reports cannot restore readiness. Only authorized platform actors
  and the scoped provider may report their respective binding's state.
- **DEP-04:** Persist selected binding revisions with the composition. Validate
  component/binding references and cycles before admission. Block dependent starts
  and replacements until required bindings are currently ready. Never substitute
  a shared endpoint, subscription or credential when the selected binding is absent.
- **DEP-05:** A binding becoming unavailable or unobservable marks affected
  readiness false and blocks new dependent work. Binding lifetime expiry or
  explicit revocation additionally suspends future scheduling and requests
  termination of dependent workloads; report termination pending until absence is observed. Recovery from
  a transient outage may resume existing execution, but revoked/expired bindings
  require an explicit approved rebind and never silently restart cancelled Jobs.
- **DEP-06:** Composition deletion removes only Envy-owned binding resources.
  Stop consumers before deleting owned subscriptions. Externally supplied
  dependencies and their grants are never deleted by Envy; publish handoff status
  and retain the external cleanup owner. A failure to reach Kubernetes is not
  evidence that dependent work has stopped.
- **DEP-07:** Provide synthetic database-like and messaging fixtures supporting
  success, denial, outage, expiry and revocation. Label their protocol coverage.
  Real database/broker provisioning, schema preparation and grants stay external.
- **DEP-08:** Expose bindings and prerequisite state consistently across REST,
  CLI, MCP and UI, including in plans and diagnostic explanations. Required binding
  failure affects composition status; independent healthy HTTP endpoints retain
  their individual state. Approval must describe which effects are actually enforced.

Minimum interfaces are operator binding registration/revision, bounded observation
reporting, consumer selection and prerequisite observations. Start with the
operator-report adapter, current Pub/Sub provider and synthetic fixtures; no
arbitrary scripts, cloud provisioning adapter or new broker provider is required.
External Secret distribution still owns materializing approved references where
the existing runtime contract requires it.

## Acceptance

- [ ] **DEP-A1 / DEP-01, DEP-03, DEP-04:** Reject wrong-project consumers, unknown
  bindings, cycles and stale reports. Accept identical retries without duplicated
  resources. Restart between selection and ensure without changing binding identity.
- [ ] **DEP-A2 / DEP-02, DEP-04, DEP-07:** A synthetic dependency is pending, then
  ready, then denied/unavailable. No dependent workload starts before readiness;
  a missing isolated binding never selects a shared resource.
- [ ] **DEP-A3 / DEP-02, DEP-03, DEP-05:** Let evidence expire and show unknown
  readiness without treating the binding as revoked. Expire/revoke the binding
  during execution. Prevent new work,
  request termination, and reject a late ready report. During a cluster outage
  show pending termination; recovery must finish it before owned-resource cleanup.
- [ ] **DEP-A4 / DEP-06, DEP-07:** Delete one of two compositions during active
  consumption. Its consumers stop before subscription deletion; the other
  composition, baseline and operator-provided resources remain untouched.
- [ ] **DEP-A5 / DEP-02, DEP-08:** Clients display declared versus observed
  isolation and external ownership consistently. Credential payloads are absent
  from plans, observations and exports. Healthy unrelated endpoints remain visible.

## Dependencies and rollout

Ship persistence and reporting before consumers. New typed bindings are opt-in for
existing HTTP profiles; do not convert free-text shared-dependency declarations
into ready bindings. Profiles claiming enforced prerequisites must select the new
contract explicitly. Apply scoped permissions from [PRD 05](05-team-controls.md)
when available; before that, retain the installation's explicit trusted-operator
boundary without claiming project authorization.

[PRD 04](04-workloads.md) consumes this gate and termination ordering.
Each future protocol/cloud adapter requires separate installation acceptance for
real IAM, networking and protocol behaviour. Record those gates as pending until
executed; the synthetic fixtures are sufficient for product orchestration tests.
