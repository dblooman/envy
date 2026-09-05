# REST and MCP contracts

REST is authoritative. The stdio MCP server uses the same private HTTP client
that a future CLI will use. The formal contract is
[`api/openapi.yaml`](../api/openapi.yaml). JSON field names use snake_case.

All `/v1` routes require `Authorization: Bearer <token>`. Health endpoints contain
no catalog or composition data and do not require a token. The local API listens
on `http://127.0.0.1:8081`.

| Method | Path | Result |
| --- | --- | --- |
| GET | `/v1/projects` | Seeded project discovery |
| GET | `/v1/projects/{project}/components` | Project-scoped component catalog |
| GET | `/v1/projects/{project}/components/{component}` | Component profile |
| GET | `/v1/projects/{project}/baselines` | Registered baselines |
| POST | `/v1/compositions` | Persist create intent; 202 composition |
| GET | `/v1/compositions` | Bounded composition list |
| GET | `/v1/compositions/{id}` | Full desired and observed state |
| GET | `/v1/compositions/{id}/status` | Lifecycle and latest operation |
| GET | `/v1/compositions/{id}/endpoints` | Allocated endpoint and readiness |
| DELETE | `/v1/compositions/{id}` | Persist repeatable deletion intent; 202 composition |

Catalog writes, updates, standalone operation endpoints, logs, events, and a
public Go SDK are deferred. A future PATCH will require the expected generation
and reject stale changes with 409.

## Create and retry

```http
POST /v1/compositions
Authorization: Bearer <token>
Content-Type: application/json
Idempotency-Key: demo-service-b-v2-001

{
  "project": "demo",
  "baseline": "staging",
  "name": "service-b-v2",
  "overrides": { "service-b": { "image": "envy/service-b:v2" } },
  "ttl": "8h"
}
```

The response is the full composition, including stable `id`, desired
`generation`, `phase`, `latest_operation.id`, and `endpoints.public`. `Location`
identifies the polling URL. The preview URL may be allocated immediately, but
`endpoints.public.ready` remains false until observed request verification.

`Idempotency-Key` is optional, at most 128 printable ASCII characters. Replaying
the same logical request with the same key returns its original composition;
different request content with the same key returns 409. Keys remain associated
with retained tombstones. An omitted TTL means `8h`; explicit TTLs must be
positive and no greater than the configured maximum (`24h` by default).

Only the seeded demo service-b image override is supported initially. Resource
overrides and unknown strategies are rejected before any provider mutation.
Live compositions are capped at twenty by default, including compositions still
being destroyed.

## Observations and errors

Composition fields are `id`, `project`, `baseline`, `baseline_revision`, `name`,
`overrides`, `generation`, `observed_generation`, `phase`, `expires_at`,
`created_at`, `updated_at`, `components`, `endpoints`, `conditions`,
`latest_operation`, and optional `last_error`. Component observations expose
`source`, `status`, `image`, and optional `workload_id`. Endpoint readiness is
independent of URL allocation. Conditions carry `type`, boolean `status`, and
`message`.

Status returns `id`, `phase`, `generation`, `observed_generation`, `conditions`,
optional `last_error`, and `latest_operation`. Endpoints returns `id` and
`endpoints`. Operation fields are `id`, `kind`, `status`, and optional `error`.

Errors use this stable envelope:

```json
{
  "error": {
    "code": "validation_error",
    "message": "only service-b can be overridden",
    "retryable": false
  }
}
```

The API distinguishes malformed or unsupported requests (400), authentication
failure (401), unknown IDs (404), idempotency/state conflict (409), capacity
limits (429), and unavailable dependencies (503). Error messages must not expose
tokens or connection strings. Provider failures remain visible on the persisted
composition, and recoverable failures continue to reconcile.

Lists return `{ "items": [], "next_cursor": "..." }`. `limit` defaults to 20
and is bounded to 1–100. Pass `after=<next_cursor>` to fetch subsequent items.
Cursors identify the last returned ID; an absent/empty cursor means no next page.
Composition lists support the `project` filter.

## MCP

`cmd/mcp` implements the official Go SDK's stdio transport. Configure the REST
URL and bearer token in its environment. Diagnostics go to stderr; stdout is
reserved for protocol messages.

`make dev` builds `.envy/bin/envy-mcp`. Configure `ENVY_API_URL` (default
`http://127.0.0.1:8081`) and `ENVY_API_TOKEN_FILE` (for local development,
`.envy/envy-dev/api-token`). `ENVY_API_TOKEN` is an alternative; the file takes
precedence when both are provided.

```sh
ENVY_API_TOKEN_FILE="$PWD/.envy/envy-dev/api-token" .envy/bin/envy-mcp
```

| Tool | Input | Structured result |
| --- | --- | --- |
| `create_composition` | Create fields and optional `idempotency_key` | Full composition |
| `get_composition` | `id` | Full composition |
| `wait_for_composition` | `id`, optional `timeout_seconds` (default 30, maximum 60) | Latest composition |
| `get_composition_endpoints` | `id` | ID and endpoints |
| `destroy_composition` | `id` | Full composition with deletion status |

Tools have typed input/output schemas and a concise text compatibility result.
Waiting polls REST, returns the latest status at timeout, and stops on terminal
states. Cancelling a wait never requests deletion. Actual MCP-client acceptance
must create, wait, fetch endpoints, and destroy against the running API.
