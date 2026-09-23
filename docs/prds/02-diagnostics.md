# PRD 02: Diagnostics, verification and baseline drift

Status: first milestone in implementation. Priority: first delivery alongside onboarding.
Contract: [Shared boundaries](product-boundaries.md).
Evidence baseline: [reviewed commit](README.md#evidence-baseline).

## Problem, users and current state

Developers need to distinguish a broken build, dependency outage, rejected route
and missing verification without searching several screens. Platform owners need
the evidence behind each diagnosis, especially when inherited services change.

Implemented evidence:

- [Verification persistence](../../internal/persistence/postgres/verification.go)
  and [tests](../../internal/persistence/postgres/verification_test.go) retain
  generation-scoped evidence. [Application evidence](../../internal/application/evidence.go)
  also exposes operator-configured external observability links.
- [Preview evidence tests](../../web/src/components/compositions/PreviewEvidence.test.tsx)
  cover the existing UI, while [live verification tests](../../tests/e2e/verification_test.go)
  exercise the ingress evidence contract.
- [Kubernetes watches](../../internal/providers/kubernetes/watch.go) provide
  observations. Inherited baseline workloads remain live; a captured catalog or
  recipe does not freeze their running deployments or shared data.

Logs are bounded snapshots, including explicitly labelled shared-baseline logs.
Existing evidence does not provide a general dependency diagnosis or an immutable
snapshot of every service participating in a preview.

## First-milestone implementation progress

The initial increment adds [read-only baseline execution observations](../../internal/providers/kubernetes/baseline_observation.go)
for registered Services and inherited Deployments. The fingerprint uses Service
UID, selector and ports, Deployment UID and Pod-template content, declared images
and observed image IDs when present. It excludes status and replica-only changes;
missing image identity remains unknown. It never reads Secret or ConfigMap
contents. The [reconciler](../../internal/reconciler/reconciler.go) records that
fingerprint with new verification evidence, checks it again after a probe, and
keeps serving proof unknown during an observation outage. [Evidence reads](../../internal/application/evidence.go)
derive current, stale, unavailable, failed or unknown-coverage freshness, and
the [dashboard](../../web/src/components/compositions/PreviewEvidence.tsx)
does not present stale or legacy proof as current.

A second increment adds a read-time [structured diagnosis](../../internal/domain/diagnosis.go)
from persisted composition conditions and the latest verification record. It
reports separate observed blockers for owned workloads, messaging, routing,
verification, baseline observation and cleanup, with scope, observation time and
next steps. The [REST endpoint](../../internal/api/diagnostics.go), CLI, MCP and
preview Overview expose the same response. Findings are ordered for inspection,
without claiming that the first one caused the others. Missing evidence remains
unknown; HTTP reachability does not become routing proof.

When selected workloads have registered execution dependencies, their findings
are ordered prerequisite first and carry those declared edges. An operator
binding outage cannot yet be ordered from an authoritative binding observation;
that contract belongs to [PRD 03](03-dependencies.md).

Configured external telemetry links can include the current composition
`{generation}`. The HTTP and chain checkers retain a strictly validated
`X-Request-ID` from each response, when supplied. A `{request_id}` link resolves
only from the preview probe of current or failed fingerprint-covered evidence;
without that observation the link is omitted. Envy never fabricates a request
or trace identifier, and a separate telemetry backend remains operator-owned.

This is a foundation, not M1 acceptance. Authoritative external dependency
observations, broader trace integration, full synthetic failure scenarios and the remaining
acceptance below are still to be delivered. Historical evidence is retained
with unknown fingerprint coverage rather than upgraded.

## First milestone and journey

M1 combines existing observations into an explanation, adds baseline execution
fingerprints and marks affected evidence stale. A developer opens a failed preview,
sees the earliest known blocking prerequisite, inspects its evidence and follows
an external trace/log link if available. An operator can distinguish local preview
failure from a baseline change affecting several previews.

## Requirements and interfaces

- **DIA-01:** Provide one structured explanation of observed workload, dependency,
  routing, verification and cleanup conditions. Show a dependency-ordered blocker
  when supported by evidence; show multiple independent blockers without inventing
  a unique root cause. Include timestamps, scope and actionable next steps.
- **DIA-02:** Label evidence by composition generation, workload/image identity,
  verification contract and observed baseline fingerprint. Distinguish current,
  stale, unavailable and failed checks. Keep historical evidence inspectable.
- **DIA-03:** Track relevant registered baseline Service routing and inherited
  workload execution identity using supported observed fields: resource UID,
  Service selector/ports, Pod-template generation and actual image identities
  where available. Exclude status churn and replica-count-only changes from
  execution drift. Unknown image/configuration identity remains explicitly unknown;
  do not inspect Secret payloads to claim a complete configuration fingerprint.
- **DIA-04:** When an observed inherited execution or routing fingerprint changes,
  invalidate affected current verification without changing desired overrides or
  restarting unrelated workloads. Request a new supported verification check.
  If the baseline is unavailable or watches are unsynchronised, current proof is
  unknown until observation recovers. Generation/fingerprint checks must reject a
  late successful result from the previous state.
- **DIA-05:** Retain the distinction between reachability and routing proof.
  Display observed service hops and baseline comparison only when the checker
  supplies them. A baseline success plus preview failure narrows the problem; it
  does not identify a faulty application line or prove every request path.
- **DIA-06:** Extend configured external telemetry links with supported context
  such as generation and observed request/trace identity. Retain bounded log reads,
  shared-log labels, URL validation and project authorization. Missing telemetry
  integrations must not block preview readiness or produce fabricated results.
- **DIA-07:** REST supplies explanations and evidence freshness for UI, CLI and
  MCP. Report cleanup separately from serving readiness. Observation outages must
  never be interpreted as successful deletion or trigger baseline fallback.

Add optional diagnostic relationships, observation scopes and baseline fingerprints
to the existing evidence model. Migrate historical records as lacking fingerprint
coverage, not newly verified. A presentation graph is derived from declared
relationships and observations, not automatic discovery of all application calls.
Before PRD 03, external prerequisite information may only be declared/unknown;
consume its authoritative binding observations when available.

## Acceptance

- [ ] **DIA-A1 / DIA-01, DIA-05:** Synthetic image failure, rejected routing and
  dependency outage produce distinct evidence-backed explanations. Two unrelated
  failures remain visible; no unsupported root-cause claim appears.
- [ ] **DIA-A2 / DIA-02, DIA-03, DIA-04:** Change an inherited image or Service
  destination without changing the preview generation. Prior evidence becomes
  stale; a late old result cannot restore it. A fresh check can establish current
  evidence. Replica-only/status churn does not repeatedly invalidate it.
- [ ] **DIA-A3 / DIA-02, DIA-04:** Restart or disconnect observation, recover the
  baseline fingerprint and preserve the history. Old records retain an explicit
  coverage limitation rather than receiving invented evidence.
- [ ] **DIA-A4 / DIA-05, DIA-06:** HTTP-only checks never show unobserved hops.
  Trace links use only available identifiers and contain no credentials; absent
  telemetry leaves lifecycle diagnosis usable. Shared logs remain labelled.
- [ ] **DIA-A5 / DIA-07:** Compare REST, CLI, MCP and UI outputs during failure,
  update and deletion; unobservable resources never become confirmed absent.

## Rollout and exclusions

Ship additive observation storage and API fields before client presentation.
Existing verification results remain readable. Fleet later adds target identity;
fingerprints must already be scoped to an installation and registered baseline.
Record blocker categories and stale-evidence transitions through the operational
metrics contract in [PRD 06](06-reliability-and-bundles.md).

No telemetry backend, application log ingestion/search, arbitrary network probing,
automatic remediation or full environment snapshotting. Bounded built-in ingress
verification remains; application tests and instrumentation are externally owned.
