# GitHub Actions and Envy

For retained, automatically updated PR environments requested with `envy-preview`,
use the [label workflow example](label-preview-example.yml) and configure the
[GitHub App preview controller](../../docs/github-pr-previews.md). The disposable
CI test workflow below remains separate: it creates an environment for a test job
and removes it afterwards.

Onboard the deployed application once using [deployment-derived profiles](../../docs/deployment-derived-previews.md).
Then the existing build pipeline supplies immutable images or published build IDs.
CI does not render charts or send Secret payloads to Envy.

- [Disposable test caller](caller-example.yml): adapt the build and live baseline-health gate to your pipeline; cleanup runs after tests.
- [Explicit retained preview](retained-example.yml): a trusted manual request creates or updates a known composition with a bounded TTL.

The workflow checks the accepted desired and observed generation before returning
an endpoint. Coordinate retained-preview writers for the whole test duration;
readiness is not a lease preventing another actor from updating the URL. Image
updates preserve captured configuration; recreate to adopt current configuration.
Hidden state artifacts are included explicitly for diagnostics and cleanup recovery.

# Report published images from GitHub Actions

Envy does not build images or dispatch Actions workflows. Add this step after
an existing successful image push. Copy `report-build.py` into your application
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

## Automated composition previews

This directory also contains the composition helper (`preview.py`), a reusable
workflow (`../../.github/workflows/envy-preview.yml`), and a caller template
(`caller-example.yml`). The workflow is deliberately separate from image
building: callers build and push images with their existing trusted job, report
them with `report-build.py`, and pass the returned immutable build IDs to this
workflow. It does not dispatch Actions or execute a Helm chart.

### REST helper contract

Use Python 3.9 or newer and provide the full Envy API credential in
`ENVY_API_TOKEN` (never `ENVY_BUILD_TOKEN`). The API URL may also be passed with
`--api-url`; otherwise `ENVY_API_URL` is used. URLs must be HTTP(S), without
credentials, query strings, or fragments. The helper sends
`X-Envy-Channel: github` and does not follow redirects.

Create a new preview:

```sh
ENVY_API_TOKEN="$ENVY_API_TOKEN" python3 integrations/github-actions/preview.py run \
  --api-url "$ENVY_API_URL" \
  --project shop \
  --baseline staging \
  --name pricing-pr-123 \
  --overrides-file .github/envy-overrides.json \
  --idempotency-key "shop-pr-123-create" \
  --ttl 8h \
  --timeout 300 \
  --state-file .envy-preview-state.json
```

The overrides file (or `--overrides-json`) is a complete object with at most
three entries. Each entry has exactly one of these forms:

```json
{
  "pricing": {"build_id": "64-lowercase-hex-build-id"},
  "gateway": {"image": "registry.example/gateway@sha256:immutable-digest"}
}
```

An empty object is valid and means that every component inherits the baseline.
Build IDs must be the 64-character IDs returned by the build-report endpoint;
images must be non-empty values without whitespace (digest-pinned images are
recommended). The helper rejects unknown override fields and never accepts
server-owned `source` data.

Before a mutation, the helper:

1. lists `/v1/projects` and `/v1/projects/{project}/baselines`, following
   `next_cursor` pages, and requires the exact project and baseline IDs;
2. obtains the registered baseline `revision`;
3. optionally reads `--catalog-manifest-file`, requires its project and
   baseline IDs to match the selected target, and posts it to
   `/v1/catalog/validate` (validation never applies or changes the catalog);
4. posts `/v1/compositions` with the discovered revision as
   `expected_baseline_revision`, the complete override map, TTL, and the
   `Idempotency-Key` header.

`--expected-baseline-revision` is optional. When supplied, it must equal the
discovered revision; a mismatch stops before a composition write. The
idempotency key is required for every `run` invocation. Keep it stable when
retrying a request after a lost response; changing the request with the same
key is a conflict. The API response's composition ID is written to
`--state-file` immediately after acceptance (mode `0600`) so cleanup can still
run if readiness later fails.

Update a retained composition by sending the complete desired map and an
optimistic generation:

```sh
ENVY_API_TOKEN="$ENVY_API_TOKEN" python3 integrations/github-actions/preview.py run \
  --api-url "$ENVY_API_URL" --project shop --baseline staging \
  --mode update --composition-id COMPOSITION_ID --expected-generation 3 \
  --overrides-json '{"pricing":{"build_id":"64-lowercase-hex-build-id"}}' \
  --idempotency-key "shop-pr-123-update-4" --timeout 300
```

Update mode first reads the composition, confirms its project, baseline, and
pinned baseline revision still match discovery, then sends
`PATCH /v1/compositions/{id}` with `expected_generation` and the complete
override map. A stale generation or baseline revision is a visible conflict;
the helper never silently retries against a newer generation.

