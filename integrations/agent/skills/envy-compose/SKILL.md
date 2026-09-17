---
name: envy-compose
description: Choose, reuse, save and recreate Envy environments for integration tasks in registered repositories. Coordinate exact builds and frontend commits through MCP or the Envy CLI; local-only tasks need no preview.
---

# Compose and verify with Envy

Read the repository's Envy instructions for project, baseline, components, image
build commands, frontend identity and tests. Discover missing catalog information
with `list_projects`, `list_components`, `get_component`, and `list_baselines`.
Never invent registered service bindings, credentials or deployment profiles.

## Decide whether compute is needed

Choose using the requested checks, not the presence of a PR. Documentation and
local-only changes normally need no composition. Frontend-only work can use the
baseline API when it is sufficient and the user intends that target; this is an
explicit baseline workflow, not a fallback from failed preview resolution.
Current frontend bindings require a composition, so do not create a dummy override
just to bind a frontend to baseline. Builds remain separate from deployment.

If a preview is needed, state its selected overrides, inherited components and
expiry. Use a lifetime appropriate to the task. A user request to perform an
integration task covers routine creation within that scope; do not repeatedly
ask for the same authorization. Do not allocate a preview for every PR by default.

## Choose message isolation independently of compute

Before creating or reusing a preview, inspect event schemas, publisher behavior,
known consumers and the intended checks. Honor an explicit user setting.
Otherwise set `message_isolation: true` for schema changes, changed event meaning
or publication conditions, unwanted baseline effects, or uncertain downstream
impact. Choose `false` only when messaging behavior is unchanged and baseline
processing is acceptable. State the decision and its reason; always submit an
explicit boolean rather than relying on omission.

Isolation requires registered Pub/Sub bindings, prepared baseline filters and
application instrumentation that propagates the isolation context and attributes.
Inspect that evidence; `MessagingReady` confirms infrastructure only. If the
integration is missing, report what must be onboarded rather than claiming the
preview is isolated or silently disabling isolation.

Do not deploy a consumer merely to enable isolation. For producer/payload checks,
use the returned subscription IDs and caller-owned Google credentials to run
`gcloud pubsub subscriptions pull SUBSCRIPTION --limit=10 --format=json` without
`--auto-ack`. Pulls lease messages and compete with running workers; acknowledgement
removes them from that subscription. If processing needs testing, run a local
consumer or add an approved consumer override through a generation-checked update.
Retain the existing producers; subscriptions and their backlog survive attachment.
An empty subscription binding disables consumption and must not fall back to the
baseline. Messages expire after their retention period; composition destruction
or TTL cleanup deletes remaining backlog. Reuse only a preview with the required
immutable isolation setting.

Report isolation mode and reason, subscription IDs, retention/expiry, infrastructure
conditions, and application checks separately. Report any possible backlog loss.

## Coordinate a composition

1. Use an explicit composition ID supplied by the task. Otherwise use
   `list_compositions` scoped to the project to find a candidate, and inspect it
   with `get_composition`. Names and branches are not unique identities. Do not
   modify another task's preview based only on a similar name.
2. Inspect desired overrides, baseline, readiness and remaining lifetime before
   reuse; only mutate a composition associated with this task. Discover registered
   source mappings with `list_source_repositories`; browse branches/commits and
   `resolve_source_revision` to obtain exact published builds. Select `build_id`
   when available, keeping each repository's revision independent. No matching
   build means use external CI or report the missing artifact, not select an older
   revision silently. Direct prebuilt immutable images remain supported. Never
   invent or supply server-owned `source` provenance.
3. Create with `create_composition` and a stable idempotency key for this request.
   Reuse the key only for identical input. For updates, fetch current generation,
   submit `update_composition` with `expected_generation`, and include the complete
   existing override set. Updates may add or remove approved components within the three-override limit;
   omitted keys return to baseline. Baseline, message isolation and TTL remain
   fixed and require a replacement to change. Preserve the complete desired
   override set, including existing producers when attaching a consumer.
   Account for both during overlap; do not evict another task to make space.
   One coordinator submits combined changes. On a generation conflict, inspect
   intervening changes rather than blindly resubmitting an outdated selection.
