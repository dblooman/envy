#!/usr/bin/env bash
set -euo pipefail

# ==============================================================================
# Envy Cloudflare Pages Build Adapter
#
# Connects frontend preview builds to their corresponding Envy backend preview
# composition. Injects VITE_GRAPHQL_URL, NEXT_PUBLIC_GRAPHQL_URL, and API_URL.
# ==============================================================================

# If delivery CLI is installed, delegate directly to the native wrapper
if command -v delivery >/dev/null 2>&1; then
  echo "==> Using Envy CLI frontend adapter"
  exec delivery frontend run -- "$@"
fi

ENVY_API_URL="${ENVY_API_URL:-http://127.0.0.1:8081}"
ENVY_API_TOKEN="${ENVY_API_TOKEN:-}"

# Resolve commit and branch from environment or git
COMMIT_SHA="${CF_PAGES_COMMIT_SHA:-${GITHUB_SHA:-}}"
BRANCH="${CF_PAGES_BRANCH:-${GITHUB_HEAD_REF:-${GITHUB_REF_NAME:-}}}"
PR_NUMBER="${GITHUB_PR_NUMBER:-${PR_NUMBER:-}}"

if [ -z "$COMMIT_SHA" ] && command -v git >/dev/null 2>&1; then
  COMMIT_SHA="$(git rev-parse HEAD 2>/dev/null || true)"
fi
if [ -z "$BRANCH" ] && command -v git >/dev/null 2>&1; then
  BRANCH="$(git branch --show-current 2>/dev/null || true)"
fi

echo "==> Envy Cloudflare Pages Build Adapter"
echo "    Commit: ${COMMIT_SHA:-unknown}"
echo "    Branch: ${BRANCH:-unknown}"
echo "    Envy API: ${ENVY_API_URL}"

if [ -z "$COMMIT_SHA" ] && [ -z "$BRANCH" ] && [ -z "$PR_NUMBER" ]; then
  echo "ERROR: Unable to detect git commit SHA, branch name, or PR number." >&2
  echo "Ensure CF_PAGES_COMMIT_SHA or GITHUB_SHA is set." >&2
  exit 1
fi

AUTH_HEADER=""
if [ -n "$ENVY_API_TOKEN" ]; then
  AUTH_HEADER="Authorization: Bearer ${ENVY_API_TOKEN}"
fi

# Query Envy API for active composition
LOOKUP_QUERY="commit_sha=${COMMIT_SHA}&branch=${BRANCH}&pr=${PR_NUMBER}"
LOOKUP_URL="${ENVY_API_URL%/}/v1/compositions/lookup?${LOOKUP_QUERY}"

HTTP_RESPONSE="$(curl -s -w "\n%{http_code}" ${AUTH_HEADER:+-H "$AUTH_HEADER"} "$LOOKUP_URL" || true)"
HTTP_CODE="$(echo "$HTTP_RESPONSE" | tail -n1)"
RESPONSE_BODY="$(echo "$HTTP_RESPONSE" | sed '$d')"

if [ "$HTTP_CODE" != "200" ]; then
  echo "" >&2
  echo "==============================================================================" >&2
  echo "ERROR: No active Envy composition found for commit ${COMMIT_SHA} (branch: ${BRANCH})." >&2
  echo "Status code: ${HTTP_CODE}" >&2
  echo "Details: ${RESPONSE_BODY}" >&2
  echo "" >&2
  echo "Preview frontend builds must be connected to an active backend preview." >&2
  echo "Please create or update the backend preview composition before building:" >&2
  echo "  delivery composition create --name <name> --override <comp>=<image>" >&2
  echo "==============================================================================" >&2
  exit 1
fi

COMP_ID="$(echo "$RESPONSE_BODY" | grep -o '"id":"[^"]*' | head -n1 | cut -d'"' -f4)"
PUBLIC_URL="$(echo "$RESPONSE_BODY" | grep -o '"url":"[^"]*' | head -n1 | cut -d'"' -f4)"
COMP_GEN="$(echo "$RESPONSE_BODY" | grep -o '"generation":[0-9]*' | head -n1 | cut -d':' -f2)"

if [ -z "$PUBLIC_URL" ]; then
  echo "ERROR: Active composition ${COMP_ID} has not allocated a public endpoint." >&2
  exit 1
fi

GRAPHQL_URL="${PUBLIC_URL%/}/graphql"

echo "==> Connected to backend composition: ${COMP_ID}"
echo "    Public URL:  ${PUBLIC_URL}"
echo "    GraphQL URL: ${GRAPHQL_URL}"

# Export environment variables for Vite, Next.js, and general frontends
export VITE_GRAPHQL_URL="$GRAPHQL_URL"
export NEXT_PUBLIC_GRAPHQL_URL="$GRAPHQL_URL"
export GRAPHQL_URL="$GRAPHQL_URL"
export VITE_API_URL="$PUBLIC_URL"
export NEXT_PUBLIC_API_URL="$PUBLIC_URL"
export API_URL="$PUBLIC_URL"
export ENVY_COMPOSITION_ID="$COMP_ID"
export ENVY_COMPOSITION_URL="$PUBLIC_URL"

# Record external frontend URL if CF_PAGES_URL is available
if [ -n "${CF_PAGES_URL:-}" ] && [ -n "$COMP_ID" ] && [ -n "$COMP_GEN" ]; then
  echo "==> Recording Cloudflare Pages URL against composition: ${CF_PAGES_URL}"
  curl -s -X PATCH "${ENVY_API_URL%/}/v1/compositions/${COMP_ID}" \
    ${AUTH_HEADER:+-H "$AUTH_HEADER"} \
    -H "Content-Type: application/json" \
    -d "{\"expected_generation\": ${COMP_GEN}, \"frontend_url\": \"${CF_PAGES_URL}\"}" >/dev/null || true
fi

# Execute build command
if [ "$#" -gt 0 ]; then
  echo "==> Executing build command: $*"
  exec "$@"
else
  echo "==> Executing default build: npm run build"
  exec npm run build
fi
