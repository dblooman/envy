---
title: GitHub Actions
description: Report published images and run disposable or retained Envy previews from your existing pipeline.
---

## Choose who manages the environment

For a PR environment that automatically follows new builds at the same URL, use
the [GitHub App & PR Previews setup guide](/integrations/github-app/). Adding
`envy-preview` opts in; Envy owns creation, updates and cleanup. CI only publishes
and reports exact PR-head builds.

The disposable and retained workflows below are separate CI-managed options.
Do not use their composition lifecycle steps to manage an App-owned preview.

## Report published images

Envy does not build images or dispatch Actions workflows. Add this step after
an existing successful image push. Copy [`report-build.py`](https://github.com/dblooman/envy/blob/main/integrations/github-actions/report-build.py) into your application
repository at `.github/scripts/report-build.py` (or use `delivery source report`).
The runner must reach Envy's API. Configure `ENVY_BUILD_TOKEN` as a repository
secret containing its separately scoped CI reporting token, never the admin token.

This example assumes the existing build step is named `build` and exports a
registry manifest/index `digest`, as docker/build-push-action does. Keep the
image repository equal to the component's registered location. Report the
**checked-out commit**, which can differ from the PR head when CI builds a merge
commit. Do not label a merge build with its PR head SHA.

```yaml
# Append after your existing checkout, build, and push steps.
- name: Record the published artifact
  env:
    ENVY_API_URL: ${{ vars.ENVY_API_URL }}
    ENVY_PROJECT: shop
    ENVY_REPOSITORY: backend
    ENVY_BUILD_TOKEN: ${{ secrets.ENVY_BUILD_TOKEN }}
    BUILD_DIGEST: ${{ steps.build.outputs.digest }}
    IMAGE_REPOSITORY: us-docker.pkg.dev/example/previews/pricing
    COMPONENT: pricing
  run: |
    python3 - <<'PY'
    import datetime, json, os, pathlib, subprocess
    revision = subprocess.check_output(["git", "rev-parse", "HEAD"], text=True).strip()
    report = {
        "component": os.environ["COMPONENT"],
        "revision": revision,
        "image": os.environ["IMAGE_REPOSITORY"] + "@" + os.environ["BUILD_DIGEST"],
        "run_id": os.environ["GITHUB_RUN_ID"],
        "attempt": int(os.environ["GITHUB_RUN_ATTEMPT"]),
        "built_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
    }
    pathlib.Path("envy-build-report.json").write_text(json.dumps(report))
    PY
    python3 .github/scripts/report-build.py --file envy-build-report.json
```

Preserve `envy-build-report.json` for retries; identical reports return the same
build ID. Changing a report within the same run attempt/component returns 409.
A new Actions attempt produces a separate record, even at the same Git commit.
The timestamp above records completion of the publish step.

For a monorepo, report each built component separately and authorize each in its
CI token scope. A single component can report one image per run attempt. Choose
an OCI multi-platform index digest when the build publishes multiple platforms.

If the reporting step fails, the image may still exist in the registry. Retry
reporting the same file rather than silently selecting an earlier build. Envy
checks repository access, commit existence, approved image location, and registry
availability before accepting the report. The scoped CI credential establishes
who may report; this is CI-reported provenance, not an image attestation verifier.

With an installed delivery CLI, the equivalent is:

```sh
ENVY_API_TOKEN="$ENVY_BUILD_TOKEN" delivery source report \
  --project shop --repository backend --file envy-build-report.json
```

Unset `ENVY_API_TOKEN_FILE` when using this CLI example; that existing CLI option
takes precedence over an environment token. Never make the reporting token
available to untrusted fork code; configure your existing CI trust boundary.

## Run tests against a preview

For an Argo-managed baseline, first [discover and approve the deployed service configuration](/integrations/argo-cd/). The existing pipeline continues deploying main; the candidate image runs in a separate Envy composition.

The repository includes an optional [reusable preview workflow](https://github.com/dblooman/envy/blob/main/.github/workflows/envy-preview.yml) and two caller templates:

- [Disposable tests](https://github.com/dblooman/envy/blob/main/integrations/github-actions/caller-example.yml): adapt the build and baseline-health steps, create a preview, run application tests, and destroy it.
- [Retained previews](https://github.com/dblooman/envy/blob/main/integrations/github-actions/retained-example.yml): an explicit manual request creates or updates a composition with cleanup disabled and a bounded TTL.

The reusable workflow runs only when called. It consumes image/build selections, waits for the accepted desired and observed generation, exposes the preview URL, and preserves state for cleanup. It does not build images or render Helm charts. Pin the workflow and helper to the same reviewed commit.

Preview creation requires a full Envy API credential, separate from the scoped build-reporting token above. Your runner must reach Envy and the preview endpoint. Adapt the templates to your CI trust boundary and application tests; hosted GitHub Actions execution of these examples has not yet been validated.

Image updates retain captured configuration. Recreate a preview to adopt newer configuration, and coordinate updates to retained previews for the whole test duration. See the [workflow inputs and retry contract](https://github.com/dblooman/envy/blob/main/integrations/github-actions/README.md).
