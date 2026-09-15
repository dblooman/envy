# Explicit mesh acceptance fixtures

Run from the repository root with Docker, kubectl, Helm 3.19, Python 3,
pnpm (the version in `web/package.json`), and the Go version in `go.mod`.
Install the kind version in `versions.json` on PATH. Allow enough Docker resources
for the selected controller and twenty demo workloads; run one profile at a time
on constrained development machines.

```sh
make test-mesh MESH=istio
make test-mesh MESH=cilium
make test-mesh MESH=linkerd
```

Each invocation creates an isolated `envy-test-<provider>` cluster. It refuses an
existing cluster of that name. `ENVY_CLUSTER_NAME`, `ENVY_PREVIEW_PORT` (19080), and
`ENVY_API_PORT` (19081) select a different isolated fixture. The scripts set their
own kubeconfig and never use your current context. Cleanup removes only the
fixture cluster. Set `ENVY_E2E_KEEP_CLUSTER=1` to retain it for investigation;
`ENVY_TEST_RESUME=1` explicitly resumes its provisioning after a failed setup.
Do not use resume against a production installation.

The production Helm chart installs only Envy. These scripts install test meshes,
Gateway API CRDs, ingress controllers, PostgreSQL, baseline demos and test-only
certificates. Cilium and Linkerd clusters contain no Istio APIs. HTTPS is enabled
by default, with an explicit CA bundle trusted by Envy and the test client.
`ENVY_TEST_HTTPS=0` is available for debugging and does not satisfy HTTPS acceptance.

The same traffic suite checks REST, CLI, MCP, entry/middle/leaf overrides, concurrent
compositions, twenty compositions on one Service, baggage isolation, updates,
expiry, restart recovery and deletion. The fixture installs the real chart and
runs both read-only preflight and the in-cluster database/HTTPS connectivity Job.
Missing tools, APIs, controllers or connectivity fail explicit runs.

`versions.json` records exact controller versions and registry image digests.
Cilium and Envoy data-plane digests are also fixed by their versioned charts.
Update this manifest and fixture pins together, run every profile, then record
acceptance and regenerate documentation with:

```sh
python3 deploy/testing/render-versions.py
python3 deploy/testing/render-versions.py --check
```

Ordinary `go test ./...` does not contact Kubernetes. `TestGatewayAPIResources`
requires both `ENVY_TEST_GATEWAY_API_RESOURCES=1` and an explicit
`ENVY_KUBE_CONTEXT`; it checks schema admission and lifecycle only. Its success
is never evidence that a controller forwards application traffic.

## Legacy upgrades and Linkerd conformance

The Istio fixture first installs the server and chart at `istio_upgrade_from` in
`versions.json`, registers a baseline, and creates a live HTTPS composition. It
then upgrades to the current chart, verifies provider binding, stable URLs and
workload identities, baseline traffic, and destruction before running the shared
suite. `ENVY_TEST_ISTIO_UPGRADE=0` disables this step for debugging only.

The pinned Linkerd controller omits `observedGeneration` on producer HTTPRoute
conditions. Envy creates content-addressed producer routes and accepts those
generation-less conditions only for owned, generation-one objects; Envoy Gateway
ingress remains generation-checked. The Linkerd fixture records this conformance
status and then runs the full traffic suite in CI.