4. Wait with bounded `wait_for_composition` calls. On failure inspect conditions,
   `get_component_logs`, and `list_composition_events`; report actionable errors.
   Do not loop indefinitely. Cancelling a wait does not destroy the preview.
5. Inspect endpoint readiness and verification level. HTTP reachability does not
   prove selective routing or passing application tests. Inherited workloads and
   state remain live and shared; follow repository rules for test data and writes.

## Bind and build a frontend

Use the frontend repository's **full lowercase Git commit SHA**, independently of
backend branch names. Uncommitted frontend edits are not represented by that SHA;
make the revision reproducible before claiming a revision-specific deployment.

- `bind_frontend`: project, frontend name, revision, explicit composition ID and
  HTTPS repository URL. Identical retries are safe. Associations are immutable;
  resolve a conflict by inspecting the existing binding, never silently rebinding.
- `resolve_frontend`: bounded resolution of that exact association. Missing,
  unready, expired or deleted previews must not fall back to staging. An expiry
  or generation in a receipt is an observation, not a lease.
- Run the repository's frontend build/hosting workflow. The Cloudflare Pages
  adapter is `integrations/cloudflare-pages/build.mjs` in the Envy repository.
  Pass the resolved public API URL into the configured public build variable;
  never copy Envy credentials into browser configuration.
- `publish_frontend`: record the caller-reported deployment URL using the binding's
  expected version. A successful build or reported URL does not prove hosting.
  On a version conflict, inspect current state before retrying.

Different frontends or revisions can share one composition without new backend
workloads. Use distinct names for different frontend repositories. They will all
observe later backend updates. Simultaneous independent backend variants require
separate compositions. External frontend hosting/CORS rules still apply.

## Save and resume

Use `export_recipe` (CLI: `envy recipe export ID`) to save exact backend
intent, optionally selecting existing frontend name/revision pairs. Default export
includes no frontends: choose them explicitly rather than exporting all history.
Save the structured JSON to the task's agreed location outside its runtime.
Recipes require immutable direct images or build IDs; tag-based exports fail
rather than claiming reproducibility. Build IDs belong to the same installation.

Tomorrow, inspect the task's saved composition ID first. Reuse only if it is still
suitable and has enough lifetime. Otherwise `validate_recipe` checks structure and
`recreate_recipe` creates a new composition through REST, requiring a fresh stable
idempotency key for this recreation. Use that same key for retries. A changed
baseline binding revision blocks creation: inspect the current binding and amend
the saved selection explicitly. Missing builds need external restoration or an
intentional new selection. Inherited staging and shared data remain live.

Recreation returns a new ID/URL and fresh binding identities for saved frontends.
Inspect `binding_errors`: composition creation and binding are separate writes;
retry partial binding failures with the same key. Wait for readiness, resolve the
returned binding keys and rebuild frontends that embed the URL. Old browser
checks and published URLs are not transferred. Envy does not suspend/restore
workload memory, renew TTL, host the frontend or revive an expired URL on access.

## Verify and report

Open the deployed frontend in a real browser. Exercise its API path and the
requested user flow; check HTTPS/mixed content, CORS, authentication, cookies and
login callbacks where the application uses them. Record only checks actually
performed. An HTTP-only demo cannot establish production TLS or login behavior.

Re-read the binding and composition before `report_frontend_check`. Supply the
expected binding version and the composition generation used by the test, status
`passed` or `failed`, and a concise evidence summary including limitations. If the
backend changed during testing, repeat the affected checks. A changed frontend
URL clears old evidence; backend changes make evidence stale.

Return composition ID, generation, expiry, API URL, frontend URL, verification
level, and application-check evidence separately. Preserve IDs for later reuse.
Use `destroy_composition` for explicitly owned previews when cleanup is requested
or part of the task. Envy does not delete external hosting. Source-control writes,
PR comments and messages require the user's authorization through those tools.
