---
name: envy-compose
description: Create or reuse Envy application previews, bind exact frontend commits, diagnose readiness, and report browser evidence through Envy MCP or delivery CLI. Use when developing or testing a repository registered with Envy; builds, hosting and tests run through the caller's existing tools.
---

# Compose and verify with Envy

Read the repository's Envy instructions for project, baseline, components, image
build commands, frontend identity and tests. Discover missing catalog information
with `list_projects`, `list_components`, `get_component`, and `list_baselines`.
Never invent registered service bindings, credentials or deployment profiles.

## Coordinate a composition

1. Use an explicit composition ID supplied by the task. Otherwise use
   `list_compositions` scoped to the project to find a candidate, and inspect it
   with `get_composition`. Names and branches are not unique identities. Do not
   modify another task's preview based only on a similar name.
2. Build and load/push images with the repository's existing commands. Envy
   accepts prebuilt images; it does not build code or run agents. Prefer immutable
   image digests, recording source repository and commit when available.
3. Create with `create_composition` and a stable idempotency key for this request.
   Reuse the key only for identical input. For updates, fetch current generation,
   submit `update_composition` with `expected_generation`, and include the complete
   existing override set. The initial update interface cannot add override keys.
   One coordinator should submit combined changes from multiple contributors.
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
