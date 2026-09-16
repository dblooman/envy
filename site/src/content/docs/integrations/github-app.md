---
title: GitHub App & PR Previews
description: Configure GitHub App permissions, HTTPS webhooks, scoped CI credentials and automatically updated labelled PR environments.
---

Add `envy-preview` to an open pull request to request a backend preview. Envy
waits for published builds of the PR's exact head commit, then creates an
environment. Subsequent builds update the same environment URL. Opening a PR
without the label creates nothing.

This integration is optional and disabled until you enable a repository policy.
Environments created independently through the UI, CLI or agents keep their
existing lifecycle, even when they use the same repository or builds.

## Before you start

- A running Envy installation with PostgreSQL and a working baseline. Complete
  [application onboarding](/getting-started/onboarding/) first.
- Permission to create a GitHub App and install it on your selected repository.
- A publicly reachable HTTPS endpoint for webhooks. Hosted Actions runners must
  also reach Envy's build-report endpoint.
- CI that publishes images to a registry Envy and its Kubernetes nodes can pull
  from. GitHub App access does not grant private-registry pull access.

The first release supports same-repository PRs and one to three backend component
overrides. Explicitly labelled drafts are eligible. Fork PRs are unsupported and
allocate no resources. Frontend hosting and combined multi-repository environments
continue through the existing UI and agent workflows.

## 1. Create and configure the GitHub App

In GitHub, open your user or organization **Settings → Developer settings →
GitHub Apps**, then create or edit an App. App creation and private-key management
remain operator responsibilities; Envy discovers installations after configuration.

### Repository permissions

| Permission    | Access                | Why Envy needs it                                                       |
| ------------- | --------------------- | ----------------------------------------------------------------------- |
| Contents      | Read-only             | Inspect source revisions and repository contents.                       |
| Metadata      | Read-only (mandatory) | Identify repositories and installation access.                          |
| Pull requests | Read and write        | Inspect PR state and maintain an App-authored preview comment.          |
| Actions       | Read-only             | Verify the trusted workflow, successful run/attempt and exact head SHA. |
| Deployments   | Read and write        | Publish deployment records and readiness/inactive statuses.             |

Existing source-only integrations can continue with Contents read access without
the additional preview permissions. Envy checks capabilities before enabling previews.
These are **App repository permissions**, not the workflow's `GITHUB_TOKEN`
permissions. The example workflow separately uses Contents read and Packages write
to check out code and publish to GHCR.

### Webhook URL and events

1. Under the App's **General** settings, activate webhooks.
2. Set **Webhook URL** to your externally reachable Envy API origin followed by
   `/webhooks/github`, for example `https://envy-api.example.com/webhooks/github`.
   This is not the GitHub repository URL, a frontend URL or a composition URL.
3. Set a randomly generated webhook secret of at least 32 characters. Store the
   same value in Envy's server configuration; keep SSL verification enabled.
4. Under **Permissions & events**, grant the permissions above, then find
   **Subscribe to events** and select **Pull request**. Save the changes.
5. Install the App on the repositories you want Envy to access. For an existing
   installation, have its owner approve the updated permission request.

**Can't see “Pull request”?** Enable webhooks first and grant the App's **Pull
requests** repository permission. Event availability depends on App permissions;
changing filesystem permissions on the downloaded PEM file does not affect it.
See GitHub's [App settings guide](https://docs.github.com/en/apps/maintaining-github-apps/modifying-a-github-app-registration).

GitHub also sends installation and repository-access changes. Envy uses these to
stop owned previews when access is removed. No Issues event subscription is needed
for the PR label trigger.

Download a private key from the App's General page and note its **App ID**. Envy's
configuration uses the App ID; you do not need the OAuth client secret. Keep the
PEM server-side, out of source control, browser configuration and Actions secrets.
Restrict the downloaded file, for example with `chmod 600 /path/to/private-key.pem`.

## 2. Configure Envy's credentials

Keep these credentials separate:

| Credential                          | Where it belongs                                                               |
| ----------------------------------- | ------------------------------------------------------------------------------ |
| App private key                     | Envy server only; used to obtain scoped installation tokens.                   |
| Webhook secret                      | GitHub App settings and Envy server; validates incoming signatures.            |
| Scoped build-report token           | Envy server and an Actions secret; authorizes reports for explicit components. |
| Registry pull credential, if needed | Envy registry configuration and Kubernetes nodes.                              |

For Helm installations, provision Kubernetes Secrets in the release namespace and
reference them in your existing values. The names below are examples; the chart
does not create the secret contents or expose a public HTTPS endpoint for you.

```yaml
github:
  appID: "123456"
  privateKeySecret:
    name: envy-github-app
    key: private-key.pem
  webhookSecret:
    name: envy-github-webhook
    key: webhook-secret
  buildCredentialsSecret:
    name: envy-build-access
    key: build-credentials.json
```

Use secret files containing only the intended values, without accidental trailing
newlines in tokens. Apply these alongside your existing installation values.
Restart Envy after rotating mounted credentials.

For a server configured through environment variables, use:

