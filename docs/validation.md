# Vertical-slice validation

Verified locally on 6 September 2026 (Europe/London), using an ARM64 Docker host,
Go 1.27.1, kind 0.33.0, Kubernetes 1.36.4, Istio 1.31.0, and PostgreSQL 18.6.

## Results

| Check | Result |
| --- | --- |
| Manual routing spike before control-plane implementation | Passed on a real kind/Istio cluster |
| Package tests with the Go race detector | Passed, including real PostgreSQL integration tests |
| `go vet ./...` | Passed |
| Fresh-cluster REST lifecycle acceptance | Passed |
| Expiry while the controller is stopped | Passed |
| Actual MCP SDK client over subprocess stdio | Passed against the running REST API and mesh |
| Final Istio protobuf update fix | Provider race tests, static analysis, and a repeat live MCP lifecycle passed |

The complete fresh-cluster acceptance test took approximately 142 seconds after
cluster setup. It verified two simultaneous compositions, idempotency and key
conflicts, control-plane restarts during provisioning and after readiness,
unavailable images, an override with no healthy endpoints, hostname baggage
normalization, unknown host rejection, draining, namespace cleanup, and durable
expiry. Baseline workload identities remained unchanged throughout.

The observed request paths were:

```text
Baseline:     gateway-v1 → service-a-v1 → service-b-v1
Composition:  gateway-v1 → service-a-v1 → service-b-v2
```

Each composition hop reported the composition ID from its request context. The
gateway and service-a reported the same pod identities as the baseline. Service-b
reported a separate composition-owned pod running the v2 binary.

The test cluster and the separate PostgreSQL test container were disposable.
The reusable `envy-dev` environment is separate and can be removed with
`make dev-down`.

## CLI and update acceptance

The extended fresh-cluster gate passed on 6 September 2026 in approximately
189 seconds after setup. It reran the initial lifecycle and expiry tests and
added actual CLI and MCP image updates:

- The compiled CLI created v2 and updated it to the independently built v3 image.
  The ID, URL, Deployment UID, and Service UID stayed stable; service-b received
  a new pod UID while baseline and inherited hop UIDs stayed unchanged.
- A controller restart during the update recovered the persisted generation.
- A stale expected generation returned a structured conflict.
- An unavailable-image update never reported ready or fell back to baseline;
  a subsequent update repaired the failed rollout at the same URL.
- Deletion rejected further updates and removed the endpoint and namespace.
- An actual stdio MCP SDK client created, waited, updated to v3, verified real
  ingress traffic, and destroyed the composition.

`go test -race ./...` passed with a separate PostgreSQL 18.6 test container,
including competing updates, stale observation fencing, retained operation
history, and deletion/expiry precedence. `go vet ./...` passed. The web frontend's
API request types match PATCH and `make ui-build` passed. Browser diagnostics
verification is described below.

The disposable kind cluster was deleted after the run. Acceptance output remains
in `.envy/envy-e2e/acceptance.log` and setup output in `.envy/update-acceptance.log`.

## Diagnostics validation

Diagnostics package tests passed with the race detector and real PostgreSQL
18.6. They cover byte/pod/line limits, per-pod partial errors, selector scoping,
namespace/Deployment/Service ownership checks, pod identity changes during reads,
shared-baseline labelling, query validation, and CLI/MCP argument preservation.
Database tests verify event rollback with the enclosing state transaction,
idempotent-create and unchanged-poll deduplication, cursor pagination, stale-write
fencing, expiry classification, and repeatable backfill migration.

The frontend production build passed. A browser session against the refreshed
`envy-dev` backend verified that simulation mode exposes no fabricated diagnostics,
Live Mode displays genuine service-b v2 container logs, gateway logs carry the
shared-baseline label, and create/provisioning/ready events appear in the history
panel. Credentials were supplied by the existing loopback Vite proxy.

## Reproduction

```sh
ENVY_CLUSTER_NAME=envy-spike ENVY_PREVIEW_PORT=28080 ENVY_API_PORT=28081 make routing-spike
ENVY_CLUSTER_NAME=envy-spike make dev-down
make test
make test-e2e
```

Run the PostgreSQL package tests against a disposable database by setting
`ENVY_TEST_DATABASE_URL`, then run `go test -race ./...`. Those tests create and
drop isolated schemas. Without that variable, PostgreSQL-specific package tests
are explicitly skipped; the kind end-to-end suite still uses real PostgreSQL.

`make test-e2e` creates a fresh cluster and deletes it on completion. Its test
output remains in `.envy/<cluster-name>/acceptance.log`, with cluster diagnostics
on failure. Set `ENVY_E2E_KEEP_CLUSTER=1` only when retaining a test cluster for
inspection; an existing cluster is never silently replaced.

The manual spike also refuses a cluster already running the control plane. Use
the dedicated name and ports above when a development environment is running.

## Limits of the evidence

This proves the one-override HTTP demo and two concurrent compositions. It does
not establish production readiness, isolation of shared data or side effects,
twenty-composition performance, multi-replica atomic cutovers, gRPC support, or
asynchronous consumer routing. See the architecture and routing documents for
those boundaries.
