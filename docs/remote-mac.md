# Development on a second Mac

Run Docker, kind, Istio, PostgreSQL, Envy and demo workloads on the more capable
Mac. Run the browser, delivery CLI and stdio MCP client on your everyday Mac.
SSH forwards traffic over the LAN; Envy and Kubernetes remain bound to loopback.
This is a private development installation, not the enterprise Helm deployment.

## Prepare the cluster Mac

The initial target is Apple Silicon at `192.168.1.172`. Power it on and enable
System Settings → General → Sharing → Remote Login for your macOS account.
Verify SSH from the client Mac with `ssh YOUR_USER@192.168.1.172`, checking its
host-key fingerprint through the target machine. An SSH config alias is also
supported. No password or SSH private key belongs in this repository.

Install and start Docker Desktop, and install the repository's pinned Go version
(from `go.mod`), kubectl, Git and Python 3. Docker Desktop's separate Kubernetes
cluster is unnecessary: the setup creates its own `envy-dev` kind cluster.
Allocate enough Docker resources for the demo and overrides; start with 16 GB
of the machine's 32 GB and adjust based on observed usage. Keep the Mac awake
while testing. Reserve its LAN address if your router supports DHCP reservations.

Clone or transfer the committed repository into an absolute path, such as
`/Users/YOUR_USER/Developer/envy`. Do not copy `.envy`, kubeconfigs, credentials,
node_modules or Docker state from the old Mac. Both Macs should use the same Git
revision for initial acceptance. Build images natively on the target Mac.

On the **cluster Mac**, from the checkout:

```sh
make remote-dev
```

Use `make remote-dev` for later rebuilds too; it sets preview port 18080 and
API port 18081. They are chosen to
avoid this client's existing local environment on 8080/8081. kind host mappings
are fixed at cluster creation: changing environment variables does not remap an
existing cluster. If `envy-dev` already exists on the target, inspect its mappings
before proceeding rather than deleting any environment you want to retain.

The local PostgreSQL volume survives server restarts. Removing the kind cluster
removes its development database. The target setup creates fresh local credentials.
There is no automatic migration of existing compositions or GitHub App settings.

## Connect the client Mac

From this checkout on the **client Mac**:

```sh
python3 deploy/remote/connect.py \
  --host YOUR_USER@192.168.1.172 \
  --checkout /Users/YOUR_USER/Developer/envy
```

Leave that terminal running. It forwards preview port 18080, API/web port 18081,
and Kubernetes on local port 18443. All forwarded sockets bind to 127.0.0.1.
It fetches the target's development token and flattened kubeconfig over SSH into
ignored `.envy/remote` files with restrictive permissions. Kubernetes certificate
verification is preserved; the default kubeconfig and current context are not edited.
The copied kind credentials have development-cluster administrator access.

In another client terminal:

```sh
source .envy/remote/client.env
kubectl get nodes
make build
.envy/bin/delivery composition list --project demo
python3 deploy/remote/smoke.py
```

The smoke test runs from this Mac against the other Mac: create/retry, verify v2,
update to v3 with the same URL, verify unchanged baseline identities, destroy,
and require the returned preview hostname to stop forwarding. It explicitly
preserves Host while dialing loopback, so command-line acceptance does not rely
on wildcard localhost DNS. It creates only a temporary demo composition and
requests cleanup on failure; its TTL is ten minutes.

Open `http://127.0.0.1:18081` for the deployed website. Use its explicit development
token entry with `.envy/remote/api-token`; the browser does not persist the token.
Returned composition URLs use `*.envy.localhost:18080` and reach the remote cluster
through the tunnel. If a browser does not resolve wildcard localhost, configure
local DNS before browser acceptance; the smoke test's explicit dialing does not
prove browser DNS works.

Configure your agent's stdio MCP server to run this Mac's absolute
`.envy/bin/envy-mcp` path with these environment variables:

```text
ENVY_API_URL=http://127.0.0.1:18081
ENVY_API_TOKEN_FILE=/ABSOLUTE/CLIENT/CHECKOUT/.envy/remote/api-token
```

Use `list_projects`, `list_components`, `create_composition`,
`wait_for_composition`, and `destroy_composition` from the agent. Kubernetes
credentials are for diagnostics; the CLI/MCP workflow only needs API access.
GitHub builds, private registry credentials and externally hosted frontends are
separate integrations. A hosted frontend or GitHub webhook cannot call these
loopback URLs; test a local frontend through the tunnel first.

## Disconnect and diagnose

Ctrl-C stops the SSH tunnel, not the remote cluster or compositions. Expiry keeps
running on the cluster Mac. Remove `.envy/remote` when you no longer want the
copied development credentials on the client. Reconnect to refresh credentials
or the Kubernetes API port after a target cluster recreation.

If ports are occupied, stop the conflicting tunnel/process or choose matching
preview/API ports during target setup and connection. Never point a client at a
local API accidentally: verify its catalog and Kubernetes nodes before testing.
If injected applications return 503 while the API works, check the gateway's
mesh certificate expiry as described in [installation](installation.md).

Validation here covers connection configuration tests and the local HTTPS
composition gate. Cross-Mac, browser and actual-agent acceptance must be recorded
once the target Mac is running and accessible.
