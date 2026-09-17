# Evolving compositions and installation into an existing cluster

Status: proposed implementation plan, 12 September 2026. This document changes no
runtime behavior. It covers roadmap items 2 and 3.

## Agreed scope

- A composition can add, change, or remove overrides while retaining its ID and URL.
- Zero overrides is valid: the preview URL continues selecting the live baseline.
- The first installation package integrates with existing Kubernetes, Istio,
  PostgreSQL, DNS/TLS, and an authentication proxy. It does not install those systems.
- REST remains authoritative; the website, CLI, and MCP expose the same behavior.
- Keep the current maximum of three simultaneous overrides and single-cluster,
  single-organisation boundary. No atomic cutover, built-in SSO, new runtime,
  database cloning, or automatic PR provisioning is included.

## 1. Evolving compositions

### User contract

Example: create an environment with service-b v2; add service-a v2; return
service-b to the baseline; return service-a to the baseline; then add another
override. Every step uses the same composition ID and preview URL.

Keep `PATCH /v1/compositions/{id}` with `expected_generation` and a complete
desired `overrides` map. An omitted component returns to baseline. An explicit
empty map means inherit everything; an omitted or null map is invalid. Accept
zero overrides on create as well, including recipe validation and recreation.
Existing complete-map clients remain compatible.

Only ready or failed, unexpired compositions accept updates. A concurrent
rollout, cleanup still in progress, stale generation, expiry, or destruction
returns a structured conflict. Deletion and expiry always take precedence.
Reverting a selection is another generation, never history mutation.

Retain the pinned baseline bindings and existing approved profiles. Resolve new
components from the same project's catalog and require a binding in the pinned
baseline. Persist their resolved profiles with the update. Previously inherited
services remain live; this does not snapshot their deployments.

The URL and frontend bindings survive updates. Reported frontend checks become
stale when their recorded generation differs. Published frontend artifacts are
neither rebuilt nor republished by changing backend overrides.

Updates do not extend expiry. The current eight-hour default and configurable
maximum still apply. Keeping a URL across several days would need a separate
explicit renewal feature; it is not implied by these changes. Recipes provide
recreation after expiry with a new composition identity.

### Durable state and update transaction

The current runtime has one override map, a single `RoutingActive` flag, and
namespace-wide deletion. Removing a map key alone would lose cleanup intent and
could change shared routes before replacement workloads are ready.

Introduce separate persisted representations of:

1. Desired overrides and their resolved profiles for the requested generation.
2. The complete published route selection and its generation.
3. Owned namespace and workload identities, including workloads pending retirement.
4. Retirement evidence and a persisted drain deadline.

Compute added, changed, removed, and unchanged components in the application
layer. Lock the composition and commit the new desired plan, operation initiator,
immutable revision, and allowlisted before/after activity together. Preserve
retiring workload references until absence is observed.

Migrate existing rows from their resolved plan, workload inventory, and published
routing flag. Support legacy single-workload records. Do not invent old revision
snapshots or attribution. The empty-profile representation must be explicit so
legacy profile fallback cannot accidentally resurrect the final removed override.

Add optional `Idempotency-Key` to PATCH, scoped to composition and update action.
An accepted retry with identical content returns its original acceptance result
and polling location before checking the now-stale expected generation. Changed
content with the same key returns 409. Persist the request fingerprint and result
in the transaction. Unkeyed callers retain the existing generation-conflict behavior.

### Reconciliation and safe removal

1. Ensure added and changed workloads; leave unchanged workloads untouched.
   Budget quota for desired plus retiring workloads and normal rollout surge.
2. While new additions are unavailable, retain the previously published route
   selection. The composition remains updating or failed with readiness false.
   Existing-image changes keep ordinary rolling Deployment semantics.
3. Once desired workloads are ready, persist the new complete route selection
   before applying it. This is durable routing intent, not proof of convergence.
