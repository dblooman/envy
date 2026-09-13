# First LAN deployment: storefront → pricing

This is an API-only installation using the normal Envy Helm chart on Docker
Desktop Kubernetes, initially targeting the Apple Silicon Mac at `192.168.1.172`.
The other laptop needs only network endpoints and an Envy credential. It does not
need SSH, Kubernetes credentials, a DNS change, or a local cluster.

HTTP is explicit for this private-LAN trial; credentials travel unencrypted.
HTTPS remains supported by the normal chart. The website, enterprise auth,
automatic PR environments, GitHub App discovery and hosted CI access to the LAN
are outside this test. Existing kind development tooling is not used here.

## Build inputs already prepared

`deploy/lan/builds.json` records three successful multi-architecture GitHub Actions
builds, with repositories, source commits, run IDs, attempts and immutable GHCR
digests. These are private packages:

- Storefront main: `493e843ee25d9fdb201c824c15066c0692f2dfc5`.
- Pricing main: `6ede0296617719def9a50a571dae9a3eb8c9cea7`, price 1090.
- Pricing branch `codex/lan-pricing-trial`: `6a1b60565bf9cfa18456948ac18ca64f00971020`, price 1050.

The branch changes source code, with the same v1 build argument as main. Main
builds remain automatic; branch builds are explicitly dispatched and never deploy.
Actions only pushes images and uploads the existing `envy-build-report` artifact.
Download new artifacts with `gh run download RUN --repo OWNER/REPO --name
envy-build-report` when deliberately changing the recorded inputs. Do not infer
source provenance from mutable image tags. Both main images are deployed explicitly;
only the branch image is deployed by a composition request.

## Cluster Mac prerequisites

Start Docker Desktop and enable its Kubernetes cluster. Use a dedicated test
installation; do not reset existing clusters. Install Git, Python 3, Docker CLI,
kubectl, Helm 3.19.0 and istioctl 1.31.0. Use the Kubernetes 1.36 line for the
current tested compatibility range; record the exact Desktop/Kubernetes versions.
Kyverno chart 3.8.2 is installed separately for registry-secret distribution.
The Go compiler is only needed for Go tests/CLI builds; the server builds in Docker.

Clone the committed Envy repository. The installation builds from a clean source
commit and tags the server with that commit. Check Docker Desktop uses an image
store visible to its Kubernetes cluster. The chart uses `imagePullPolicy: Never`:
`ErrImageNeverPull` is a hard setup failure, not a reason to substitute a mutable
image or another cluster. Inspect the migration Job if installation stalls.

On the cluster Mac, prepare a minimal Docker config with only a read-only GHCR
credential authorized to pull both private packages. Store it outside the repo,
with mode 0600. Its format is `{"auths":{"ghcr.io":{"auth":"BASE64_USER_COLON_TOKEN"}}}`.
Do not pass tokens in command arguments or copy a credential-helper config: this
file must contain the actual GHCR credential, scoped to package reads. Package
access must be granted to that account independently of repository visibility.

```sh
export ENVY_GHCR_CONFIG_FILE=/absolute/private/path/ghcr-readonly.json
bash deploy/lan/install.sh
```

Every Kubernetes/Helm command explicitly selects `docker-desktop`. The script
provisions PostgreSQL with a separate 5Gi PVC, installs Istio, installs Kyverno
and its scoped generation policy, then installs the Envy chart. Credentials are
generated in ignored `.envy/lan` files with restrictive permissions and created
as separate Secrets. Existing Secrets are retained, never silently rotated.
Keep the original local credential files on reinstall; a newly generated local
file does not replace an existing cluster Secret.

