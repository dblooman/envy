# Envy

Envy creates temporary compositions of a distributed application by combining
a shared staging baseline with selected workload overrides. Developers, CI, and
external agents use REST, MCP, or the delivery CLI to create a composition, inspect its status,
obtain its URL, and destroy it.

The first slice runs a three-service Go demo on Kubernetes with Istio:

```text
Baseline:     gateway-v1 → service-a-v1 → service-b-v1
Composition:  gateway-v1 → service-a-v1 → service-b-v2
```

In this example, only service-b is duplicated. Compositions can select up to three
approved overrides, including gateway and service-a. Inherited services remain
shared and propagate composition baggage. The preview hostname supplies the
context automatically.
PostgreSQL persists lifecycle state; the control plane stays out of the request
path.

This is a local development slice for one trusted organisation. Baseline
deployments remain live, and inherited databases, caches, credentials, and side
effects remain shared. See [sharing semantics](docs/adr/007-sharing-semantics.md).

## Local development

Docker must be running. Setup creates a dedicated `envy-dev` kind cluster and
uses a task-local kubeconfig, independently of the current desktop Kubernetes
context. The deployment tooling owns version and image pins under `deploy/`.
Install Go 1.27.1, `kubectl`, Python 3, `curl`, and `sqlc` (for SQL code generation); setup downloads checksum-verified
kind 0.33.0 and Istio 1.31.0 and uses Kubernetes 1.36.4 with PostgreSQL 18.6.

```sh
make routing-spike   # Prove propagation and selective routing before the control plane
make dev             # Reusable baseline, PostgreSQL, and control plane
make test            # Unit, API, and provider tests
make test-e2e        # Fresh isolated kind acceptance environment
make dev-down        # Remove the dedicated development cluster
make sqlc            # Generate Go code from SQL queries using sqlc
```

Default endpoints are `http://127.0.0.1:8081` for the API and
`http://baseline.envy.localhost:8080` for the baseline. Preview URLs use
`http://cmp-<id>.envy.localhost:8080`. Local bindings are loopback only. Setup
generates API credentials in ignored local files with restrictive permissions;
PostgreSQL remains internal.

The default token is `.envy/envy-dev/api-token` and the dedicated kubeconfig is
`.envy/envy-dev/kubeconfig`. Load the token into your shell without printing it:

```sh
export ENVY_API_TOKEN="$(cat .envy/envy-dev/api-token)"
```

With `ENVY_API_TOKEN` set from the generated local credential, create a
composition:

```sh
curl --fail-with-body http://127.0.0.1:8081/v1/compositions \
  -H "Authorization: Bearer $ENVY_API_TOKEN" \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: demo-service-b-v2-001' \
  --data-binary @examples/create-composition.json
```

Poll the returned `Location` until `phase` is `ready`, then open
`endpoints.public.url`. The URL can be allocated before it is ready. If local
wildcard DNS is unavailable, `curl --resolve '<preview-host>:8080:127.0.0.1'`
preserves the required Host while dialing loopback. Delete the composition with
an authenticated `DELETE /v1/compositions/<id>`; cleanup is asynchronous and
repeatable. Compositions expire after eight hours by default.

The stdio MCP executable exposes `create_composition`, `get_composition`,
`wait_for_composition`, `get_composition_endpoints`, `update_composition`,
`destroy_composition`, `get_component_logs`, and `list_composition_events`.
It calls the same REST API. See [API and MCP usage](docs/api.md).

`make dev` builds the stdio executable at `.envy/bin/envy-mcp`. To configure a
compatible MCP client, use that executable's absolute path as its server command
and set `ENVY_API_URL=http://127.0.0.1:8081` and `ENVY_API_TOKEN_FILE` to the
absolute path of `.envy/envy-dev/api-token`. The adapter needs no Kubernetes
credentials. To launch it directly:

```sh
ENVY_API_TOKEN_FILE="$PWD/.envy/envy-dev/api-token" .envy/bin/envy-mcp
```

PostgreSQL integration tests are enabled by `ENVY_TEST_DATABASE_URL`; the test
suite creates and drops its own isolated schema. Point it at a disposable test
database. To include those checks and the Go race detector:

```sh
ENVY_TEST_DATABASE_URL='<disposable-postgres-connection-url>' go test -race ./...
```

`make test-e2e` uses a fresh `envy-e2e` cluster on ports 18080/18081 and removes it
when finished. Set `ENVY_E2E_KEEP_CLUSTER=1` to retain it for inspection. Existing
clusters are never silently deleted; select a fresh `ENVY_CLUSTER_NAME` or remove
the prior dedicated cluster explicitly. Diagnostics and observed image identities
remain under `.envy/<cluster-name>/`.

## CLI and image updates

`make dev` also builds `.envy/bin/delivery` and loads service-b v3. `make build`
builds the executables without changing the cluster.

