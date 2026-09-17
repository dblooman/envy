# GitHub App and labelled PR previews

Add `envy-preview` to an open PR to request a dedicated backend environment.
Opening an unlabelled PR does not provision compute. Existing UI, CLI and agent
compositions remain independent, including compositions using the same builds.

## Operator setup

Create/install the GitHub App on selected repositories. For source browsing only,
Contents read remains sufficient. PR automation additionally requires Pull requests
write (inspection and comments), Actions read (workflow verification), and Deployments
write (deployment feedback). Reapprove changed permissions on existing installations.
Subscribe to Pull request events; GitHub also sends installation and installation
repository-access events. Configure a publicly reachable HTTPS webhook URL ending
in `/webhooks/github`, with a random secret of at least 32 characters.

The webhook endpoint validates the raw-body HMAC before durable ingestion. It does
not accept the Envy operator token in place of the signature. Configure HTTPS and
network reachability through the installation's existing ingress; the chart does
not expose a public service automatically. CI runners also need access to the build
report endpoint. A loopback-only installation cannot receive hosted GitHub webhooks.
See GitHub's [signature validation guidance](https://docs.github.com/en/webhooks/using-webhooks/validating-webhook-deliveries).

Supply existing Kubernetes Secrets through Helm:

```yaml
github:
  appID: "123456"
  privateKeySecret: {name: envy-github-app, key: private-key.pem}
  webhookSecret: {name: envy-github-webhook, key: webhook-secret}
  buildCredentialsSecret: {name: envy-build-access, key: build-credentials.json}
```

The equivalent server variables are `ENVY_GITHUB_APP_ID`,
`ENVY_GITHUB_APP_PRIVATE_KEY_FILE`, `ENVY_GITHUB_WEBHOOK_SECRET_FILE`, and
`ENVY_BUILD_CREDENTIALS_FILE`. JSON server configuration accepts
`github.webhook_secret_file` alongside the existing App settings. Keys and tokens
remain server-only; restart the server after secret rotation.

In Catalog → Sources, use **Discover installed repositories**, select a repository,
and register its approved component/image mappings. Catalog → GitHub shows App
configuration, webhook/reconciliation health, preview policies and preview controls.
Create an enabled policy with a baseline, one to three mapped components, TTL and
the numeric trusted Actions workflow ID. Envy validates App permissions and workflow
access. Only one enabled policy can own a GitHub repository within an installation.

## CI contract

Use [the labelled PR workflow example](../integrations/github-actions/label-preview-example.yml)
with your existing image build/push steps. Check out `pull_request.head.sha` explicitly;
the default PR checkout may be a synthetic merge commit, which is not eligible.
Publish all policy components in the same Actions run/attempt and report each digest
using the existing scoped build token. Do not provide the full Envy API credential.

Envy waits for GitHub to report successful workflow completion, verifies the exact
run attempt and head SHA, then selects a complete component set. A successful image
report before the workflow completes is expected: periodic reconciliation checks
completion. For multiple reports, a partial newer successful run waits for its
missing components rather than mixing runs. Failed builds do not replace an existing
environment. The token establishes trusted CI reporting; this is not artifact
attestation verification. Keep reporting credentials away from untrusted code.

Draft PRs can opt in with the label. Fork PRs cannot use automatic previews.
Merge-commit builds, workflow dispatch, frontend hosting, OIDC token exchange and
automatic rollout rollback are outside this release.

## Lifecycle and controls

Label addition requests the current head. Subsequent matching builds update the
same composition URL without extending its TTL. Requested and deployed revisions
are reported separately; readiness means infrastructure readiness, not passing
application tests. A rollout failure remains visible and does not promise that the
previous revision is healthy.

Label removal, PR close/merge, policy disable and App-access removal stop updates
and destroy only the owned composition. Expiry and explicit stop are durable: a
still-present label or late build does not revive compute. Reopening a labelled PR,
an observed remove/add label cycle, or explicit restart requests a new lifecycle.
Restart requires an open labelled same-repository PR and an enabled policy, waits
for old resources to be destroyed, and allocates a new composition URL and TTL.
Policy baseline/component/TTL changes apply to new lifecycles.

```sh
envy pr-preview list --project shop
envy pr-preview get PREVIEW_ID
envy pr-preview stop PREVIEW_ID
envy pr-preview restart PREVIEW_ID
envy pr-preview policy --file preview-policy.json
```

Policy JSON contains `project`, `repository`, `enabled`, `baseline`, `components`,
`ttl`, and `workflow_id`. MCP exposes `list_pr_previews`, `get_pr_preview`,
`stop_pr_preview` and `restart_pr_preview`. Generic composition updates reject
controller-owned previews. Generic composition deletion also records an explicit
stop. Create a separate composition to experiment with independent build selections.

## Operations and acceptance

PostgreSQL stores webhook receipts, policy, preview ownership, lifecycle history and
pending feedback state. Delivery IDs deduplicate retries. The controller runs under
Envy's existing reconciliation leadership, checks pending work every 15 seconds and
performs discovery/recovery scans every five minutes and on webhook/build wakeups.
GitHub does not automatically retry failed deliveries; see its
[redelivery documentation](https://docs.github.com/en/webhooks/testing-and-troubleshooting-webhooks/redelivering-webhooks).
GitHub rate-limit responses apply provider backoff; outages do not imply revocation.
CAS versions fence concurrent stop/restart requests; composition creation commits
ownership in the same database transaction.

One App comment carries URL, status, revisions, expiry and restart instructions.
GitHub deployment success is published only for an observed ready generation.
Feedback failure is retried without preventing Envy cleanup. Inspect
`GET /v1/github/status`, preview `reason`/`feedback_error`, and Activity for diagnosis.

Roll out with policies disabled, then enable one dedicated test repository. Verify:
unlabelled PR allocates nothing; label and successful build create a preview; a new
head updates the same URL; close removes resources; late reports cannot revive it;
expiry requires restart; unrelated environments survive. Use an operator-provisioned
HTTPS endpoint and disposable deployment for this live acceptance test.
