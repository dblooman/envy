# Installing Envy in an existing cluster

The Helm chart at `deploy/helm/envy` installs only the Envy control plane: its
Deployment, ClusterIP Service, service account, RBAC, configuration, and an
ingress NetworkPolicy. It never installs PostgreSQL, Istio, DNS, TLS,
authentication, demo workloads, or a catalog.

Create operator-managed Secrets first. `externalDatabase.secretName` must hold a
TLS-configured PostgreSQL URL. In proxy mode, `proxySecret` is shared only with
the trusted authentication proxy. Optional shared-token and machine-credential
Secrets preserve non-browser API access. Never put their values in Helm values.

Use a stable, unique `installationID`, copy
`deploy/helm/envy/values-example.yaml`, and set existing endpoint and proxy
details. Preview URLs require wildcard DNS and TLS coverage for the chosen
domain. The proxy must strip user identity headers from client requests, add one
identity header and the proxy secret, and forward the same web/API origin.

```sh
helm upgrade --install envy deploy/helm/envy --namespace envy-system --create-namespace \
  --values deploy/helm/envy/values-example.yaml
kubectl -n envy-system rollout status deployment/envy-envy
```

The chart starts one controller replica. PostgreSQL remains the canonical state
and its advisory lease protects a rolling replacement; this does not claim HA.
Database migration runs at startup for this first package, so upgrade one chart
version at a time and never describe Helm rollback as a database rollback.

Before installation, an operator must verify Kubernetes/Istio API access,
gateway ownership, sidecar injection, controller-to-ingress reachability,
PostgreSQL connectivity, DNS/TLS, and referenced Secret distribution. Create a
catalog and a smoke composition only after deployment; installing or opening the
website never creates application resources.

Use the read-only preflight before Helm. It reports `pass`, `fail`, or `unknown`
as JSON; an unknown controller-network check is deliberately not a pass because
it must be completed from a pod in the target cluster.

```sh
delivery installation check --file installation.json
```

The file contains `namespace`, `gateway.namespace`, `gateway.name`,
`database_secret.name`, `database_secret.key`, `preview_base_url`, `ingress_url`,
and `baseline_host`; it may also provide a task-local `kubeconfig` path. It never
prints Secret values or attempts provider mutations.

Uninstalling the chart retains PostgreSQL and compositions. Destroy compositions
through Envy and verify cleanup before removing the chart when data-plane cleanup
is intended. Baselines, Istio, proxy, certificates, and database infrastructure
remain operator-owned.
