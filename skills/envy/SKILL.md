---
name: envy
description: Manage isolated, cost-effective preview environments and compositions with Envy using baseline routing and multi-component overrides.
---

# Envy Preview Platform Skill

Use this skill when creating, updating, inspecting, binding, or destroying ephemeral preview environments with Envy.

Envy creates preview environments by running **only the services modified in your branch or PR** alongside a shared baseline staging cluster. Request routing and context propagation (e.g. `x-envy-composition`) ensure traffic reaches your overridden services while all other calls route to shared baseline infrastructure.

---

## Agent Workflow Overview

```
1. Discover ──► 2. Resolve Images ──► 3. Create or Update ──► 4. Wait & Verify ──► 5. Bind Frontend ──► 6. Report
```

1. **Discover**: Read machine-readable `.envy/project.json` (or inspect via `list_compositions` / `lookup_composition`).
2. **Resolve Images**: Ensure pre-built container images exist for your branch/commit in the project's registry.
3. **Create or Update**: Call `create_composition` (or `update_composition`) passing overrides and git revision metadata.
4. **Wait & Verify**: Wait for phase `ready` and verify end-to-end request routing (`RequestRoutingVerified`).
5. **Bind Frontend**: If building or deploying a frontend (e.g., Cloudflare Pages), use `delivery frontend run` or `record_frontend_url`.
6. **Report**: Share the preview URL and verification summary with the user or pull request.

---

## Step-by-Step Instructions

### Step 1: Discover Project & Component Identity

Do not guess component names. Always check local configuration first:
- Check for `.envy/project.json`, `envy.json`, or `.envy/config.json`.
- Extract:
  - `project`: Project ID (e.g., `ecommerce`)
  - `component`: The component built by the current repository (e.g., `graphql`, `backend`)
  - `baseline`: Target baseline (defaults to `staging`)
  - `entrypoint`: Public ingress entrypoint component (e.g., `graphql`)

If checking whether a preview composition is already running for the current branch or commit:
- Use MCP tool `lookup_composition` with `commit_sha`, `branch`, or `pr_number`.
- Alternatively, run:
  ```bash
  delivery composition lookup --project <project> --commit-sha <sha> --branch <branch>
  ```

---

### Step 2: Create a Preview Composition

When a new branch or PR is created:
1. Ensure image(s) are built and pushed to the container registry.
2. Call MCP tool `create_composition`:
   - `project`: Project ID from config
   - `name`: Human-readable identifier (e.g. `pr-42-checkout-flow`)
   - `overrides`: Map of component names to image tags, e.g.:
     ```json
     {
       "graphql": {"image": "ghcr.io/org/graphql:sha-a1b2c3d"},
       "backend": {"image": "ghcr.io/org/backend:sha-e5f6g7h"}
     }
     ```
   - `revisions`: Map of component revision metadata:
     ```json
     {
       "graphql": {
         "repo": "org/graphql",
         "branch": "feat/checkout",
         "commit_sha": "a1b2c3d4e5f6",
         "pr_number": "42"
       }
     }
     ```
   - `ttl`: Lease duration (e.g., `"2h"`, `"8h"`). Defaults to server policy (typically 4 hours).
   - `idempotency_key`: Stable key based on branch or commit (e.g., `pr-42-create`).

Alternatively via CLI:
```bash
delivery composition create \
  --project ecommerce \
  --name pr-42-checkout \
  --override graphql=ghcr.io/org/graphql:sha-a1b2c3d \
  --override backend=ghcr.io/org/backend:sha-e5f6g7h \
  --revision graphql=org/graphql@a1b2c3d#42 \
  --ttl 4h
```

---

### Step 3: Wait for Readiness & Verify Ingress

Never report success before the composition has fully provisioned and passed ingress verification.

1. Call MCP tool `wait_for_composition`:
   - `id`: The composition ID returned by create (e.g. `cmp-abc12345`)
   - `timeout_seconds`: Up to 60 seconds
2. Check `phase`: Must be `ready`.
3. Check `conditions`:
   - `WorkloadsReady`: All overridden workloads are deployed and healthy.
   - `RoutingActive`: Dynamic mesh routing is configured.
   - `IngressConfigured`: Wildcard preview ingress is bound to the public entrypoint.
   - `RequestRoutingVerified`: End-to-end request chain flow test passed through the entrypoint and overridden services.
4. Retrieve the allocated public URL using `get_composition_endpoints`:
   - `endpoints.public.url`: e.g. `https://cmp-abc12345.preview.example.com`

---

### Step 4: Frontend Binding & Cloudflare Pages Integration

If the project includes a frontend application (e.g. Cloudflare Pages, Next.js, Vite):
The frontend must receive the backend composition URL (`VITE_GRAPHQL_URL` or `NEXT_PUBLIC_GRAPHQL_URL`) at build time.

#### Option A: Build Wrapper (Recommended for CI / Agents)
Use the Envy frontend build wrapper in the build command:
```bash
delivery frontend run -- npm run build
```
This automatically:
- Resolves the active backend preview composition using git commit/branch.
- Waits for backend readiness if not yet ready.
- Injects `VITE_GRAPHQL_URL`, `NEXT_PUBLIC_GRAPHQL_URL`, `GRAPHQL_URL`, and `API_URL`.
- Executes the build command.
- If no active backend composition exists, **fails immediately with a clear error** to prevent silent fallback to production or staging.

#### Option B: Record Deployed Frontend URL
Once the frontend preview is deployed to its host (e.g., `https://pr-42.my-app.pages.dev`):
Call MCP tool `record_frontend_url`:
- `id`: Composition ID
- `frontend_url`: `https://pr-42.my-app.pages.dev`

This binds the external frontend to the composition, making it visible in the Envy dashboard and API.

---

### Step 5: Updating Existing Compositions

When new commits are pushed to an open PR:
Do NOT destroy and recreate the composition. Use `update_composition` to preserve the stable preview URL:
1. Fetch current `generation` with `get_composition`.
2. Call MCP tool `update_composition`:
   - `id`: Composition ID
   - `expected_generation`: Current generation (prevents race conditions)
   - `overrides`: Updated component images
   - `revisions`: Updated commit SHAs

---

### Step 6: Destroying Compositions

When a PR is closed or testing is finished:
- Call MCP tool `destroy_composition`:
  - `id`: Composition ID
- All preview pods, Envoy routing filters, and DNS entries are cleanly unmapped, leaving shared baseline staging unaffected.

---

## Troubleshooting & Diagnostics

If a preview environment fails or behaves unexpectedly:

1. **Check Composition Events**:
   Call MCP tool `get_composition_events` to inspect reconciliation errors.
2. **Fetch Workload Logs**:
   Call MCP tool `get_composition_logs` for specific components:
   - For an overridden component: returns logs from your preview pod.
   - For an inherited component: returns shared baseline logs tagged with `shared-baseline`.
3. **Run Envy Doctor**:
   ```bash
   delivery doctor --project <project> --probe
   ```
   Verifies API reachability, baseline catalog registration, ingress DNS wildcarding, and end-to-end request routing.