Kyverno, not Envy, distributes `envy-ghcr` to namespaces labeled
`envy.dev/installation=envy-lan` and the explicit `envy-lan-baseline` namespace.
The policy is based on [Kyverno's secret synchronization example](https://kyverno.io/policies/other/sync-secrets/sync-secrets/).
Kyverno has Secret permissions for this infrastructure responsibility; Envy's
service account does not gain them. Only pricing is approved as overridable.

The script renders and deploys the two main workloads, and runs the preflight
Job after their `/products` route exists. Preflight authenticates to PostgreSQL
with `SELECT 1`, then checks HTTP status through ingress. It does not certify
schema compatibility or downstream routing. Catalog registration is a separate,
explicit API action below. PostgreSQL/PVCs are not owned by the Envy Helm release.

For subsequent server upgrades, build a new clean commit tag and run only the
Envy `helm upgrade` command from the script. Do not rerun infrastructure installation
to upgrade unrelated components. Save `.envy/lan/*version*` and `releases.json`
with the acceptance report. Record Docker Desktop's version separately.

## Prove LAN access before creating compositions

From the other laptop:

```sh
curl --noproxy '*' -i http://192.168.1.172:30081/v1/session
curl --noproxy '*' http://192.168.1.172:30080/products \
  -H 'Host: baseline.envy.test:30080'
```

The first request must reach Envy and return 401. The second must return the main
pricing response. Both connections must work without SSH or port-forward processes.
Allow Docker Desktop inbound access in the host firewall and keep the Mac awake.

If Desktop publishes NodePorts only on localhost, the optional persistent TCP
adapter binds specifically to the LAN address and forwards to Desktop's local
NodePorts. It preserves Host and all application bytes; it does not authenticate:

```sh
ENVY_LAN_IP=192.168.1.172 docker compose -f deploy/lan/adapter.compose.yaml up -d
```

Use this only when direct NodePort access fails but localhost NodePort requests
work. Verify `host.docker.internal` reaches the NodePorts from that container;
if it does not, stop and report the Desktop networking limitation. Never silently
fall back to SSH. Re-run the remote probes and record whether direct NodePort or
the adapter was used. Check restart behavior; the adapter requires Docker Desktop
running. Remove it with `docker compose ... down`, not cluster deletion.

## Prove private image access on the cluster Mac

Before deploying a composition:

```sh
python3 deploy/lan/private-pull.py
```

The test uses the branch digest with `imagePullPolicy: Always`. An uncredentialed
namespace must show an authentication-related pull error; a labeled namespace must
receive `envy-ghcr` and successfully start the same image. Failure on both paths is
not a pass. Anonymous success fails the gate because public packages or node-level
credentials would invalidate it. Run on a fresh target before the branch has been
pulled; Always additionally forces registry resolution even when layers are cached.
This test removes its two temporary namespaces and writes `private-pull.json`.

## Register the baseline and run remote acceptance

Transfer only the named client's API token from `.envy/lan/api-token` to a private
file on the client laptop using your chosen secure file-transfer mechanism.
Do not transfer kubeconfig or registry credentials. Both checkouts must use the
same committed `builds.json`.

From the client checkout:

```sh
python3 deploy/lan/render.py --builds deploy/lan/builds.json
python3 deploy/lan/catalog.py validate --token-file /private/envy-lan-token
python3 deploy/lan/catalog.py apply --token-file /private/envy-lan-token
python3 deploy/lan/acceptance.py --token-file /private/envy-lan-token
```

`--api` and `--ingress` override the defaults if the address changes. The latter
is the connection address; returned preview hostnames remain `*.envy.test:30080`.
The test uses explicit Host headers and does not depend on public or local DNS.
An ordinary agent uses the same REST API and named credential; no Envy Python
server or custom deployment service is involved. Python here only automates the
operator/client test steps.

Acceptance verifies invalid credentials, baseline identity, creation and idempotent
retry, branch business behavior, context at both hops, repeated baseline/preview
interleaving, hostile baggage normalization, unknown hosts and destroyed-host 404s.
Envy reports ordinary HTTP reachability for this application's contract; the
external test independently verifies routing. Credentials and raw request headers
are not included in the report. Failed runs request deletion and report any pending
cleanup; TTL is ten minutes.

On the cluster Mac, take the composition ID from `.envy/lan/acceptance.json` and
confirm `kubectl --context=docker-desktop get namespace envy-ID --ignore-not-found`
returns nothing. Confirm both baseline Deployments and PostgreSQL remain ready.
After all compositions are destroyed, `helm uninstall envy -n envy-system
--kube-context docker-desktop` must leave PostgreSQL/PVC and borrowed baseline
resources present. Reinstall with the same installation ID and credentials to
verify catalog persistence. Record these cluster-side checks separately: the remote
API test deliberately has no Kubernetes access.

## Failure evidence and completion boundary

Use bounded `kubectl logs --tail=100`, Pod status/events, migration/preflight Job
logs and `istioctl proxy-config secret` on the cluster Mac. Never collect Secrets
or raw credential files into an acceptance bundle. An expired ingress mesh identity
can produce 503s for injected workloads even when the API responds; investigate
certificate renewal rather than disabling TLS verification inside the mesh.

Repository tests and GitHub image builds can run before the target is available.
Installation, private-pull proof, direct LAN/adapter connectivity and the complete
cross-laptop report remain pending until the target Mac is powered on. A successful
render or local unit test is not evidence that those deployment gates have passed.
