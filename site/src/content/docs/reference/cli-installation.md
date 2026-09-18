---
title: Install the CLI
description: Download the optional Envy CLI and connect it to an existing installation.
---

The `envy` CLI is an optional client for scripting and terminal workflows.
You do not need it to install the Helm chart or use the web interface.

## Download, verify, and install

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
release and commit as JSON; see the [CLI reference](/reference/cli/) for usage.

Release archives contain the CLI, not the separate stdio MCP adapter. Teams can
connect an agent to the installed server's `/mcp` endpoint; the
[MCP guide](/agents/mcp-server/) also explains building the local adapter.

## Connect to Envy

Set `ENVY_API_URL` to the same origin you use to open the dashboard. For the
[Helm evaluation install](/getting-started/installation/), keep the port-forward
running and use:

```sh
export ENVY_API_URL=http://localhost:8081
envy auth login
envy auth status
```

For a shared installation, replace that URL with its actual HTTPS address.
Password and Google modes open the browser for login and authorization. The
source development stack's explicit `dev` mode requires no login.

The CLI defaults to `http://127.0.0.1:8081`, but browser authentication needs the
exact configured origin. `localhost` and `127.0.0.1` are different origins.
Do not append `/v1`; the CLI adds API paths itself.

See the [command reference](/reference/cli/) for JSON output, flags, and commands,
or [authentication](/guides/authentication/) for machine credentials.
