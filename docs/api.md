# REST and MCP contracts

REST is authoritative. The stdio MCP server uses the same private HTTP client
used by the Envy CLI. The formal contract is
[`api/openapi.yaml`](../api/openapi.yaml). JSON field names use snake_case.

All `/v1` routes require an admitted identity. The compatible default uses
`Authorization: Bearer <token>`; browser sessions, Google login, dev, proxy, and anonymous modes are
documented in [authentication and activity](authentication-and-activity.md).
Health endpoints contain no catalog or composition data and do not require a
token. The local API and website listen on `http://127.0.0.1:8081`.

| Method | Path                                                | Result                                                         |
| ------ | --------------------------------------------------- | -------------------------------------------------------------- |
| POST   | `/v1/catalog/validate`                              | Read-only configuration and live connectivity checks           |
| POST   | `/v1/catalog/apply`                                 | Atomic, repeatable configuration registration                  |
| GET    | `/v1/projects`                                      | Registered project discovery                                   |
| POST   | `/v1/projects`                                      | Register immutable project; 201                                |
| POST   | `/v1/projects/{project}/components`                 | Register approved component profile; 201                       |
| POST   | `/v1/projects/{project}/baselines`                  | Validate and register existing baseline; 201                   |
| GET    | `/v1/projects/{project}/components`                 | Project-scoped component catalog                               |
| GET    | `/v1/projects/{project}/components/{component}`     | Component profile                                              |
| GET    | `/v1/projects/{project}/baselines`                  | Registered baselines                                           |
| POST   | `/v1/compositions`                                  | Persist create intent; 202 composition                         |
| GET    | `/v1/compositions`                                  | Bounded composition list                                       |
| GET    | `/v1/compositions/{id}`                             | Full desired and observed state                                |
| GET    | `/v1/compositions/{id}/status`                      | Lifecycle and latest operation                                 |
| GET    | `/v1/compositions/{id}/endpoints`                   | Allocated endpoint and readiness                               |
| GET    | `/v1/compositions/{id}/components/{component}/logs` | Bounded application container log snapshot                     |
| GET    | `/v1/compositions/{id}/events`                      | Paginated durable lifecycle history                            |
| PATCH  | `/v1/compositions/{id}`                             | Persist image update with expected generation; 202 composition |
| DELETE | `/v1/compositions/{id}`                             | Persist repeatable deletion intent; 202 composition            |
| GET    | `/v1/session`                                       | Effective authenticated identity and mode                      |
| GET    | `/v1/installation`                                  | Sanitized installation settings                                |
| GET    | `/v1/activity`                                      | Filtered, cursor-paginated operational history                 |
| GET    | `/v1/compositions/{id}/revisions`                   | Immutable desired-state generations                            |
| POST   | `/v1/recipes/export`                                | Export a portable recipe                                       |
| POST   | `/v1/recipes/validate`                              | Validate a portable recipe                                     |
| POST   | `/v1/recipes/recreate`                              | Idempotently recreate a recipe and its selected bindings       |

Catalog registration is described below. Standalone operation endpoints and a public Go SDK are deferred. The CLI is documented in [CLI usage](cli.md).

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

Zero to three approved, bound component image overrides are accepted. An empty
`overrides` object creates a preview URL that inherits the complete baseline.
Resource
overrides and unknown strategies are rejected before any provider mutation.
Live compositions are capped at twenty by default, including compositions still
being destroyed.

## Update

```http
PATCH /v1/compositions/<id>
Authorization: Bearer <token>
Content-Type: application/json

{
  "expected_generation": 1,
  "overrides": { "service-b": { "image": "envy/service-b:v3" } }
}
```

A ready or failed, unexpired composition accepts a new image generation and
operation with 202, `phase: updating`, and endpoint readiness false. Its ID,
URL, expiry, and registered baseline bindings remain stable. The response
includes a polling `Location`. Only `expected_generation` and the complete
`overrides` map are accepted; it can add or remove approved component overrides,
and `{}` restores complete baseline inheritance. Other fields are rejected.
Missing/nonpositive generations and unsupported overrides return 400. Stale
generations, an active rollout, expiry, and deletion return 409.

