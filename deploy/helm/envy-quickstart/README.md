# Local evaluation chart

This chart is a separate product from the operator-managed `envy-chart`.
It installs the pinned Istio sidecar mesh, a persistent PostgreSQL instance,
Envy, and a real two-service Tea shop baseline. Use a dedicated cluster and the
fixed release/namespace `envy` / `envy-quickstart`. Cluster-admin installation
permissions and a default storage class are required.

## Packaging and release

Maintainers run `bash scripts/package-quickstart.sh dist` from the repository
root. This builds locked dependencies and extracts Istio's upstream CRDs into
Helm's install-first `crds/` directory. Generated dependency archives and CRDs
are ignored by Git but included in the published chart. No dependency download
or setup script is required on the user's machine.

The release workflow publishes `davey/envy-quickstart:X.Y.Z`, plus the multiarch
`davey/envy-demo:X.Y.Z-v1` and `X.Y.Z-v2` sample images. The Docker Hub PAT must
allow creating/pushing the quickstart chart and demo image repositories.

Chart/app versions, the Envy dependency, its image version, and sample tags must
match the release. Both charts remain on the same version. Publishing this chart
requires the new server image: it contains the bounded in-cluster bootstrap Job.

## Lifecycle

- Istio CRDs are installed before Gateway/VirtualService resources. The base
  dependency does not separately take ownership of those CRDs.
- Envy uses an init-container migration in this profile, waiting through database
  startup. The standard chart retains its pre-install/pre-upgrade migration Job.
- The bootstrap Job waits for Istio's injector, replaces only uninjected sample
  and ingress pods, waits for readiness, and registers the catalog through the
  authenticated API. It has read/patch access only to the named deployments in
  the two quickstart namespaces, plus injector discovery. It does not install
  Helm releases or obtain cluster-admin privileges.
- Use `--wait --wait-for-jobs` so success includes catalog registration.
- The generated database Secret and PVC are retained on uninstall. A reinstall
  reuses the password; a retained volume without credentials is rejected.
- Destroy previews before uninstalling: their namespaces belong to Envy, not
  Helm. CRDs are retained by Helm; reset the dedicated cluster for a clean slate.
- This release's baseline is immutable. An upgrade that changes its catalog
  definition requires a fresh evaluation database; do not use this chart as a
  production upgrade mechanism.

One gateway port-forward exposes both dashboard and preview HTTP hostnames.
Authentication is tied to `http://127.0.0.1:8080`. Public production ingress,
Google identity, TLS, backups, and database HA belong to the standard chart path.

## Maintainer verification

`make test-mesh-charts` packages and renders the stack without a cluster.
For live acceptance, install the packaged chart into a disposable cluster,
forward `service/envy-ingress` to a free local port, then run
`python3 deploy/testing/quickstart-acceptance.py PATH_TO_KUBECONFIG LOCAL_PORT`.
This tests password login, creates a pricing preview, checks 990 versus the
baseline's 1200, and destroys it. It is a test harness, not a user installer.