4. Reconcile aggregate routes across all compositions. Keep known routing domains
   present when their last override disappears, producing a baseline-only rule
   instead of abandoning a stale aggregate. Resolve ingress directly from the
   registered entry component, independent of whether any override exists.
5. Verify the resulting URL with the baseline's declared verification contract.
   The demo checker must prove removed components now use baseline identities.
   HTTP-only applications report reachability, not workload routing proof.
6. Retain retiring resources through a bounded, persisted drain interval after
   route acceptance and required verification. Then delete only those Deployments
   and Services, with ownership and UID preconditions, and observe absence.
7. Reduce quota after retirement. Mark the operation complete and the composition
   ready only when the requested generation is verified and cleanup has completed.
   Expose workload, routing, and cleanup conditions independently.

Istio configuration acceptance and finite probes do not prove every proxy has
converged. The documented guarantee is observed routing plus a bounded retirement
grace period, not lossless or atomic switching. After publication, an unhealthy
desired override retains its route; only an explicit removal requests inheritance.
Before publication, the previous generation can continue serving and is displayed
as such. No automatic rollback is added.

Split component deletion from namespace deletion in the internal runtime
capability. Destroy must inspect all owned inventory, including retired or
partially created workloads, rather than only current desired overrides. Every
mutation retains the database leadership guard and generation/deletion checks.

For a composition created with zero overrides, allocate no workload namespace
until its first override. If a namespace already exists, retain the empty namespace
and shared account/quota until composition destruction; remove all override pods
and Services. Persist namespace identity independently of component references.

### Website, CLI, MCP, and recipes

- The edit view lists registered baseline components with an explicit Inherit or
  Override selection. Existing build selections and drafts survive polling.
- Review shows additions, image/build changes, removals, and unchanged selections.
  Show a clear "All components inherit baseline" state for zero overrides.
- A conflict shows the latest selection alongside the draft and requires review.
  Do not silently merge edits or retry against a new generation.
- CLI and MCP send complete desired selections and stable update keys. CLI may
  offer explicit add/remove conveniences that first read state and then submit
  the resulting complete map with that generation; stale writes still conflict.
- Revisions/activity show the selection difference and the verified initiator;
  progress distinguishes desired generation, published routes, and cleanup.
- Route recipe operations through the server application service from all clients,
  removing duplicated orchestration in the private Go client. Allow empty recipes,
  preserve immutable image/build requirements and baseline-revision checks, and
  retain partial-binding retry behavior.

### Acceptance gate

Exercise REST, CLI, MCP, and a real browser across one stable URL:

- Create zero overrides; add service-b; add service-a; change service-b's image;
  remove service-b; remove the final override; re-add an override.
- Add and remove the entry component; check exact ingress destinations.
- Preserve a second composition's routes and baseline pod identities throughout.
- An unavailable addition retains the previous published selection and never
  marks the requested generation ready. A failed published override never falls
  back. Explicit removal can repair it.
- Crash/restart after intent commit, after route write, and during drain/deletion;
  recover without duplicate resources, lost retirement inventory, or actor changes.
- Lose database leadership during mutation; prevent subsequent writes.
- Retry an accepted PATCH; verify one operation, revision, and logical activity.
  Test stale/conflicting writes, TTL during update, and destroy during retirement.
- Remove the last route in a routing domain; assert no stale mesh entry remains.
- Verify unchanged workload UIDs, quota headroom, partial cleanup errors,
  stale frontend checks, empty recipe recreation, and older-record migration.

## 2. Installation into an existing cluster

### Installation product

Provide one versioned Helm chart at `deploy/helm/envy`, with a values schema,
an example values file, and a preflight command. Teams supply existing infrastructure
and secret references. The chart installs Envy's Deployment, ClusterIP Service,
service account, configuration, explicit RBAC, and control-plane network policy.
An optional chart-owned API routing object may attach to an existing gateway;
default to the team's existing proxy forwarding to the ClusterIP Service.

