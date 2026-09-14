# First LAN deployment: storefront → pricing

This is a LAN API and web UI installation using the normal Envy Helm chart on
Docker Desktop Kubernetes. The validated target Mac used `192.168.2.217`; do
not assume that address is permanent. The other laptop needs only network
endpoints and an Envy credential. It does not need SSH, Kubernetes credentials,
a DNS change, or a local cluster.

HTTP is explicit for this private-LAN trial; credentials travel unencrypted.
HTTPS remains supported by the normal chart. The website, enterprise auth,
automatic PR environments, GitHub App discovery and hosted CI access to the LAN
are outside this test. Existing kind development tooling is not used here.

The tested endpoints are:

| Surface | Address | Purpose |
| --- | --- | --- |
| Envy control plane and web UI | `http://<LAN_IP>:30081` | API and dashboard |
| Istio application ingress | `http://<LAN_IP>:30080` | Baseline and preview traffic |

Use port `30081` for the dashboard and Envy API. Use port `30080` only for
baseline or preview application requests.

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
installation; do not reset existing clusters. Select Docker Desktop's **kubeadm**
Kubernetes runtime rather than Kind. Kind mode can keep Kubernetes' image store
separate from Docker's host image store and can fail to publish NodePorts to the
host. Install Git, Python 3, Docker CLI, kubectl, Helm 3.19.0 and istioctl 1.31.0.
Use the Kubernetes 1.36 line for the current tested compatibility range; record
the exact Desktop/Kubernetes versions.
Kyverno chart 3.8.2 is installed separately for registry-secret distribution,
using its official `ghcr.io/kyverno/*` images. The installer overrides the
chart's default `reg.kyverno.io` registry because Docker Desktop can otherwise
encounter truncated image layers during pulls.
The Go compiler is only needed for Go tests/CLI builds; the server builds in Docker.

Clone the committed Envy repository. The installation builds from a clean source
commit and tags the server with that commit. Commit tracked deployment changes
before running the installer; its clean-source guard deliberately rejects a
dirty tree. Check Docker Desktop uses an image store visible to its Kubernetes
cluster. The chart uses `imagePullPolicy: Never`: `ErrImageNeverPull` is a hard
setup failure, not a reason to substitute a mutable image or another cluster.
Inspect the migration Job if installation stalls.

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
service account does not gain them in this legacy-profile installation.
Opt-in [deployment-derived previews](deployment-derived-previews.md) use separately
approved copies and named source-read permissions. Only pricing is approved as overridable.

The script renders and deploys the two main workloads, and runs the preflight
Job after their `/products` route exists. Preflight authenticates to PostgreSQL
with `SELECT 1`, then checks HTTP status through ingress. It does not certify
schema compatibility or downstream routing. Catalog registration is a separate,
explicit API action below. PostgreSQL/PVCs are not owned by the Envy Helm release.

The Envy server image also serves the React web application from `/web`. In the
LAN values, the API service is a NodePort on `30081`, so the dashboard is opened
from that same origin. This avoids browser CORS problems and makes the default
empty frontend server URL resolve correctly.

For subsequent server upgrades, build a new clean commit tag and run only the
Envy `helm upgrade` command from the script. Do not rerun infrastructure installation
to upgrade unrelated components. Save `.envy/lan/*version*` and `releases.json`
with the acceptance report. Record Docker Desktop's version separately.

## Prove LAN access before creating compositions

Set the address to the Mac's current LAN address. The value below is the one
used by the validated installation:

```sh
LAN_IP=192.168.2.217
```

From the other laptop:

```sh
curl --noproxy '*' -i "http://${LAN_IP}:30081/v1/session"
curl --noproxy '*' "http://${LAN_IP}:30080/products" \
  -H 'Host: baseline.envy.test:30080'
```

The first request must reach Envy and return 401. The second must return the main
pricing response. Both connections must work without SSH or port-forward processes.
Allow Docker Desktop inbound access in the host firewall and keep the Mac awake.

If Desktop publishes NodePorts only on localhost, the optional persistent TCP
adapter binds specifically to the LAN address and forwards to Desktop's local
NodePorts. It preserves Host and all application bytes; it does not authenticate:

