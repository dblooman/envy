# Source revisions and published builds

The workflow is: push a commit → existing CI builds and pushes an image → CI
reports the source-to-digest mapping → select a build → Envy deploys its immutable
image with the approved profile → Kubernetes/Istio readiness checks run.

Git commit SHAs and OCI image digests are different identities. Envy stores both.
Tags are not used to infer provenance. A historical commit uses the approved
profile and current shared baseline; it does not restore historical staging.
For the recommended handoff between a GitOps-managed baseline and Envy's
short-lived compositions, see [Argo CD, Helm, Istio, and Envy](argo-cd-integration.md).

## Server setup

Create a GitHub App with repository **Contents: read** permission (Metadata read
is implicit). Install it on the repositories you want to make available, using
GitHub's selected-repositories installation option. No webhook, Actions write
permission, or CI dispatch permission is required.

Configure the control plane:

- `ENVY_GITHUB_APP_ID`: GitHub App identifier.
- `ENVY_GITHUB_APP_PRIVATE_KEY_FILE`: mounted PEM RSA private key, readable by the
  server's user. The server signs short-lived JWTs and requests installation
  tokens scoped to the repository being queried. GitHub.com is supported.
- `ENVY_BUILD_CREDENTIALS_FILE`: mounted JSON array of scoped CI credentials:

```json
[
  {
    "token": "REPLACE_WITH_A_RANDOM_TOKEN_AT_LEAST_32_CHARACTERS",
    "project": "shop",
    "repository": "backend",
    "components": ["pricing", "storefront"]
  }
]
```

Keep this file and the App key outside source control; configure them as server
secrets. Each token must be unique and different from the main API token. Restart
the server to rotate credentials. CI tokens can only POST build reports to their
exact project/repository and listed components. They cannot browse repositories,
register catalog entries, or create/update compositions. The existing main bearer
token remains the trusted operator/UI credential; it cannot report builds.

The registry reader uses the server's Docker credential configuration, located
via `DOCKER_CONFIG` or the standard Docker config path. Mount a readable config
with read-only registry credentials. Credentials never appear in API catalog
responses. Google Artifact Registry and ECR work through OCI registry APIs; renew
short-lived credentials externally. Credential helpers work when installed in
the runtime, but the shipped scratch image contains no helper executables.

Kubernetes needs its **own** pull access. Configure node credential providers or
an operator-managed image-pull-secret integration for Envy's generated namespaces
and `envy-workload` service account. The control plane does not copy baseline
secrets or registry credentials into workloads. Registry lookup success does not
prove the node can pull an image; normal rollout failures surface pull errors.

The server image includes the public CA bundle for GitHub and registry HTTPS.
Private CA/registry auth deployment remains operator configuration. GitHub App
credentials are optional: direct-image compositions still work without them.

For an existing local kind environment, attach an App key with:

```sh
ENVY_GITHUB_APP_ID=YOUR_APP_ID \
ENVY_GITHUB_APP_PRIVATE_KEY_FILE=/absolute/path/to/private-key.pem \
bash deploy/local/github-app.sh
```

The script uses the dedicated `envy-dev` kubeconfig (or `ENVY_CLUSTER_NAME`),
mounts a separate Kubernetes Secret read-only, and restarts the control plane.
Keep the local PEM outside source control with mode `0600`. Re-run the script
after key rotation or cluster recreation. Installation and repository selection
remain GitHub account configuration.

For local private image access and build reporting, supply an inline Docker
registry config and the scoped build credentials JSON described above:

```sh
ENVY_REGISTRY_CONFIG_FILE=/absolute/path/to/config.json \
ENVY_BUILD_CREDENTIALS_FILE=/absolute/path/to/build-credentials.json \
bash deploy/local/build-access.sh
```