The same image still increments the generation and triggers verification but
no forced restart. Repeating PATCH with an old generation returns 409. After
an uncertain response, GET the composition to inspect its desired state before
retrying. Rolling deployment can serve either the old or new override until
convergence. Unavailable images can leave the previous override serving, never
the baseline as a fallback. Failed updates can be repaired with a new update.
See [update lifecycle](updates.md) for the readiness and persistence guarantees.

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
    "message": "at most three component overrides are allowed",
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

## Logs and events

```http
GET /v1/compositions/<id>/components/gateway/logs?tail_lines=200&max_bytes=65536
GET /v1/compositions/<id>/events?limit=20
GET /v1/compositions/<id>/events?limit=20&after=<next_cursor>
```

Log responses contain `id`, `project`, `component`, `source`,
`composition_filtered: false`, `message`, `streams`, `truncated`, and `partial`.
Each stream contains `pod`, `workload_id`, `container`, `text`, `truncated`, and
an optional structured `error`. Inherited components use `source: shared-baseline`
and include traffic from other compositions. Override logs use `source: override`.
Limits are 1–1000 lines per pod (default 200), 1–262144 bytes total (default 65536),
and at most three pods. `since_seconds` optionally selects the previous 1–86400
seconds; `previous=true` reads the last terminated application container instance.
No arbitrary pod/container selectors or streaming are supported.

Empty pod inventories return an empty list. Individual pod read errors produce
`partial: true`, even when all streams fail. Limits can truncate a snapshot.
Destroyed compositions return 409 for logs. Logs are not persisted by Envy.

Event pages use `items` and optional `next_cursor`, oldest first. Event IDs and
cursors are decimal strings. Events include composition, project, generation,
phase, operation, conditions, timestamp, type, and optional error. Types are
`snapshot`, `create_requested`, `update_requested`, `destroy_requested`, `expired`,
and `observation_changed`. History survives destruction. The migration records
current snapshots for existing compositions; earlier history is unavailable.
Malformed/repeated/unknown query options return 400. See [diagnostic guarantees](diagnostics.md).

## MCP

`cmd/mcp` implements the official Go SDK's stdio transport. Configure `ENVY_API_URL` (default `http://127.0.0.1:8081`). Dev mode needs no credentials. For password or Google installations, run `envy auth login` first; the local adapter shares the saved credentials. Explicit `ENVY_API_TOKEN_FILE` and `ENVY_API_TOKEN` remain available for machines. Diagnostics go to stderr; stdout is reserved for protocol messages.

`make dev` builds `.envy/bin/envy-mcp`:

```sh
ENVY_API_URL=http://127.0.0.1:8081 .envy/bin/envy-mcp
```

Remote clients use Streamable HTTP at `/mcp`, with OAuth discovery, S256 PKCE, browser login/consent, and resource-bound Envy credentials. See [authentication and sessions](authentication-and-activity.md) for endpoints, registration, lifetimes, and configuration.

| Tool                        | Input                                                                              | Structured result                     |
| --------------------------- | ---------------------------------------------------------------------------------- | ------------------------------------- |
| `create_composition`        | Create fields and optional `idempotency_key`                                       | Full composition                      |
| `get_composition`           | `id`                                                                               | Full composition                      |
| `wait_for_composition`      | `id`, optional `timeout_seconds` (default 30, maximum 60)                          | Latest composition                    |
| `get_composition_endpoints` | `id`                                                                               | ID and endpoints                      |
| `update_composition`        | `id`, `expected_generation`, `overrides`                                           | Full composition with update status   |
| `get_component_logs`        | `id`, `component`, optional `tail_lines`, `max_bytes`, `since_seconds`, `previous` | Labelled bounded log snapshot         |
| `list_composition_events`   | `id`, optional `after`, `limit`                                                    | Event page                            |
| `destroy_composition`       | `id`                                                                               | Full composition with deletion status |