External PostgreSQL is required. No bundled database, mesh installation, demo
catalog, NodePort, Docker Desktop path, or secret generation runs by default.
Keep `make dev` as the reproducible local evaluation path.

Use a stable explicit installation ID across upgrades. Publish versioned server
images and chart artifacts with recorded digests and a tested compatibility matrix.
Keep one controller replica initially; the database advisory lock remains necessary
for rollout overlap. HA operation is not claimed by this milestone.

### Configuration and removal of local assumptions

Consolidate startup settings into typed configuration, retaining environment
variable compatibility and documenting exact precedence. Cover public API origin,
preview domain/scheme, ingress connection address, gateway identity/selector,
sidecar injection mode/revision, PostgreSQL TLS and secret references, namespace
prefix, limits/timeouts, registry access, and history retention. Sensitive values
stay in referenced Secrets, never installation-information responses or chart values.

Required implementation changes include:

- Accept configured HTTPS baseline and preview endpoints; remove hardcoded
  HTTP/port-80 and gateway-selector checks. Select one routing domain/ingress
  topology per installation initially. Existing resources remain borrowed.
- Separate the public hostname and TLS server name from an optional internal
  dial address for probes. Verify certificates, including configured private CAs;
  never use insecure TLS verification. Where auth protects preview ingress, use
  an explicit probe identity or documented private ingress route without a
  public auth bypass, and label the verification path accurately.
- Respect an operator-selected Istio injection revision instead of rewriting
  every namespace to `istio-injection=enabled`. Validate sidecars, connectivity,
  and mesh visibility; keep workload application protocols HTTP in this phase.
- Document existing wildcard DNS and TLS certificate coverage. The operator owns
  certificate issuance/rotation and gateway configuration; Envy validates them.
- Support approved workload credential references and imagePullSecrets without
  copying baseline credentials or accepting arbitrary Pod settings. In newly
  created composition namespaces, the team's existing secret-distribution
  controller must materialize approved references. Missing references yield a
  bounded prerequisite condition; Envy does not read/copy secret values. Keep
  permitted secret references operator-controlled even though catalog edits are
  available to every admitted user. Test the contract with namespace provisioning.

### External authentication boundary

The browser uses the public API/web origin through the team's authenticated proxy.
Named machine and scoped build credentials need a documented ingress path that
accepts bearer credentials without being redirected into browser login. Both paths
reach the same authoritative API; preview application access remains a separately
configured boundary and is never granted by baggage.

Before certifying this installation mode, fix and test the current edge cases:

- Any present malformed/duplicate Authorization header fails closed; it cannot
  be treated as an absent bearer and fall through to anonymous/proxy access.
- Proxy identity and proxy-secret headers must be singular and unambiguous;
  explicitly reject joined identity values and trusted-header spoofing.
- Validate browser mutation origins against the configured external scheme and
  authority. Protection must still apply when the upstream proxy strips cookies.
- Credential values cannot overlap across shared, machine, and scoped build
  credentials; machine IDs must be unique and system identities distinguishable.
- Require the trusted direct peer and proxy authentication mechanism together,
  plus a tested network restriction. Do not infer trust from X-Forwarded-For.
  Document proxy source NAT and the actual peer addresses observed in the cluster.

Supply an integration example for an existing OIDC proxy, with header stripping,
same-origin forwarding, and bearer routing. Envy does not implement login or
directory/group administration in this milestone.

### Preflight, installation, upgrade, and uninstall

Add `envy installation check --file installation.json` with structured JSON
and a useful exit status. Read-only checks discover required APIs, gateway and
namespace configuration, RBAC, mesh visibility, DNS/TLS reachability, PostgreSQL
connectivity/schema compatibility, and credential references. Mark inaccessible
or location-dependent checks unknown rather than passed. Network tests from an
operator laptop do not establish connectivity from the controller pod; supply an
explicit in-cluster check Job for that second perspective.