This mounts a separate read-only server Secret and configures the dedicated
kind nodes using [kind's node credential approach](https://kind.sigs.k8s.io/docs/user/private-registries/).
Existing credentials for other registries are preserved. Credentials are
available for pulls throughout this trusted development cluster. Use a registry
credential with read access, keep both input files at mode `0600`, and re-run
after rotation or cluster recreation. Docker credential helpers are not supported
by the shipped server image.

GitHub-hosted Actions cannot report directly to a loopback Envy API. For local
testing, upload the immutable build report as an Actions artifact, download it
with `gh run download`, and submit the unchanged report using `delivery source
report` and its repository-scoped token. This is an operator-driven import;
it does not establish automatic CI connectivity or expose the API publicly.

The [local preview helper](../integrations/local-preview/README.md) combines this
import with create/update, exact frontend binding, local build/serve, rollback
and explicit cleanup in one repeatable command.

## Register a source repository

First register the project's existing infrastructure and approved component
profiles using catalog onboarding. Then register repository/image mappings in
the Catalog UI or with `delivery source register --file repository.json`:

```json
{
  "project": "shop",
  "id": "backend",
  "github_repository": "acme/shop",
  "installation_id": 123456,
  "enabled": true,
  "images": {
    "pricing": "us-docker.pkg.dev/example/previews/pricing",
    "storefront": "us-docker.pkg.dev/example/previews/storefront"
  }
}
```

The server validates App access and approved component existence. A repository
can map multiple components; a component belongs to one repository within its
project. The existing component `repository` metadata remains informational;
these explicit mappings control source eligibility. Repository identity and image
mappings are immutable, and identical
registration retries succeed. The profile still defines ports, probes, literal
environment, resources and workload identity. No Helm chart or YAML from a
selected source revision is executed.

Enable/disable via Catalog or:

```sh
delivery source disable --project shop --repository backend
delivery source enable --project shop --repository backend
```

Disabled repositories reject lookups, build reporting, new build-backed
compositions, and updates using their builds. Existing compositions keep running
and expire normally. Removing App access also blocks future lookups and selection;
registration listings retain the saved mapping so operators can diagnose it.
Disable is checked again under a database lock when persisting build/deployment
intent. A GitHub access change after lookup is not an atomic external-system lock.

## Resolve and deploy

```sh
delivery source list --project shop
delivery source branches --project shop --repository backend
delivery source commits --project shop --repository backend --ref main
delivery source resolve --project shop --repository backend \
  --component pricing --ref main
# Resolve a historical full lowercase SHA by supplying it to --ref instead.
delivery composition create --project shop --baseline staging --name pricing-review \
  --build pricing=BUILD_ID --ttl 8h
delivery composition update COMPOSITION_ID --expected-generation 1 \
  --build pricing=ANOTHER_BUILD_ID
```

`--build component=build_id` can be repeated and mixed with `--override
component=image` for distinct components, up to the existing three-component
limit. Updates retain the full override set, generation checks, URL and expiry.
Direct images explicitly have no source provenance.

The UI supports branch browsing, commit history, full SHA entry, and explicit
build selection. A branch is resolved to one SHA; refreshing builds uses that
frozen SHA. To pick a later branch head, resolve the branch again. GitHub pages
contain 30 entries; REST/client/MCP expose numeric page parameters. Build and
repository results use cursor pagination (20 default, 100 maximum). Use the
resolved SHA when requesting further build pages.

A valid commit with no build returns a successful resolution and an empty build
list, with a CI URL. Envy neither dispatches CI nor substitutes an older build.
An invalid/inaccessible revision, disabled repository or upstream lookup failure
is a separate error. Historical artifacts deleted by retention cannot be deployed;
registry availability is rechecked during create/update.

Build IDs identify project, repository, component, Actions run ID and attempt.
Reports are immutable, with conflicting retries returning 409. Multiple attempts
for one commit are retained and require explicit selection. Composition responses
include `image`, `build_id` and server-owned `source` provenance. Create/update
inputs accept exactly one of `image` or `build_id`; callers must omit `source`.

The API is documented in `api/openapi.yaml`. MCP adds
`list_source_repositories`, `list_source_branches`, `list_source_commits` and
`resolve_source_revision`; existing create/update tools accept build IDs.
[GitHub Actions integration](../integrations/github-actions/README.md) contains
a reporting adapter and a workflow step for existing builds.

## Verification

Run `go test -race ./...`, with `ENVY_TEST_DATABASE_URL` set to a disposable
PostgreSQL database for persistence tests, and `pnpm --dir web test` plus
`pnpm --dir web build`. Provider tests exercise GitHub token scope and revision
escaping, and a real local OCI registry. PostgreSQL tests exercise concurrent
idempotency, immutable reports, monorepo claims, build pagination, disable checks,
provenance persistence, and generation-checked updates. UI tests cover explicit
selection, historical SHAs, empty build lists and stale asynchronous responses.

Run the existing Kubernetes/Istio regression scenarios in a fresh cluster with:

```sh
ENVY_CLUSTER_NAME=envy-builds-e2e \
ENVY_E2E_TEST_RUN='TestCompositionLifecycle|TestCLIImageUpdatePreservesComposition|TestMultipleOverridesAcrossRESTMCPAndCLI' \
make test-e2e
```

The GitHub provider tests use a local test server; a live installation and private
registry smoke test require the credentials described above.
