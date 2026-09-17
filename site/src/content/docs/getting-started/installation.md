---
title: Install a Release
description: Install the released Envy CLI and Helm chart, configure access, and continue to application onboarding and GitHub PR previews.
---

Install Envy on an existing Kubernetes mesh, then register a running application
and enable [GitHub PR previews](/integrations/github-app/). You do not need Go or
a source build to use the released CLI and control plane. To experiment on your
laptop instead, use the [local quickstart](/getting-started/quickstart/).

## 1. Choose a release and install the CLI

Choose a stable version from [GitHub Releases](https://github.com/dblooman/envy/releases)
and read its release notes. The examples below pin **0.3.0**; use the same selected
version for the CLI, chart, and server image.

| Platform             | Release archive            |
| -------------------- | -------------------------- |
| macOS, Apple silicon | `envy_darwin_arm64.tar.gz` |
| macOS, Intel         | `envy_darwin_amd64.tar.gz` |
| Linux, ARM64         | `envy_linux_arm64.tar.gz`  |
| Linux, x86-64        | `envy_linux_amd64.tar.gz`  |
| Windows, x86-64      | `envy_windows_amd64.zip`   |

Download your archive and `SHA256SUMS` from the **same release**. For example,
on macOS with Apple silicon:

```sh
ENVY_VERSION=0.3.0
ENVY_ARCHIVE=envy_darwin_arm64.tar.gz
curl --fail --location --remote-name "https://github.com/dblooman/envy/releases/download/v${ENVY_VERSION}/${ENVY_ARCHIVE}"
curl --fail --location --remote-name "https://github.com/dblooman/envy/releases/download/v${ENVY_VERSION}/SHA256SUMS"
shasum -a 256 "$ENVY_ARCHIVE"
```

Compare the full hash with the line for that archive in `SHA256SUMS`. On Linux,
use `sha256sum "$ENVY_ARCHIVE"`; on Windows, use
`Get-FileHash .\envy_windows_amd64.zip -Algorithm SHA256` in PowerShell. Stop if
the hashes differ.

After verification, extract the archive and put its `envy` binary on your `PATH`.
For the macOS example:

```sh
tar -xzf "$ENVY_ARCHIVE"
mkdir -p "$HOME/.local/bin"
install -m 0755 envy_darwin_arm64/envy "$HOME/.local/bin/envy"
export PATH="$HOME/.local/bin:$PATH"
envy version
```

For another platform, use its matching extracted directory. On Windows, extract
the ZIP and add the directory containing `envy.exe` to your user `PATH`.
Persist any PATH change in your shell configuration. `envy version` reports the
release and commit; see the [CLI reference](/reference/cli/) for usage.

Release archives contain the CLI, not the separate stdio MCP adapter. Teams can
connect an agent to the installed server's `/mcp` endpoint; the
[MCP guide](/agents/mcp-server/) also explains building the local adapter.

## 2. Prepare the cluster

Use Helm 3.19+ (Helm 3) and `kubectl` with access to your target cluster.
Before installation, prepare:

- A supported Istio, Cilium, or Linkerd configuration, with its required ingress
  controller and Gateway resources. Follow [Choose Your Mesh](/getting-started/mesh-installation/)
  for the tested profiles, manifests, and preflight configuration.
- PostgreSQL and an operator-managed Secret containing its connection URL.
- A stable installation ID, wildcard preview DNS and TLS, and a reachable
  ingress address for the controller's verification requests.
- An HTTPS origin for the Envy dashboard/API, appropriate network access, and
  [authentication](/guides/authentication/) configured for your team.
- A running baseline application whose services propagate W3C Baggage, plus
  pull access for the images you intend to deploy.

The chart installs Envy's control plane, migration Job, Service, RBAC, and
configuration. It does not install your mesh, database, baseline, DNS, certificates,
or public API ingress. Secrets and identity-provider setup remain operator-managed.

Take example manifests from the selected release tag, rather than mixing released
binaries with examples from `main`. Start with the matching profile under
[`deploy/examples`](https://github.com/dblooman/envy/tree/v0.3.0/deploy/examples)
and adapt its values and `installation.json` to your infrastructure.

## 3. Preflight and install

Run the read-only check with your adapted specification:

```sh
envy installation check --file installation.json
```

Exit `0` means all checks passed, `1` means a failure, and `2` means incomplete
evidence. Controller-network checks can remain unknown locally; follow the
[mesh installation guide](/getting-started/mesh-installation/#3-preflight-and-install-envy)
to run the optional in-cluster preflight Job. Do not treat unknown as success.

Install with your prepared values:

```sh
helm upgrade --install envy oci://registry-1.docker.io/davey/envy-chart \
  --version 0.3.0 --namespace envy-system --create-namespace \
  --values values.yaml
kubectl -n envy-system rollout status deployment/envy-envy
```

With release name `envy`, the Deployment is `envy-envy`. The chart defaults to
`davey/envy:0.3.0`; keep that image aligned with the chart. Pin versions instead
of using the rolling `latest` image tag. Chart archives are also attached to
GitHub Releases. The repository's `deploy/helm/envy` directory is for source
contributors and local chart development.

## 4. Connect and onboard

Once your HTTPS dashboard/API origin is reachable, configure the CLI. For a
password or Google installation:

```sh
export ENVY_API_URL="https://envy.example.com"
envy auth login
envy auth status
```

Replace the example origin with your installation. For proxy authentication or
unattended clients, follow the [authentication guide](/guides/authentication/)
for machine credentials.

Continue in this order:

1. [Onboard an application](/getting-started/onboarding/): register approved
   components and a baseline; create and verify a smoke preview.
2. [Set up the GitHub App](/integrations/github-app/): configure credentials,
   build reporting, and an enabled repository policy.
3. Label a same-repository PR `envy-preview`, wait for successful builds of its
   exact head commit, and inspect the preview in GitHub or the
   [dashboard](/guides/web-interface/).

## Upgrading an installation

Read the target release notes, back up PostgreSQL according to your operator
procedures, and review changed chart values before rerunning the version-pinned
Helm command with your maintained values file. Install the matching CLI and
verify rollout, authentication, and a disposable preview through cleanup.

The pre-upgrade migration Job runs before the server starts. Helm rollback does
not reverse database migrations; follow the
[operations guide](https://github.com/dblooman/envy/blob/main/docs/operations.md)
for recovery. For detailed Secret, networking, and uninstall behavior, see the
[installation reference](https://github.com/dblooman/envy/blob/main/docs/installation.md).
