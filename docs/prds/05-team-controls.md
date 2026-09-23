# PRD 05: Team permissions and resource governance

Status: proposed. Priority: before supported fleet operation.
Contract: [Shared boundaries](product-boundaries.md).
Evidence baseline: [reviewed commit](README.md#evidence-baseline).

## Problem, users and current state

Platform owners need to delegate preview use without delegating approval of
execution profiles or administration of every installation. Developers and agents
need predictable project access and capacity errors rather than broad credentials.

Implemented evidence:

- [Authentication](../../internal/api/auth.go) and
  [authentication tests](../../internal/api/auth_test.go) enforce current entry
  credentials. [Session/OAuth implementation](../../internal/authn/server.go) and
  [tests](../../internal/authn/server_test.go) provide browser and agent sessions.
- [Application service](../../internal/application/service.go) and
  [admission persistence](../../internal/persistence/postgres/store.go) already
  constrain creation and lifetime. Kubernetes quota enforcement exists separately.
- [Authentication documentation](../authentication-and-activity.md) explicitly
  describes one admitted organisation with broad management access and no role
  hierarchy. Existing authentication is not project authorization.

## First milestone and journey

M1 adds explicit project membership/roles, scoped automation credentials and
transactional project admission limits. An installation administrator enables
scoped access after reviewing the migration. A project operator prepares a baseline;
a developer creates and manages team previews; a viewer inspects permitted evidence.

## Requirements and interfaces

- **TEAM-01:** Enforce roles server-side on every project-scoped read and write,
  including catalog, builds, recipes, frontend bindings, logs and evidence. List
  and search results must not leak inaccessible projects. UI controls are a
  convenience, not the authorization boundary.
- **TEAM-02:** Use the role matrix below. Membership/grant and target administration
  are installation-admin actions. Project operators can approve execution profiles
  and dependency bindings but cannot grant themselves installation privileges.
  Actors without a project grant have no project access in scoped mode.
- **TEAM-03:** Scope machine/CI credentials by allowed project and operation, with
  explicit expiry and revocation. A user-derived agent grant cannot exceed its
  user's current permissions. Re-evaluate scope on each command; neither refresh
  nor a cached fleet session can restore a removed grant. Keep existing specialized
  build-reporting constraints rather than replacing them with broader access.
- **TEAM-04:** Persist project admission limits for active compositions, reserved
  workload CPU/memory and maximum lifetime, within installation caps. Reserve
  atomically with intent and count pending work, concurrent Jobs and replacement
  overlap. Count resources pending cleanup until absence is confirmed. Limits are
  resource budgets, not estimated billing or measured utilization.
- **TEAM-05:** Capacity exhaustion returns the violated budget, scope and next
  action. Reducing a budget below current usage blocks increases without silently
  evicting previews. Deletion, diagnostics and recovery remain available. Keep
  the component limit unchanged until PRD 06 qualifies an increase.
- **TEAM-06:** Preserve verified initiator, credential identity and project scope
  on operations and audit events. Show current owner, expiry and pending cleanup.
  Removing a user's access does not abandon owned resources or disable automatic
  cleanup; another authorized project actor can manage team previews.
- **TEAM-07:** Provide a reviewable transition from legacy trusted access to scoped
  access. Seed an explicit installation administrator and review project grants
  before activation. Preserve legacy behaviour until activation; after activation
  do not silently fall back to broad access. Revoked rights take effect for future
  commands, while already accepted lifecycle/cleanup work retains durable authority.

| Role | Allowed actions within scope |
| --- | --- |
| Viewer | Read project catalog, previews, permitted logs and evidence |
| Developer | Viewer actions plus create/update/rerun/cancel/destroy project previews and manage their recipes/frontend bindings |
| Project operator | Developer actions plus register catalog, approve profiles/bindings and manage project limits within installation caps |
| Installation administrator | Manage memberships, credentials, installation caps and target registration/authorization; administer projects |

M1 needs membership/role and scoped-credential records, authorization context at
the application boundary, and transactional resource reservations. Keep claims
about ownership separate from authorization: developers collaborate within their
granted project rather than being limited to resources they personally created.
Expose effective capabilities and budget usage through REST, CLI, MCP and UI.

## Acceptance

- [ ] **TEAM-A1 / TEAM-01, TEAM-02:** Use two projects and each role to exercise
  direct APIs plus CLI/MCP/UI. A developer cannot approve a profile, a viewer
  cannot mutate, and no actor can list or fetch another project's data without a
  grant. Project operators cannot alter installation grants or targets.
- [ ] **TEAM-A2 / TEAM-03, TEAM-06:** Revoke/expire user and machine scopes during
  an agent session. New commands fail, token refresh cannot restore rights,
  accepted cleanup continues, and audit attribution remains the original actor.
- [ ] **TEAM-A3 / TEAM-04, TEAM-05:** Race creation and updates at the project cap,
  including rollout overlap and active Jobs. Only admitted work proceeds. Pending
  cleanup retains its reservation; observed absence releases it exactly once.
- [ ] **TEAM-A4 / TEAM-05, TEAM-06:** Lower a quota below use, remove a preview's
  creator and restart. Existing resources are not evicted or orphaned; permitted
  team members can inspect and destroy them.
- [ ] **TEAM-A5 / TEAM-07:** Upgrade legacy records, review grants and activate
  scoped mode without losing admin access. Existing HTTP payloads still work for
  authorized users; a configuration/version mismatch must not bypass enforcement.

## Dependencies, rollout and exclusions

Ship authorization storage and server checks before client role management. Test
activation with existing password, Google, token and trusted-proxy modes. Scoped
mode requires attributable authenticated principals; anonymous/dev access must not
be presented as organisational authorization. Retain explicit local evaluation mode.

Early target design adds scope without granting remote rights. Fleet requires
explicit local target authorization as described in [PRD 07](07-fleet-management.md).
Report access denials and quota pressure using bounded operational categories.

No hostile tenant isolation, customer billing, generic policy language, directory
provisioning or automatic access grants based solely on a cloud account or email
domain. Broader corporate identity integrations can be separate adapters later.