```sh
ENVY_GITHUB_APP_ID=123456
ENVY_GITHUB_APP_PRIVATE_KEY_FILE=/run/secrets/private-key.pem
ENVY_GITHUB_WEBHOOK_SECRET_FILE=/run/secrets/webhook-secret
ENVY_BUILD_CREDENTIALS_FILE=/run/secrets/build-credentials.json
```

The build credentials file contains a JSON array of narrowly scoped tokens:

```json
[
  {
    "token": "REPLACE_WITH_A_UNIQUE_RANDOM_TOKEN_AT_LEAST_32_CHARACTERS",
    "project": "shop",
    "repository": "backend",
    "components": ["pricing"]
  }
]
```

Use the same token as the repository's `ENVY_BUILD_TOKEN` Actions secret. Never
substitute Envy's operator/admin token. See the
[source-build configuration reference](https://github.com/dblooman/envy/blob/main/docs/source-builds.md)
for registry access and local Kubernetes credential helpers.

## 3. Discover the repository and enable a policy

In **Catalog → Sources**, choose **Discover installed repositories**, select the
repository and register its approved component-to-image mappings. Discovery removes
the need to type an installation ID manually. Components and the baseline must
already exist in Envy; registration does not deploy a baseline or create Dockerfiles.

In **Catalog → GitHub**, inspect App access and webhook health, then configure an
enabled preview policy with:

- The registered project and source repository.
- A baseline containing all selected components.
- An explicit selection of one to three mapped components.
- A fixed TTL within the installation's existing limits.
- The numeric ID of the trusted Actions workflow that publishes those components.

Find workflow IDs with an authenticated GitHub CLI:

If the build workflow does not exist yet, complete step 4 before enabling the policy.

```sh
gh api repos/OWNER/REPO/actions/workflows \
  --jq '.workflows[] | {id, name, path}'
```

One enabled policy may own a GitHub repository within an Envy installation. The
label is always `envy-preview`; create it in your repository if it does not exist.
Baseline, component and TTL edits apply to subsequent lifecycles, not an already
running preview. Disabling a policy stops and cleans up its owned previews.

The equivalent policy file for `delivery pr-preview policy --file policy.json` is:

```json
{
  "project": "shop",
  "repository": "backend",
  "enabled": true,
  "baseline": "staging",
  "components": ["pricing"],
  "ttl": "30m",
  "workflow_id": 123456789
}
```

## 4. Publish exact-head builds from Actions

Copy and adapt the
[labelled preview workflow](https://github.com/dblooman/envy/blob/main/integrations/github-actions/label-preview-example.yml)
into your application repository. Copy
[`report-build.py`](https://github.com/dblooman/envy/blob/main/integrations/github-actions/report-build.py)
to `.github/scripts/envy-report-build.py` as expected by that example.

Configure these Actions settings:

- Variable `ENVY_API_URL`: the reachable Envy API origin, without `/webhooks/github`.
- Secret `ENVY_BUILD_TOKEN`: the separately scoped reporting token from step 2.
- Variable `ENVY_PREVIEW_COMPONENTS_JSON`: the exact policy component set, with
  each component's approved image, build context and Dockerfile, for example:

```json
[
  {
    "component": "pricing",
    "image": "ghcr.io/example/pricing",
    "context": ".",
    "dockerfile": "Dockerfile"
  }
]
```

Adapt `ENVY_PROJECT`, `ENVY_REPOSITORY` and registry login/push settings in the
workflow. The example uses GHCR. Register the workflow before selecting its numeric
ID in the policy.

Check out the PR head explicitly: the default PR checkout can build a synthetic
merge commit, which is not eligible for this controller. Do not report a merge
build as though it were the head build.

```yaml
# Under your pinned actions/checkout step:
with:
  ref: ${{ github.event.pull_request.head.sha }}
  persist-credentials: false
```

Publish and report every configured component in the same workflow run/attempt.
Envy verifies successful completion of the trusted workflow, its head SHA and the
complete component set before deploying. Reporting before the workflow finishes
is expected; reconciliation checks completion later. Newer eligible runs take
precedence, and reports from different runs are never mixed to fill a partial set.

The workflow only builds and reports; Envy creates, updates and destroys the
composition. Keep secrets away from untrusted code. This is trusted CI reporting,
not artifact-attestation verification. Workflow dispatch, merge-commit builds,
fork approval, OIDC credential exchange and automatic rollback are not supported.
See [GitHub Actions](/integrations/github-actions/) for the report contract and
the separate disposable/retained CI-managed workflows.

## Lifecycle and controls

| Event                                 | Result                                                                                  |
| ------------------------------------- | --------------------------------------------------------------------------------------- |
| Open an unlabelled PR                 | Nothing is allocated.                                                                   |
| Add `envy-preview`                    | Wait for complete, successful exact-head builds, then create.                           |
| Push new commits                      | Update the existing composition when matching builds arrive; retain its URL.            |
| Build fails or a component is missing | Keep the existing environment; show requested and deployed revisions separately.        |
| Remove label, close or merge PR       | Cancel pending work and destroy only the PR-owned composition.                          |
| Reopen a labelled PR                  | Start a new lifecycle, subject to policy and available builds.                          |
| TTL expires                           | Clean up; further builds do not revive it. Updates never renew the TTL.                 |
| Explicit stop                         | Clean up and suppress recreation while the current label cycle remains.                 |
| Explicit restart                      | Revalidate an open labelled PR, finish old cleanup, then create with a new URL and TTL. |
| Revoke App access or disable policy   | Stop automation and clean up owned previews; preserve independent environments.         |

After expiry or explicit stop, use **Restart** or a newly observed remove/add label
cycle to request another lifecycle. An outage is not treated as access revocation:
new deployments wait, while existing TTLs continue.

**Catalog → GitHub** provides preview status, stop and restart controls. Composition
details identify PR ownership, requested/deployed revision, pending reason and
expiry. Generic composition updates cannot override controller-managed selections;
generic deletion also records an explicit stop. Create a separate composition for
independent experiments.

```sh
delivery pr-preview list --project shop
delivery pr-preview get PREVIEW_ID
delivery pr-preview stop PREVIEW_ID
delivery pr-preview restart PREVIEW_ID
```

Agents can use `list_pr_previews`, `get_pr_preview`, `stop_pr_preview` and
`restart_pr_preview` through [MCP](/agents/mcp-server/).

Envy maintains one App-authored PR comment per policy and publishes GitHub
deployment statuses. A deployment becomes successful only when its matching Envy
generation is ready; retired deployments become inactive. Readiness does not mean
application tests passed. A failed rollout does not guarantee the previous revision
is still healthy. GitHub feedback retries independently of resource cleanup.

## Local HTTPS webhook testing

For the full developer walkthrough, including secret mounting, an allowlisted
proxy configuration and lifecycle assertions, see
[Local GitHub Testing](/integrations/github-local-testing/).

GitHub cannot call `localhost` on your machine. Use a temporary HTTPS tunnel to a
local reverse proxy, then put its public origin plus `/webhooks/github` in the App's
Webhook URL box. The composition URL can remain local for developer testing.

Do not expose an unauthenticated development API wholesale. Configure the proxy
to forward **only** `POST /webhooks/github` and, if hosted Actions will report
builds, `POST /v1/projects/PROJECT/repositories/REPOSITORY/builds` for the test
repository. Preserve the raw webhook body, signature headers and build-report
Authorization header; return 404 for other paths. Webhook HMAC and scoped build
authentication must remain enabled.

Once that proxy is listening on loopback port 8087, for example:

```sh
cloudflared tunnel --url http://127.0.0.1:8087
```

Use the generated `https://….trycloudflare.com/webhooks/github` URL for GitHub,
and the same origin without a path as Actions' `ENVY_API_URL`. Keep the tunnel
running. [Quick tunnels](https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/do-more-with-tunnels/trycloudflare/)
are for testing, have no uptime guarantee and use a temporary address. Update both
settings if the address changes; use stable HTTPS ingress for a deployed installation.

## Verify and troubleshoot

In the App's **Advanced → Recent deliveries**, inspect a delivery or redeliver its
ping after configuration. A valid persisted delivery receives `202 Accepted`.
Delivery IDs are deduplicated; GitHub does
[not automatically redeliver failures](https://docs.github.com/en/webhooks/testing-and-troubleshooting-webhooks/redelivering-webhooks).
Signatures follow GitHub's
[raw-body HMAC guidance](https://docs.github.com/en/webhooks/using-webhooks/validating-webhook-deliveries).

Envy checks active work and performs periodic recovery discovery, by default every
five minutes, so missed events or build wakeups can recover. Inspect **Catalog →
GitHub**, `GET /v1/github/status`, preview reasons/feedback errors and Activity for
last webhook receipt, reconciliation progress and lifecycle actions.

| Symptom                                             | Check                                                                                                                                                    |
| --------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------- |
| No installations or repository missing              | App ID/key match; App installed on the correct account; selected-repository access includes this repository.                                             |
| Permissions fail after editing the App              | The installation owner must approve the new permissions, not just save the App registration.                                                             |
| Webhook 401                                         | GitHub and Envy secrets match exactly; proxy preserves body and signature. A bearer token is not a webhook signature.                                    |
| Webhook 404/405 or connection failure               | Public HTTPS URL ends in `/webhooks/github`; tunnel/proxy is running and permits POST. Opening the URL in a browser sends GET, not a webhook.            |
| Waiting for builds                                  | Correct workflow ID; successful run/attempt; exact PR head checkout; all configured components reported together; Actions can reach the report endpoint. |
| Composition fails readiness                         | Inspect composition diagnostics, baseline health, image pull access and ingress/TLS. GitHub permissions do not fix a runtime failure.                    |
| No recreation after a new build                     | Preview may be stopped or expired; explicitly restart an open labelled PR.                                                                               |
| Preview works but comment/deployment feedback fails | Inspect feedback errors and Pull requests/Deployments write permissions; feedback is retried separately.                                                 |

Start with one dedicated test repository and a disposable deployment. Verify:
unlabelled PR → no environment; label → successful build → ready; push → update
at the same URL; close → confirmed cleanup. Also verify expiry does not revive
on a late report and that an independently created environment is untouched.
