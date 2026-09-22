# Composite application simulator

The disposable composite fixture is a small application and dependency stack for
testing Envy without access to another repository, cloud account or database.
It exercises the shape of a multi-container Pod and an application-owned
read-only request. Its data and identities are synthetic.

```text
gateway -> service-a -> service-b application -> localhost dependency proxy
                                               -> shared synthetic database Service
```

The selected `service-b` application runs beside a regular HTTP proxy. A
one-shot init container prepares the Pod; one native sidecar remains running as
a helper, and another proxies database-like requests to the shared Service in
the baseline namespace. Each container has its own copied ConfigMap and Secret
references, explicit probes, resource bounds and non-root security settings.
Envy discovers and approves this exact shape before creating previews. Only
the application image changes between the baseline and previews.

`GET /api/v1/subjects/sample-user/resources/sample-space/access` returns a
synthetic read grant. The response identifies the selected application release,
Pod and composition, so the acceptance test can distinguish a preview response
from a healthy baseline fallback. The application queries the dependency
through its local sidecar for every request and checks it for readiness. When
the shared Service loses its endpoints, readiness and business traffic fail;
both recover when it returns. The normal demo does not expose a working record
lookup because it does not set `SIMULATED_DATABASE_URL`.

From the Envy repository root, with the local Docker, kind and Kubernetes tools
installed, run:

```sh
make doctor
make test-composite-e2e
```

The command creates a fresh `envy-composite-e2e` cluster and removes it on
completion. It validates discovery and approval, baseline plus two distinct
previews, request routing, the synthetic record response, image updates,
configuration copying, dependency outage and recovery, sidecar failure
diagnostics, logs and cleanup. The test log is written under
`.envy/envy-composite-e2e/composite-acceptance.log`.

The synthetic database speaks HTTP so this test proves Pod wiring, service DNS,
mesh connectivity and readiness propagation. It does not prove SQL protocol
compatibility, cloud IAM or an application's business rules. Those checks
belong to installation acceptance when a real environment is available.

On 22 September 2026, the disposable-cluster lifecycle test passed in 197.53
seconds. It covered the record request from baseline and both previews, an
application image update, dependency loss and recovery, sidecar failure, and
deletion. The cluster was removed after the run.

The application request is implemented in
[`demo/internal/server/record.go`](../demo/internal/server/record.go). The
container and fake database are in
[`deploy/local/composite-fixture`](../deploy/local/composite-fixture); the live
acceptance is in [`tests/e2e/composite_test.go`](../tests/e2e/composite_test.go).
