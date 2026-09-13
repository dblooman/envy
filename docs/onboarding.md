# Application onboarding

A checked-in `envy/v1` JSON configuration describes one project, its complete
baseline component catalog, and one existing baseline. It contains approved
literal workload configuration, never credentials. Envy borrows existing Services
and deployments; operators still install the application and its mesh ingress.

`delivery catalog validate --file application.json` performs read-only catalog,
Kubernetes, Istio ownership and ingress checks. `delivery catalog apply --file
application.json` repeats validation and registers the bundle in one PostgreSQL
transaction. Identical existing entries are retained; changed immutable entries
conflict. Repeating apply is safe, including after an uncertain response.

The REST API owns both operations at `/v1/catalog/validate` and
`/v1/catalog/apply`. CLI output is JSON with the normalized configuration,
checks and limitations. No local kubeconfig is required by the CLI.

Verification has two explicit levels:

- `envy-chain`: proves the registered chain, context propagation, selected pod
  identities and live baseline inheritance. Successful compositions report
  `verification_level: routing` and `RouteVerified: true`.
- `http`: checks a configured absolute path and expected successful HTTP status
  through both baseline and preview ingress. Response content is application
  owned. Successful compositions report `verification_level: reachability`,
  `IngressReachable: true`, and `RouteVerified: false`. Healthy pods plus a 200
  response cannot prove downstream propagation, override selection or inheritance.

For HTTP verification, ready means all selected workloads are ready, routing
configuration was accepted, and both ingress probes succeeded. Integrators must
verify propagation and selected workload behavior through their own application
checks. Failed or pending compositions report no current verification evidence.
Resource and access isolation guarantees remain unchanged.

The shop example uses business JSON and ordinary HTTP response headers rather
than Envy's chain schema. Its external acceptance checks independently prove
context propagation and workload selection. Capacity acceptance creates twenty
compositions, checks the twenty-first is rejected, interleaves baseline and
preview requests, measures readiness/latency, and verifies complete cleanup.
This is a development capacity check, not a production throughput benchmark.

For existing Deployments with environment references and configuration mounts,
use the opt-in [deployment-derived workflow](deployment-derived-previews.md).
It adds a discover/review/approve step after baseline catalog registration and
avoids maintaining a separate literal workload configuration.
