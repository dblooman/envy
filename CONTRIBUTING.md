# Contributing to Envy

Envy welcomes focused bug fixes, documentation improvements, and tests. Open an
issue before a substantial feature or architecture change so its scope can be
agreed. Contributions use the repository's MIT license; no separate CLA is required.

## Tools and first setup

Clone your fork, then work on a branch. Use Go **1.27.1 or newer** (the minimum in
`go.mod`), Node **24.8.0 or a newer Node 24 patch**, and **pnpm 10.20.0**.
`.nvmrc` pins the Node version used by CI and the dashboard container build.
With nvm installed, run `nvm install && nvm use`. Install pnpm using
`npm install --global pnpm@10.20.0`.

```sh
git clone https://github.com/dblooman/envy.git
cd envy
make setup
make help
```

`make setup` downloads Go modules and installs both JavaScript packages with frozen
lockfiles. It does not create a cluster. The packages have separate lockfiles;
run dependency updates within `web/` or `site/` and commit the matching lockfile.
Dependencies with approved install scripts are listed in each package manifest.

## Pick a development path

| Work | Commands | Additional tools |
| --- | --- | --- |
| Dashboard simulation | `cd web && pnpm dev` | None |
| Documentation | `make site-dev` | None |
| Go binaries and unit tests | `make build`, `make check-go` | Git |
| Full Kubernetes demo | `make doctor`, `make dev`, `make ui-dev` | Docker, kubectl, Python 3, curl |
| Complete fast checks | `make check` | Python 3, Helm 3.19+ (Helm 3) |

For dashboard simulation, open `http://localhost:5173`, turn on the
**Demo Simulation** switch in the sidebar. The initial disconnected state is expected without
an API. Simulation is an explicit browser preference and never provisions workloads.

Documentation runs at `http://localhost:4321/envy/`. See [site development](site/README.md)
for diagrams, checks, and deployment. Docs-only contributors can install just the
site dependencies with `cd site && pnpm install --frozen-lockfile`.

Go-only contributors can use `go mod download` and `make check-go` without Node or
Kubernetes. PostgreSQL tests requiring a database and cluster acceptance are
separate from the default local unit-test path. CI supplies a disposable PostgreSQL
service, so persistence tests run there. Locally set `ENVY_TEST_DATABASE_URL` to a
disposable database to run those tests; never point it at a shared installation.
A passing unit suite is not evidence of live cluster acceptance.

## Full local development

Use Docker with BuildKit and the Buildx plugin (included in Docker Desktop).
The bootstrap supports macOS and Linux on amd64/arm64. Windows users need WSL2
with Docker integration. Start with at least **4 CPUs and 8 GiB RAM allocated to
Docker**, plus **20 GiB free disk** for images and builds. These are practical
starting allocations, not measured minimums; a larger application may need more.

`make doctor` checks tool versions, Docker, the platform, and ports 8080/8081. It
makes no infrastructure changes. Bootstrap runs its own subset before creating
the cluster. `kind` and `istioctl` are downloaded with verified checksums into
`.envy/tools/`; the dedicated kubeconfig stays under `.envy/envy-dev/`.

```sh
make dev
curl --fail http://baseline.envy.localhost:8080/
make ui-dev
```

`make ui-dev` reads the generated local API token on the server side of Vite and
proxies API calls to `127.0.0.1:8081`. Turn **Demo Simulation off** in the sidebar for
live data. The token is not compiled into the browser bundle. Keep development
servers bound to loopback; do not use `--host 0.0.0.0` with administrative credentials.

For CLI work:

```sh
export ENVY_API_URL=http://127.0.0.1:8081
export ENVY_API_TOKEN_FILE="$PWD/.envy/envy-dev/api-token"
.envy/bin/envy --help
```

Follow the [quickstart](https://dblooman.github.io/envy/getting-started/quickstart/)
for creating, inspecting, updating, and destroying a composition. The local
baseline is named `staging`. Istio and Cilium are accepted profiles; Linkerd is
blocked pending current-generation route status from its controller.

- Go/control-plane changes: rerun `bash deploy/local/control-plane.sh` to rebuild
  the server image and CLI/MCP binaries, reload the image, and restart the server.
- Demo service changes: rerun `make dev` to rebuild and reconcile the local stack.
- Dashboard changes: Vite hot reloads; the deployed bundle updates on a server rebuild.
- Cleanup: `make dev-down` deletes only the named local cluster. Ignored `.envy/`
  diagnostics and credentials remain on disk; never commit or attach that directory.

Use `ENVY_CLUSTER_NAME`, `ENVY_PREVIEW_PORT`, and `ENVY_API_PORT` to isolate another
stack; pass the same values to subsequent commands and teardown. `make ui-dev`
uses these same variables for its token path and local API proxy.

If setup fails, run `make doctor`, inspect the reported `.envy/<cluster>/` diagnostics,
and check Docker resources. For occupied ports choose unused values; for missing
images or download failures check registry/network access. Do not delete a cluster
you did not create. Initial downloads and builds depend on the host and network. Docker BuildKit caches
Go compilation across demo variants and subsequent server rebuilds.

## Checks and pull requests

Run `make check` before submitting. This covers Go formatting/vet/tests, dashboard
lint/tests/build, documentation lint/types/format/diagrams/build/links, lightweight
integration tests, Helm rendering, and pinned chart consistency. Neither `make check`
nor `make setup` requires Docker. `make doctor` checks the broader local environment.

Use `pnpm lint`, `pnpm format`, and `pnpm check` inside either frontend. ESLint checks
TypeScript, React Hooks, and accessibility; Prettier formats handwritten frontend
sources. Generated builds, dependency trees, and lockfiles are excluded from formatting.
Regenerate SQL with `make sqlc` after schema/query changes using sqlc, and include the
resulting code in the same change.

For routing/lifecycle changes, run `make test-e2e` in a disposable cluster. It checks
baseline traffic, authenticated API operations, preview routing, and cleanup.
`make test-mesh MESH=istio` or `MESH=cilium` runs the broader mesh acceptance suite;
CI keeps those resource-heavy jobs separate from fast checks.

Explain the problem, resulting behavior, tests run, and known limitations in your PR.
Add regression tests for behavior fixes and update the affected README/site examples.
Do not add credentials, kubeconfigs, or private installation details. See
[SECURITY.md](SECURITY.md) for private vulnerability reporting.