For either mode, readiness polling uses
`GET /v1/compositions/{id}/status` until `phase=ready`, then reads
`GET /v1/compositions/{id}/endpoints`. Successful JSON output has this stable
top-level shape:

```json
{
  "composition_id": "…",
  "generation": 3,
  "baseline_revision": "…",
  "preview_url": "https://cmp-….example",
  "endpoint_ready": true,
  "composition": {},
  "status": {},
  "endpoints": {}
}
```

`--timeout` is a required bounded lifecycle budget when non-default behavior is
needed: 1–600 seconds (default 120). Each HTTP request is separately bounded
by `--request-timeout` (1–120 seconds, default 30). A failed composition is
not reported as ready. Exit codes are `2` for local input/validation errors,
`3` for API or malformed-response errors, `4` for a terminal lifecycle
failure, and `5` for a readiness timeout. Error messages contain only the
operation and HTTP status where useful; response bodies, bearer tokens,
registry credentials, and upstream error text are never printed.

Destroy explicitly and wait for the retained tombstone:

```sh
ENVY_API_TOKEN="$ENVY_API_TOKEN" python3 integrations/github-actions/preview.py destroy \
  --api-url "$ENVY_API_URL" --composition-id COMPOSITION_ID --timeout 300
```

Destroy is the only cleanup operation and is never triggered by cancellation
of `run`. It calls `DELETE /v1/compositions/{id}`, then polls status until
`phase=destroyed`. Cleanup failures and cleanup timeouts return exit code `6`
and are not hidden. If a runner disappears, the Envy TTL remains the crash
safety bound.

### Reusable workflow contract

The workflow is a `workflow_call` entry point. Copy `preview.py` into the
caller repository at `integrations/github-actions/preview.py`, or set
`helper_repository` and `helper_ref` to a trusted, pinned helper repository.
When using a private helper repository, provide the optional
`envy_helper_token` secret. Pin both the reusable workflow reference and
`helper_ref` to a reviewed tag or commit in production.

Required `with` inputs are:

| Input | Type | Default | Contract |
| --- | --- | --- | --- |
| `envy_api_url` | string | — | HTTP(S) Envy API URL without credentials/query/fragment |
| `project` | string | — | Exact registered project ID |
| `baseline` | string | — | Exact registered baseline ID |
| `composition_name` | string | — | Name for create mode |

Optional `with` inputs are `overrides_json` (`{}`), `catalog_manifest_file` (empty
path), `ttl` (`8h`), `timeout_seconds` (`120`, maximum `600`),
`request_timeout_seconds` (`30`, maximum `120`), `mode` (`create` or `update`),
`composition_id`, `expected_generation`, `expected_baseline_revision`,
`idempotency_key`, `test_command`, `cleanup` (`true`), `helper_path`,
`helper_repository`, and `helper_ref` (`main`). Update mode requires both
`composition_id` and a positive `expected_generation`. If no idempotency key is
provided, the workflow derives one from the GitHub run ID and attempt; callers
should provide a stable explicit key when a retry must replay an earlier
request.

`envy_api_token` is required and is the full API bearer credential. The
`envy_build_token` used by a caller's separate `report-build.py` step is not
passed to this reusable workflow; the composition helper never needs it. The
optional `envy_helper_token` is read only by `actions/checkout` for a private
helper repository. No token is written to outputs, summaries, artifacts, or
application environments.

The reusable workflow outputs `composition_id`, `generation`, `preview_url`,
`endpoint_ready`, `baseline_revision`, and `cleanup_result`
(`destroyed`, `skipped`, or `failed`). A successful lifecycle exposes these
variables to the caller's `test_command`:
`ENVY_PREVIEW_URL`, `ENVY_COMPOSITION_ID`,
`ENVY_COMPOSITION_GENERATION`, and `ENVY_BASELINE_REVISION`. Tests are
caller-owned and may fail without preventing the `always()` cleanup job.
`cleanup: false` intentionally retains the preview and uploads
`.envy-preview-state.json` as
`envy-preview-state-<run_id>-<run_attempt>` for diagnosis. The workflow also
preserves that short-lived state artifact when cleanup is enabled so the
cleanup job can recover an accepted composition ID after a readiness or test
failure. A test failure or helper failure otherwise leaves the cleanup job to
destroy the accepted composition; a runner crash is bounded by the server TTL.

The caller must place its existing build/report job and an explicit Argo or
other baseline-health gate in `needs` before invoking this workflow. Do not
pass mutable tags, secrets, or untrusted fork-controlled values as overrides.
Never expose the full API token to fork code: pull-request workflows from
untrusted forks must be blocked or use a separate trusted dispatch. The
baseline remains owned by Argo/Helm; this integration owns only the
composition lifecycle through REST and does not create CRDs, modify the
baseline Gateway, or ask Argo to prune `envy-*` resources.

The focused tests use a local fake HTTP server and can be run with:

```sh
python3 -m unittest discover -s integrations/github-actions -p 'test_*.py'
```
