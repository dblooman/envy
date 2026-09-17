# Installing Envy in an existing cluster

For Istio, Cilium, and Linkerd prerequisites, examples, and acceptance status, see
[mesh installation profiles](mesh-installation.md). Istio-specific instructions below apply only to the Istio profile.

For an evaluated or production installation, use the published OCI chart rather than building Envy from source. Replace `X.Y.Z` with the selected release and supply your own values file:

```sh
helm upgrade --install envy oci://registry-1.docker.io/davey/envy-chart \
  --version X.Y.Z --namespace envy-system --create-namespace \
  --values values.yaml
kubectl -n envy-system rollout status deployment/envy-envy
```

The chart defaults to the matching public `davey/envy:X.Y.Z` image. Pin the chart version for production; `latest` is a convenience image tag, not an installation recommendation. The repository path `deploy/helm/envy` remains for source contributors and local chart development.

The Helm chart installs only the Envy control plane: its
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

For private composition images using legacy `http-small` profiles, configure `approvedWorkloadImagePullSecrets`
with the permitted Secret names (default: none). The equivalent server JSON
setting is `approved_image_pull_secrets`. A component profile can then select
up to eight unique names in `image_pull_secrets`. Envy checks approval at
registration, create/update, and before workload reconciliation.

The operator's secret-distribution controller must create these Secrets in each
composition namespace, for example by selecting its `envy.dev/installation`
label. Envy only adds Pod imagePullSecrets references: it does not grant its
controller Secret-read permissions or copy baseline credentials. Failed image
pulls remain visible through workload readiness and diagnostics. A cached image
can start without using registry credentials; readiness is not proof that the
Secret has been distributed correctly.

Removing a name from the startup allowlist prevents further workload mutations
using that reference, including persisted profiles. It does not erase existing
pods, Secrets, or cached images. Coordinate rotation/revocation with the external
distribution controller. Environment-variable Secret bindings and mounted
application credentials are not supported by the legacy profile.

Opt-in [deployment-derived profiles](deployment-derived-previews.md) instead
copy approved application configuration and registry Secrets from the baseline.
They require explicit onboarding and named source-read RBAC; enabling the feature
does not grant cluster-wide Secret access.

Use a stable, unique `installationID`, copy
`deploy/helm/envy/values-example.yaml`, and set existing endpoint and proxy
details. Preview URLs require wildcard DNS and TLS coverage for the chosen
domain. The proxy must strip user identity headers from client requests, add one
identity header and the proxy secret, and forward the same web/API origin.
Set `auth.externalOrigin` to that public HTTPS origin so browser mutations are
validated against the address users actually visit, rather than the internal
ClusterIP host.
Proxy-authenticated mutations require a singular matching Origin even when the
proxy strips cookies. Non-browser automation should use named bearer credentials.
Any present malformed Authorization header is rejected before proxy or anonymous
fallback.

HTTPS baseline registration requires an HTTPS Gateway listener with SIMPLE TLS
termination and a certificate credential reference. The server probes the
internal `runtime.ingressURL` address using the public hostname for Host and TLS
SNI/certificate verification. For private CAs, set `runtime.caConfigMap.name`
and `runtime.caConfigMap.key` to a PEM CA bundle in an existing ConfigMap.
Standalone servers can use `ENVY_INGRESS_CA_FILE` or
`runtime.ingress_ca_file` in their configuration file.
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
Exit codes are 0 for all checks passed, 1 for a failed check, and 2 for incomplete
evidence without a failure. The result reports `ready: false` for both failure
and incomplete evidence.

```sh
envy installation check --file installation.json
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

The Job supports configured HTTP or HTTPS ingress and distinct public/internal
ports. Set `preflight.path` and `preflight.expectedStatus` for the baseline's
business endpoint. HTTPS uses the existing `runtime.caConfigMap` when configured;
certificate verification is never disabled. PostgreSQL checks run an authenticated
`SELECT 1`, not only a server-readiness probe. This does not establish schema
compatibility. Run it after the borrowed baseline route exists.

For the private-LAN, API-only trial on Docker Desktop Kubernetes, see the
[LAN installation guide](lan-installation.md). The chart keeps ClusterIP defaults;
`service.type` and `service.nodePort` allow explicit operator-managed exposure.
For the ownership boundary when an existing baseline is deployed by Argo CD or
another GitOps controller, see [Argo CD, Helm, Istio, and Envy](argo-cd-integration.md).

Chart rendering is part of the repository validation gate:

```sh
make helm-lint
```

For the reproducible local acceptance path, `make test-helm` installs the chart
into a unique disposable namespace with its own PostgreSQL pod provisioned
separately from Helm. It waits for the migration hook and control-plane rollout,
then tests the deployed API through a local HTTPS reverse-proxy fixture. It checks
proxy header normalization, direct identity spoofing, ambiguous identities,
invalid bearer credentials, CSRF checks after cookies are stripped, and persisted
human/machine activity attribution. Test credentials are generated in restrictive
temporary files and removed with the release and temporary database. It never
connects to the development application database.

A second test exercises the existing Istio ingress gateway's real HTTPS listener.
It creates a temporary certificate Secret in `istio-system` and a Gateway and
VirtualService in the disposable namespace. Requests preserve public hostname/SNI
while dialing a loopback port-forward to ingress. The test checks trusted TLS,
rejection of an untrusted certificate, exact-host API and website access, bearer
authentication, unknown-host 404s, and route removal convergence. Cleanup removes
the test routing objects and certificate without removing the shared gateway.

The HTTPS test also deploys a separate three-service baseline, upgrades the chart
with `runtime.caConfigMap` pointing to its temporary trust certificate, and creates
one service-b v2 composition through REST. It waits for control-plane verification,
checks the returned HTTPS URL and baggage normalization at every hop, interleaves
baseline requests, and verifies workload identities. Destruction must reach a
404 endpoint and an absent owned namespace; the borrowed baseline must remain
unchanged. The test removes the borrowed fixture namespace afterward. Demo images
are rebuilt and loaded locally; no registry or external infrastructure is provisioned.

If ingress can serve the API but returns 503 connection-termination errors for
injected workloads, inspect the gateway's mesh certificate with
`istioctl proxy-config secret deployment/istio-ingressgateway -n istio-system`.
A valid public HTTPS certificate does not imply a valid mesh identity. During local
acceptance, an expired gateway identity was recovered by restarting that development
gateway. The test does not automatically restart shared infrastructure; investigate
certificate renewal before applying this recovery in an operator-managed cluster.

These tests require permission to create the temporary Secret in `istio-system`.
They use the local gateway's `istio: ingressgateway` selector and HTTPS service
port 443. The proxy fixture simulates an upstream session, without an identity
provider. External load balancers, network-policy enforcement, nondefault Istio
revisions, and private-registry credentials remain separate acceptance gates.

Uninstalling the chart retains PostgreSQL and compositions. Destroy compositions
through Envy and verify cleanup before removing the chart when data-plane cleanup
is intended. Baselines, Istio, proxy, certificates, and database infrastructure
remain operator-owned.

See [operations](operations.md) for backup, restore, and the two explicit
uninstall paths, and [compatibility](compatibility.md) for tested version lines.