```sh
export ENVY_API_TOKEN_FILE="$PWD/.envy/envy-dev/api-token"
.envy/bin/delivery composition create --name my-preview --image envy/service-b:v2
.envy/bin/delivery composition wait <id> --timeout 60s
.envy/bin/delivery composition update <id> --expected-generation 1 --image envy/service-b:v3
.envy/bin/delivery composition wait <id> --timeout 60s
.envy/bin/delivery composition destroy <id>
```

Commands return JSON. Updates preserve the ID, URL, and expiry, and reject stale
expected generations with 409. The new generation becomes ready only after the
new pod is observed through ingress. During a rolling update, the URL can serve
the previous or new override. See [CLI usage](docs/cli.md) and
[update semantics](docs/updates.md).

## Logs and lifecycle history

```sh
.envy/bin/delivery composition logs <id> --component service-b --tail-lines 100
.envy/bin/delivery composition logs <id> --component gateway --since 1h
.envy/bin/delivery composition events <id> --limit 20
```

Inherited logs are explicitly labelled shared-baseline and are not filtered to
the composition. Logs are bounded snapshots from Kubernetes; they disappear
with pods. Lifecycle events are stored transactionally in PostgreSQL and remain
available after destruction. The same features are exposed through REST, MCP,
and the frontend's Inspect dialog in Live Mode. See [diagnostics](docs/diagnostics.md).

## Web UI

Envy includes a modern web frontend built with React, Vite, pnpm, Base UI, and shadcn/ui.
It provides a dashboard, collapsible sidebar navigation, live composition status polling,
preset-assisted creation wizards, rolling image updates, catalog exploration, and an
interactive Istio routing topology view.

```sh
# Start the web development server (with built-in proxy to http://127.0.0.1:8081)
# The proxy reads the generated local API credential and adds it to API requests.
make ui-dev

# Or run directly inside web/
cd web
ENVY_API_TOKEN="$(cat ../.envy/envy-dev/api-token)" pnpm dev
```

The UI includes a toggleable **Demo & Simulation Mode** that allows full exploration and testing
even when the local Kind cluster or PostgreSQL backend is not running. In Live Mode,
`make ui-dev` forwards the API token generated at `.envy/envy-dev/api-token` through its local
proxy. For a remote server or another local server, provide that server's token in the Settings
view or header prompt.

## Design and validation

- [Architecture](docs/architecture.md) and [domain model](docs/domain-model.md)
- [Request routing](docs/routing.md)
- [REST and MCP contract](docs/api.md) and [OpenAPI](api/openapi.yaml)
- [Validation results and reproduction](docs/validation.md)
- [Architecture decisions](docs/adr/)
- [Original product brief](plan.md), preserved unchanged

Acceptance covers real ingress routing, baseline identity preservation,
simultaneous compositions, idempotent retries, control-plane restart recovery,
unavailable images, unhealthy overrides, hostile inbound baggage, verified
deletion, expiry while the control plane is stopped, and an actual MCP client.
Checks poll observable conditions with deadlines and capture cluster diagnostics
on failure. A Deployment becoming ready alone never marks a composition ready.

The initial persistent REST/MCP slice now includes the delivery CLI and image
updates, bounded component logs, and durable lifecycle events. Catalog
registration, discovery MCP tools, and up to three component overrides are also
implemented. Resource cloning, async consumer routing, build/test execution,
agent runtime, production operation, and enforced multi-tenancy are outside
this slice.

## License

[MIT](LICENSE).

### Register another application

The Catalog screen can register projects, approved component profiles and
existing baselines. Compositions can select one to three approved components, including
an entry or middle service. Registrations are immutable, project scoped and
validated against Kubernetes/Istio before a baseline is accepted. Applications
currently need the explicit `envy-chain` verification response contract.
See [catalog registration](docs/catalog.md) and [API schemas](api/openapi.yaml).
MCP provides four catalog discovery tools; CLI create/update accepts repeated
`--override component=image` flags. See [multiple overrides](docs/multiple-overrides.md).

## Onboard an HTTP application

Keep a versioned JSON configuration beside the application's source, then run:

```sh
.envy/bin/delivery catalog validate --file application.json
.envy/bin/delivery catalog apply --file application.json
```

Validation checks existing infrastructure without writes. Apply registers the
bundle atomically and accepts identical retries. The [shop walkthrough](examples/shop/README.md)
uses ordinary business JSON and `http` verification, with a clear distinction
between endpoint reachability and verified request routing. Start its borrowed
baseline with `make dev-shop` after `make dev`.
See [onboarding semantics](docs/onboarding.md) for configuration and evidence limits.

Frontend previews can bind an exact Git commit to a composition through REST,
`delivery frontend`, or MCP. The [frontend bindings guide](docs/frontend-bindings.md)
covers expiry and browser evidence; the [Cloudflare Pages adapter](integrations/cloudflare-pages/README.md)
resolves a public API URL before the existing frontend build. Composition details
show reported frontend links and current/stale browser checks. The
[agent workflow kit](docs/agent-workflow.md) provides a reusable skill and repository
instruction template for discovery, composition reuse, builds and verification.
