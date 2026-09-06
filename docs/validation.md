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