The documented flow is: preflight, install the pinned chart with supplied secret
references, verify session/installation, validate/apply an application catalog,
then explicitly create a smoke composition. Installing or viewing Envy never
creates an application composition.

Make database migrations an explicit command/Job under a migration lock with
version compatibility checks. Coordinate it with the existing startup migration
path so overlapping versions cannot race. Prefer additive migrations and declare
which old application version can still read the new schema. Helm rollback must
not be described as PostgreSQL rollback.

Document and test backup/restore of the database plus recovery of stable
installation identity and operator-owned credentials. A restored older database
can disagree with later Kubernetes resources: stop reconciliation during recovery
and require explicit inventory review before cleanup. Never infer orphanhood
from database unavailability or a missing row after restore.

Uninstall has two explicit paths: retain data-plane environments and PostgreSQL
for reinstallation, or destroy all compositions through Envy and verify cleanup
before removing the chart. No broad namespace deletion hook. Borrowed baseline,
Istio, proxy, certificate, and PostgreSQL resources are never uninstalled by Envy.
An interrupted cleanup remains visible and resumable.

### Installation acceptance gate

Use a fresh test cluster where infrastructure is provisioned separately, then
install only the chart. This models an existing cluster without creating a real
team's infrastructure during development.

- Install with external PostgreSQL, a nondefault Istio ingress selector and
  injection revision, HTTPS, and an authenticated proxy.
- Onboard the second application through catalog files. Prove genuine
  propagation with an instrumented contract and label ordinary HTTP probes only
  as reachability. Exercise private image/approved secret references.
- Verify human, named machine, and scoped CI access and their durable attribution;
  explicitly test proxy bypass, stripped cookies, duplicates, and invalid tokens.
- Run the evolving-composition gate, restart and upgrade the controller, preserve
  data-plane URLs, and test expired compositions after controller downtime.
- Test failed migrations, database loss, supported binary rollback, backup/restore,
  resource ownership conflicts, and both uninstall paths.
- Validate rendered chart schemas/RBAC and verify the network policy on a CNI that
  actually enforces it. Report unsupported policy enforcement as a limitation.

## Envy sequence

| Step | Deliverable | Completion evidence |
| --- | --- | --- |
| 2A | Desired/published/retiring state, migration, full-map and keyed-update contracts | Atomic persistence, migration and conflict tests |
| 2B | Add/remove/zero reconciliation and component retirement | Provider and restart tests plus real routing gate |
| 2C | Website, CLI, MCP, recipes and attributed diffs | Browser and actual CLI/MCP acceptance |
| 3A | Typed deployment config, HTTPS/ingress/mesh and secret-reference integration, auth fixes | Nondefault topology and authentication contract tests |
| 3B | Helm packaging, preflight, versioned artifacts and migration command | Fresh chart-only installation against separately prepared dependencies |
| 3C | Second-application runbook, upgrade/recovery/uninstall evidence | Repeatable external-installation acceptance report |

Start with 2A. Installation documentation and chart design can be refined while
the runtime work proceeds, but do not certify 3B before 3A's configuration and
authentication contracts pass. The previously deferred baseline ingress 503 is
not a reason to block planning; real routing acceptance must pass on a healthy
isolated baseline before these milestones are considered complete.

Write ADRs for evolving selection/retirement and installation ownership as part
of implementation. Update OpenAPI, update/catalog/routing/recipe docs and operator
runbooks with each delivered slice. Correct README performance and isolation claims
to match measured acceptance evidence before publishing release artifacts.

## Primary references used for installation design

- [Helm chart best practices](https://docs.helm.sh/docs/chart_best_practices/)
- [Helm chart and CRD lifecycle](https://helm.sh/docs/topics/charts/)
- [Istio secure ingress and certificate references](https://istio.io/latest/docs/tasks/traffic-management/ingress/secure-ingress/)
- [Kubernetes security checklist and network policy guidance](https://kubernetes.io/docs/concepts/security/security-checklist/)
