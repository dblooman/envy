# Cloudflare Pages + Envy Preview Example

This directory demonstrates how a frontend application deployed to Cloudflare Pages automatically discovers and connects to its corresponding ephemeral Envy backend composition at build time.

## How It Works

1. **Backend Preview Creation**:
   When a PR is opened modifying backend services (e.g. `graphql` or `backend`), an Envy composition is created with overrides.
   ```bash
   delivery composition create \
     --project ecommerce \
     --name pr-42-checkout \
     --override graphql=ghcr.io/org/graphql:sha-123456 \
     --revision graphql=org/graphql@123456#42
   ```

2. **Cloudflare Pages Build Step**:
   In Cloudflare Pages dashboard (or `wrangler.toml`), the build command is set to:
   ```bash
   ./adapters/cloudflare-pages/build.sh npm run build
   ```
   or using the Envy CLI:
   ```bash
   delivery frontend run -- npm run build
   ```

3. **Automatic Binding**:
   - The build adapter detects the current git commit SHA or branch (`CF_PAGES_COMMIT_SHA` / `CF_PAGES_BRANCH`).
   - Queries Envy API to find the active composition.
   - Waits for backend readiness.
   - Injects `VITE_GRAPHQL_URL` and `API_URL` pointing to the preview ingress endpoint.
   - Runs `npm run build`.
   - Records the Cloudflare Pages URL back against the composition, displaying the link in the Envy dashboard and API.
   - If no active composition is found, the build fails immediately to prevent dangerous silent fallback to staging or production.
