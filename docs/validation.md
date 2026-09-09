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

The complete fresh-kind acceptance gate passed on 6 September 2026 in 254 seconds
after setup. It includes bounded real Kubernetes log reads, shared/override pod
identity checks, lifecycle pagination and restart persistence, unchanged-poll
deduplication, distinct expiry events, and tombstone history. The existing routing,
image-update, cleanup, and actual CLI/MCP subprocess acceptance tests also passed.
Log snapshots can legitimately be partial when a terminating rollout pod disappears
during the read; the MCP acceptance checks for readable application logs while
preserving that partial-result contract.

The final setup and test output is `.envy/diagnostics-acceptance-final.log`; the
current isolated-cluster test output is `.envy/envy-e2e/acceptance.log`. The test
cluster and PostgreSQL test container were removed. `envy-dev` was refreshed, its
existing records received snapshot events, and baseline traffic still returned
all v1 versions. The temporary browser-test composition was destroyed and its
nine lifecycle events remained queryable.

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

## Catalog registration acceptance

Catalog registration was exercised through REST and the live frontend on
6 September 2026. The first fresh-cluster gate passed in 314.252 seconds after
setup. It registered an independent Orders application in `envy-orders`, approved
`service-a`, and verified `gateway-v1 → service-a-v2 → service-b-v1` while a demo
`service-b` composition and both all-v1 baselines remained intact. Restarting the
controller retained both routing domains. Duplicate registrations and an
unconfigured baseline hostname were rejected. Cleanup removed the composition
routes and owned namespaces.

A second fresh kind run validated the combined SQLC persistence, CLI and routing
changes and passed all seven acceptance tests in 512.218 seconds after setup.
Its log is `.envy/catalog-final-acceptance.log`. The test cluster and disposable
PostgreSQL test container were removed. The race-test log is
`.envy/catalog-final-race.log`; the additional schema-upgrade and routing checks
are in `.envy/catalog-upgrade-check.log`.

The live browser registered the Orders project, three profiles and its baseline,
created a `service-a:v2` composition, then updated that same component to v1 while
retaining the URL and proving the selected pod was still composition owned. The
browser destroyed the test composition; its endpoint returned 404 and its
namespace was absent. The Orders baseline and catalog remain available in
`envy-dev` for further use. Browser chain evidence is recorded in
`.envy/catalog-browser-proof.json`.

Race-enabled unit, API, MCP protocol and real PostgreSQL tests passed, including
project scoping, atomic Service-host claims, stored profiles and bindings, and an
upgrade from the pre-catalog schema retaining workload identities. Additional
routing contract tests reject overlapping hosts on Gateways sharing an ingress
proxy and exact-host coverage that cannot serve future composition URLs.
`make ui-build` and `go vet ./...` passed. Registration, component selection,
workload labels, current image display and update presets were checked in the
browser with the user's current frontend styling.

## Multiple override validation

On 7 September 2026, race-enabled unit, API, MCP, provider and PostgreSQL tests
passed with a real disposable PostgreSQL 18.6 database. Coverage includes complete
update maps, stale generations, one-to-three bounds, legacy profile/workload map
migration preserving recorded UIDs, shared quotas with distinct selectors,
per-component pod verification, retained routes after partial failure, and removal
of every aggregate entry before namespace cleanup. Logs are in
`.envy/multi-race.log`. `go vet ./...` and `make ui-build` also passed.

The full fresh-kind gate passed all eight acceptance tests in 504.301 seconds
after setup (`.envy/multi-acceptance.log`). The test cluster and disposable
PostgreSQL container were removed; `envy-dev` remains available with the refreshed
control plane and demo images.

The fresh-kind multi-override test passed in 181.56 seconds. It kept three
compositions active together: REST selected service-a/service-b, an actual MCP
client selected gateway/service-b, and the compiled CLI selected all three.
Checks covered baseline identities, idempotency, per-component logs, unchanged
pods during a partial image update, restart recovery, failure without baseline
fallback, and complete deletion while other compositions kept serving.

The live frontend created `multiple-browser-proof` with gateway, service-a and
service-b v2. Actual ingress returned v2 at every hop with the composition ID and
owned pod identities. Updating service-b to v3 retained the same URL and the
other two pod identities. Demo and Orders baseline responses and identities
remained unchanged. Search located the composition by its gateway image. Browser
destruction reached a tombstone, HTTP 404 and namespace absence. Request evidence
is in `.envy/multi-browser-proof.json`.

The long-running development mesh initially had expired workload certificates.
Restarting its affected proxy containers renewed certificates without replacing
baseline pods or application containers. Envy correctly withheld preview readiness
during that failure and recovered after mesh connectivity returned.