```sh
ENVY_LAN_IP="${LAN_IP}" docker compose -f deploy/lan/adapter.compose.yaml up -d
```

Use this only when direct NodePort access fails but localhost NodePort requests
work. Verify `host.docker.internal` reaches the NodePorts from that container;
if it does not, stop and report the Desktop networking limitation. Never silently
fall back to SSH. Re-run the remote probes and record whether direct NodePort or
the adapter was used. Check restart behavior; the adapter requires Docker Desktop
running. Remove it with `docker compose ... down`, not cluster deletion.

## Authenticate the web frontend

The LAN installation uses token authentication with a named machine credential.
`deploy/lan/prepare-secrets.py` stores the token from `.envy/lan/api-token` in
the `lan-client` credential record inside the `envy-machines` Kubernetes Secret.
The token is not embedded in the image or frontend bundle.

Open the dashboard at `http://${LAN_IP}:30081/`, not on the application ingress
port. Select **Connection settings** and enter:

```text
Server URL: http://${LAN_IP}:30081
API token: contents of .envy/lan/api-token
```

Select **Apply and test**. The frontend sends the token as an
`Authorization: Bearer ...` header and identifies itself with
`X-Envy-Channel: web`. The browser keeps the token in memory only and clears it
on reload; this is intentional. Transfer the token only through a secure
file-transfer mechanism and never put it in source control, a `VITE_*` build
variable, or a chat message.

If developing the frontend locally instead of using the deployed dashboard,
forward the API service and let the Vite server add the bearer token to its
same-origin proxy requests:

```sh
kubectl --context=docker-desktop -n envy-system \
  port-forward svc/envy-envy 8081:8081
export ENVY_API_TOKEN="$(cat .envy/lan/api-token)"
pnpm --dir web dev
```

Then open `http://localhost:5173`. Do not point the local Vite proxy at the
application ingress on `30080`.

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
python3 deploy/lan/catalog.py validate \
  --api "http://${LAN_IP}:30081" --ingress "http://${LAN_IP}:30080" \
  --token-file /private/envy-lan-token
python3 deploy/lan/catalog.py apply \
  --api "http://${LAN_IP}:30081" --ingress "http://${LAN_IP}:30080" \
  --token-file /private/envy-lan-token
python3 deploy/lan/acceptance.py \
  --api "http://${LAN_IP}:30081" --ingress "http://${LAN_IP}:30080" \
  --token-file /private/envy-lan-token
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

Common setup-specific failures:

- **Dashboard says “Server Offline”:** open the dashboard on `30081`, not
  `30080`, and set the server URL to the API NodePort. A 401 response means the
  server is reachable but the token is missing or invalid.
- **`ErrImageNeverPull`:** Docker Desktop is not exposing the host-built image to
  Kubernetes. Switch the Kubernetes runtime to kubeadm and rebuild the server
  image; do not replace the immutable installation image with a mutable tag.
- **Kyverno image pull failures:** retain
  `--set global.image.registry=ghcr.io` in the Kyverno Helm install. Docker
  Desktop can receive truncated layers from the chart's default
  `reg.kyverno.io` registry.
- **Secret policy admission denial:** Kyverno 3.8.2 validates generated
  policies through its admission controller. Keep the read-only Secret
  validation ClusterRole in `deploy/lan/secret-policy.yaml`; the broader
  Secret synchronization role belongs only to the background controller.
- **Preview pricing remains pending:** use a server image containing the quota
  fix. Istio injects a sidecar into each preview workload, so the generated
  quota now allows `3 CPU` and `2 GiB` per workload instead of `2 CPU` and
  `1 GiB`. Rebuild and Helm-upgrade the server after changing this code.

Repository tests and GitHub image builds can run before the target is available,
but they do not replace the deployment gates. Completion requires the private-pull
proof, direct LAN or adapter connectivity, catalog registration, successful
acceptance, and cluster-side confirmation that the composition namespace was
removed while PostgreSQL and the borrowed baseline remain ready.
