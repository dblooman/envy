# Installing Envy in an existing cluster

The Helm chart at `deploy/helm/envy` installs only the Envy control plane: its
Deployment, ClusterIP Service, service account, RBAC, configuration, and an
ingress NetworkPolicy. It never installs PostgreSQL, Istio, DNS, TLS,
authentication, demo workloads, or a catalog.

Create operator-managed Secrets first. `externalDatabase.secretName` must hold a
TLS-configured PostgreSQL URL. In proxy mode, `proxySecret` is shared only with
the trusted authentication proxy. Optional shared-token and machine-credential
Secrets preserve non-browser API access. Never put their values in Helm values.
For a private control-plane image, set `imagePullSecrets` to existing Kubernetes
Secret references. The chart attaches those references to the server, migration,
and optional preflight pods; it never reads or copies registry credentials.

Use a stable, unique `installationID`, copy
`deploy/helm/envy/values-example.yaml`, and set existing endpoint and proxy
details. Preview URLs require wildcard DNS and TLS coverage for the chosen
domain. The proxy must strip user identity headers from client requests, add one
identity header and the proxy secret, and forward the same web/API origin.
Set `auth.externalOrigin` to that public HTTPS origin so browser mutations are
validated against the address users actually visit, rather than the internal
ClusterIP host.
Set `istio.injectionLabels` to the cluster's injection revision label and
`istio.ingressSelector` to the selector on the existing Gateway. Envy validates
those values when a baseline is registered and applies the same injection labels
to composition namespaces.

```sh
helm upgrade --install envy deploy/helm/envy --namespace envy-system --create-namespace \
  --values deploy/helm/envy/values-example.yaml
kubectl -n envy-system rollout status deployment/envy-envy
```

The chart starts one controller replica. PostgreSQL remains the canonical state
and its advisory lease protects a rolling replacement; this does not claim HA.
A Helm pre-install/pre-upgrade Job runs database migrations under the existing
PostgreSQL advisory lock before the server starts. Helm rollback never rolls
back PostgreSQL; use additive migrations and restore procedures for recovery.

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
prints Secret values or attempts provider mutations. Provide `injection_labels`
and `ingress_selector` too; the preflight compares the latter to the configured
Gateway and reports the former as a catalog-registration prerequisite when a
single baseline namespace cannot be established from the installation file.

After Helm installation, run the optional in-cluster check to turn the local
command's controller-network result into evidence. It uses the referenced
database Secret only inside `pg_isready`, then connects to the configured
internal ingress address while validating the TLS certificate for
`baseline_host`; it never disables certificate verification or creates a
composition.

```sh
helm upgrade envy deploy/helm/envy --namespace envy-system --reuse-values \
  --set preflight.enabled=true
kubectl -n envy-system wait --for=condition=complete job/envy-envy-preflight --timeout=90s
```

The first package assumes HTTPS ingress on port 443 for this Job. An operator
using another port should run an equivalent approved connectivity Job before
marking the installation preflight complete.

Chart rendering is part of the repository validation gate:

```sh
make helm-lint
```

Uninstalling the chart retains PostgreSQL and compositions. Destroy compositions
through Envy and verify cleanup before removing the chart when data-plane cleanup
is intended. Baselines, Istio, proxy, certificates, and database infrastructure
remain operator-owned.