## Application onboarding validation

On 9 September 2026, the full race-enabled Go suite passed with a real disposable
PostgreSQL database, including atomic bundle registration, concurrent identical
applies, immutable conflicts, read-only validation, strict REST/CLI contracts,
and HTTP verification that does not claim routing proof. `go vet ./...`,
`go vet -tags=e2e ./tests/e2e`, and `make ui-build` passed.
Logs are `.envy/onboarding-final-race.log` and `.envy/onboarding-final-vet.log`.

The live frontend was checked on 8 September with the shop example: create a
pricing v2 preview, display its HTTP-only verification warning, update pricing
to v1 without changing the URL, then destroy it. External requests confirmed the
selected pricing pod, propagated context, unchanged shared storefront and baseline,
and final endpoint/namespace absence. Evidence is in
`.envy/onboarding-browser-proof.json`; the shop baseline and catalog remain in
`envy-dev`. See [the shop walkthrough](../examples/shop/README.md).

Preview ingress marks forwarded responses with `x-envy-route`. Unit and provider
tests require this marker and reject application-generated 404s as evidence of
route withdrawal. Failure-time capacity diagnostics now preserve pod statuses,
events and controller logs before cleanup removes composition workloads.
The live shop check also passed: an active application 404 retained its marker,
pricing v2 served through the preview, and deletion produced an unmarked ingress
404 plus namespace absence (`.envy/onboarding-withdrawal-live.json`).

The development proxies again held expired certificates after the machine's
inactive period. Renewing only those proxy containers restored traffic while
preserving all eight baseline pod and application-container identities. This
local mesh recovery is separate from Envy's composition lifecycle.

The first full onboarding gate passed its eight existing acceptance tests, but
failed the twenty-composition test: all previews became ready after 236.34 seconds,
then a baseline request returned 504. Concurrent probe failures affected shop
workloads and Kubernetes controllers. Those observations do not establish a root
cause or a passing capacity result. Original evidence is retained in
`.envy/onboarding-first-failure-20260909/` and `.envy/onboarding-acceptance.log`.

A fresh focused run passed all 100 preview and 100 baseline requests, including
the application-404 marker check, but then hit a connection EOF during cleanup
immediately after the deliberate controller restart. The test had treated
Deployment readiness as public API readiness. Its restart helper now drops old
idle connections and probes the public API with bounded polling before further
mutations. Traffic assertions and deletion assertions are unchanged. This failed
run is preserved in `.envy/onboarding-focused-failure-20260909/`.

The final full fresh-kind gate passed all nine tests in 820.009 seconds after
setup (`.envy/onboarding-final-acceptance.log`). This includes actual MCP and CLI
clients, multi-override updates, restart recovery, expiry, diagnostics, onboarding
and capacity. The acceptance cluster and disposable PostgreSQL test container
were removed; the refreshed `envy-dev` and shop catalog remain available.

The twenty-composition acceptance test passed in 308.87 seconds, including
restart recovery and verified cleanup of all twenty hostnames and namespaces.
The twenty-first create was rejected. All 100 preview requests selected distinct
owned pricing v2 pods and retained the shared storefront; all 100 interleaved
baseline requests preserved their v1 response and workload identities. The test
also confirmed that application 404s retained the preview route marker.

| Development measurement | Result |
| --- | ---: |
| All twenty previews ready | 205.26 seconds |
| Readiness p50 / p95 | 157.32 / 200.72 seconds |
| Preview request p50 / p95 | 1.18 / 5.35 milliseconds |
| Slowest preview request | 9.00 milliseconds |

Measurements are stored in `.envy/envy-e2e/capacity.json`. Requests were sequential,
with five passes over all twenty previews, not a concurrent throughput load.
Docker had 10 CPUs and 7.748 GiB of memory available; `envy-dev` remained running.
A snapshot during capacity showed no container restarts and about 2.46 GiB used
by the acceptance cluster. Serial reconciliation exceeded the default 60-second
provisioning window for some compositions, which temporarily reported failed
before retrying successfully. This validates eventual development capacity, not
a startup latency guarantee; reconciliation efficiency remains a scaling limit.

## Limits of the evidence

The checks cover up to three overrides per composition using the explicit
envy-chain HTTP contract, independent registered applications, concurrent routing
domains, and twenty single-override HTTP shop previews with external routing
assertions. They do not establish production readiness, isolation of shared data
or side effects, concurrent request throughput, multi-replica atomic cutovers, gRPC support, or
asynchronous consumer routing. See the architecture and routing documents for
those boundaries.