Tools have typed input/output schemas and a concise text compatibility result.
Waiting polls REST, returns the latest status at timeout, and stops on terminal
states. Cancelling a wait never requests deletion. Actual MCP-client acceptance
must create, wait, fetch endpoints, and destroy against the running API.

## Catalog registration and discovery

Authenticated `POST /v1/projects`, `POST /v1/projects/{project}/components` and
`POST /v1/projects/{project}/baselines` return 201. IDs are project scoped, and
registrations are immutable. Duplicates and Service-host ownership collisions
return 409. Malformed profiles and unsupported verification contracts return 400;
external dependency failures can return 503. Baselines are accepted only after
read-only Kubernetes and selected-mesh checks and a successful baseline ingress probe.
See [catalog requirements](catalog.md) and the OpenAPI registration schemas.

Create and update accept zero to three approved, bound components. Update resolves
the complete desired set against the current catalog plan. Baseline endpoints must be a single
DNS label under the configured preview domain and use the configured HTTP port.

MCP additionally exposes `list_projects` (`after`, `limit`), `list_components`
and `list_baselines` (`project`, `after`, `limit`), and `get_component` (`project`,
`component`). Lists return `items` and optional `next_cursor`; defaults are 20
entries with a maximum of 100. These tools call REST through the private client.

## Application configurations and verification levels

`GET`, `PUT` and `DELETE /v1/projects/{project}/onboarding-draft` manage a
non-secret preparation draft scoped to installation, project and authenticated
author. Revision zero creates a draft; later saves and deletion require the
current revision. A stale revision returns 409. Drafts exclude environment and
selected ConfigMap values, credentials and messaging configuration. Shared-token
or anonymous access uses a common draft author. Drafts do not register a
catalog entry or approve a profile.

`POST /v1/compositions/plan` accepts the same JSON body as composition creation
and returns selected and inherited workloads, approvals, declared dependencies,
destination, lifetime, resource demand and actionable blockers. It makes no
reservation or resource change; creation rechecks the contract and capacity.
See [onboarding](onboarding.md) for the guided flow.

`GET /v1/compositions/{id}/diagnosis` explains recorded blockers, unknowns and
cleanup separately. `GET /v1/compositions/{id}/verification` returns retained
checks with freshness derived at read time, including baseline and workload
identity drift. See [diagnostics](diagnostics.md) for coverage limits.

Both catalog configuration endpoints accept an `envy/v1` JSON object with
`project`, `components` and `baseline`; see [the shop configuration](../examples/shop/application.json).
They return 200 with `configuration`, `applied`, `checks` and `warnings`.
Validation has no catalog or provider mutations. Apply repeats live validation,
then commits all entries and host claims atomically. Identical entries are
retained; changed immutable values or another baseline's host claims return 409.

A baseline can select `verification: {"kind":"http", "path":"/products",
"expected_status":200}`. The path defaults to `/`, the status to 200, and only
2xx expected statuses are supported. The response body is not interpreted.
Redirects do not satisfy probes. Baseline and preview must both pass.

Composition and status responses include `verification_level`: `none` while
pending, failed or destroyed; `reachability` for successful HTTP checks; `routing`
for the full envy-chain proof. Endpoint readiness reflects the registered
contract. `RouteVerified` remains false for HTTP verification even when ready;
`IngressReachable` records its successful probes. Applications must independently
verify context propagation and intended override behavior. The returned endpoint
is a base URL; append the application's route when it is not `/`.

Frontend revision binding endpoints, lifecycle rules and error semantics are
specified in [frontend bindings](frontend-bindings.md) and OpenAPI. All successful
binding writes return 200, including identical retries. Resolution returns 404
for missing associations, retryable 409 for unready compositions, and 410 for
expired or deleting compositions. Lists are bounded; each composition accepts at
most 100 bindings. Published URLs and browser checks are caller-reported.
