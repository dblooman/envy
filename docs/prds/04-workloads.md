# PRD 04: Background workloads and lifecycle integration

Status: proposed extensions to existing finite Jobs and suspended CronJobs.
Contract: [Shared boundaries](product-boundaries.md).
Evidence baseline: [reviewed commit](README.md#evidence-baseline).

## Problem, users and current state

Application teams need previews containing background work, and sometimes no HTTP
service at all. Developers and CI need completion, cancellation and failure
outcomes without manufacturing a public endpoint.

Implemented evidence:

- [Workload model](../../internal/domain/workload.go) declares HTTP, worker, Job
  and scheduled-Job kinds, execution identity and bounded finite-work settings.
- [Job provider tests](../../internal/providers/kubernetes/jobs_test.go) cover
  endpoint-free Jobs, observation and deletion. [Reconciler tests](../../internal/reconciler/reconciler_test.go)
  check identity persistence and completed Job-only compositions.
- [Scheduled provider](../../internal/providers/kubernetes/scheduled_jobs.go)
  can construct CronJobs, but the reconciler keeps schedules suspended without
  a durable execution gate; its test explicitly requires that behaviour.
- [GitHub preview helper](../../integrations/github-actions/preview.py) waits
  for ready/destroyed and checks a public endpoint. Endpoint-free adoption needs
  an explicit extension rather than assuming this flow already covers it.

Worker runtime support and automatic scheduling are not delivered by the presence
of domain enum values. Job identity prevents unrelated generation changes from
implicitly defining a new run; explicit reruns and complete live acceptance need
their own contract.

## Milestones and journeys

**M1: finite-Job completion gate.** Complete cancellation/rerun semantics and live
synthetic acceptance for existing Jobs. CI selects a finite workload, observes
completion/logs and cleans it up without an endpoint. Ship the corresponding
client terminal-state handling in this milestone.

**M2: workers.** After PRD 03, select an approved worker that starts automatically
when isolated bindings are ready. Two compositions process separate synthetic
inputs and can restart without sharing a subscription.

**M3: bounded schedules.** Admit an approved schedule, activate it after readiness,
enforce execution bounds even across controller outages, and stop future runs on
expiry/revocation before terminating active work.

## Requirements and interfaces

- **WRK-01 — M1:** All selected workloads are required. Job-only compositions
  complete only after required Jobs succeed, stay inspectable until deletion/TTL,
  and never allocate Services or public routes. Mixed compositions expose each
  endpoint separately while required workload failure affects the aggregate.
- **WRK-02 — M1:** Persist execution identity and owned provider identity before
  starting work. An unchanged image and approved execution inputs must not rerun
  because another component changes. Explicit rerun receives a new run identity;
  changed execution inputs create a replacement only after the previous execution
  has stopped. Cancellation does not become an implicit retry on reconciliation.
- **WRK-03 — M1:** Retain bounded timeout/retries and logs. Expose explicit
  execution cancellation separately from cancelling a client wait. Preserve
  cancelled execution history. Composition deletion and TTL terminate active work
  and require observed cleanup; an execution failure does not delete its evidence.
- **WRK-04 — M2:** Workers are approved endpoint-free Deployments with bounded
  resource settings and required readiness/liveness probes, without a public
  Service requirement. Support Kubernetes probe configuration independently of
  ingress configuration. Use Pub/Sub isolation first; other messaging requires
  operator-provided isolated bindings. Never consume a shared subscription as fallback.
- **WRK-05 — M2/M3:** Dependency readiness gates automatic execution. Stop workers
  and active finite work before deleting their owned subscriptions. Count concurrent
  Jobs, workers, supporting containers and rollout overlap in admission. Readiness
  describes the approved probe/binding contract, not correct business processing.
- **WRK-06 — M3:** Create schedules inactive. A durable Envy run gate may release
  one admitted occurrence only after current prerequisites pass. Reserve each run
  identity and budget before creating its Job; retry that identity after an
  uncertain create. Enforce maximum runs, per-run duration/retries, composition
  expiry, no overlap and no replay of missed occurrences. Do not simply unsuspend
  a free-running CronJob whose limits depend on Envy remaining online.
- **WRK-07 — M3:** Suspension/revocation first prevents new occurrences, then
  terminates active work as required. Persist that ordering through restart and
  uncertain provider responses. Loss of scheduling authority stops issuing work;
  it does not imply already-running Pods instantly stop.
- **WRK-08 — every milestone:** REST, CLI, MCP, UI and CI expose ready, completed,
  failed, suspended and cancelled execution appropriately. Preserve existing HTTP
  wait behaviour and add outcome-aware waits. Frontend bindings cannot resolve an
  endpoint-free composition as an API origin. Existing frontend evidence becomes
  stale when its backend generation or availability contract changes; hosting and
  browser execution remain external.

Add explicit execution cancellation/rerun operations with idempotency and expected
execution/version checks. Maintain run history separately from a component's
current execution pointer. M3 adds occurrence reservations and schedule authority;
older records remain suspended until explicitly migrated to that capability.
Completion is not an exactly-once application-side-effect guarantee: workloads
remain responsible for processing idempotency under retries and restarts.

## Acceptance

- [ ] **WRK-A1 / WRK-01, WRK-02:** In a disposable cluster, run a Job-only
  composition and restart after intent persistence and after Job creation. Observe
  one execution, completion, no routes, retained history and no rerun when another
  component changes. Explicit rerun/replacement creates a distinct, non-overlapping run.
- [ ] **WRK-A2 / WRK-03, WRK-08:** Exercise timeout, exhausted retries, cancellation,
  TTL and deletion during work. A cancelled wait leaves execution running; an
  execution cancellation stops it and remains cancelled after restart.
- [ ] **WRK-A3 / WRK-04, WRK-05:** Two isolated workers process distinguishable
  synthetic inputs, restart and recover. Missing bindings block start; revocation
  stops work before subscriptions disappear. Exhaust quota during replacement.
- [ ] **WRK-A4 / WRK-06, WRK-07:** Lose controller authority across several schedule
  times and an uncertain Job create. Recover with no duplicate, overlap, missed-run
  replay or execution beyond the count/time bounds. Expiry/revocation prevents a
  new run before cleanup. An old suspended schedule stays inactive during upgrade.
- [ ] **WRK-A5 / WRK-08:** For each delivered kind, exercise CI create, update,
  cancellation and PR closure; reject late reports. Test a hosted synthetic
  frontend against a current HTTP backend and reject stale/destroyed bindings or
  an endpoint-free backend. Verify equivalent client outcome presentation.

## Dependencies, rollout and exclusions

M1 can start with existing approved finite inputs. M2/M3 require
[dependency bindings](03-dependencies.md) and their revocation contract. Expand
capabilities one kind at a time after its live acceptance; ship migrations and API
readers before enabling new execution writers. Keep old scheduled records inactive
and include an explicit opt-in activation procedure.

No application migrations, unrestricted process import, unbounded schedules,
additional broker providers, application test runner or frontend hosting. The
existing [workload roadmap](../workloads.md) supplies historical context; these
milestones define the next implementation gates.
