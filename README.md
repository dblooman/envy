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

Only service-b is duplicated. Gateway and service-a remain shared and propagate
composition baggage. The preview hostname supplies the context automatically.
PostgreSQL persists lifecycle state; the control plane stays out of the request
path.

This is a local development slice for one trusted organisation. Baseline
deployments remain live, and inherited databases, caches, credentials, and side
effects remain shared. See [sharing semantics](docs/adr/007-sharing-semantics.md).

## Local development

Docker must be running. Setup creates a dedicated `envy-dev` kind cluster and
uses a task-local kubeconfig, independently of the current desktop Kubernetes
context. The deployment tooling owns version and image pins under `deploy/`.
Install Go 1.27.1, `kubectl`, Python 3, and `curl`; setup downloads checksum-verified
kind 0.33.0 and Istio 1.31.0 and uses Kubernetes 1.36.4 with PostgreSQL 18.6.

```sh
make routing-spike   # Prove propagation and selective routing before the control plane
make dev             # Reusable baseline, PostgreSQL, and control plane
make test            # Unit, API, and provider tests
make test-e2e        # Fresh isolated kind acceptance environment
make dev-down        # Remove the dedicated development cluster
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
`wait_for_composition`, `get_composition_endpoints`, `update_composition`, and
`destroy_composition`.
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

## Web UI

Envy includes a modern web frontend built with React, Vite, pnpm, Base UI, and shadcn/ui.
It provides a dashboard, collapsible sidebar navigation, live composition status polling,
preset-assisted creation wizards, rolling image updates, catalog exploration, and an
interactive Istio routing topology view.

```sh
# Start the web development server (with built-in proxy to http://127.0.0.1:8081)
make ui-dev

# Or run directly inside web/
cd web
pnpm dev
```

The UI includes a toggleable **Demo & Simulation Mode** that allows full exploration and testing
even when the local Kind cluster or PostgreSQL backend is not running. In Live Mode,
provide your API token (generated at `.envy/envy-dev/api-token` during `make dev`) in the
Settings view or header prompt.

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
updates. Catalog writes, bounded logs/events, and discovery MCP tools remain
subsequent work. Resource cloning, async consumer routing, build/test execution,
agent runtime, UI, production operation, and enforced multi-tenancy are outside
this slice.

## License

[MIT](LICENSE).
