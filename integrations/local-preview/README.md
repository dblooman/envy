# Local Actions-to-preview workflow

This example joins existing GitHub CI, Envy and the small shop frontend. It
downloads one successful Actions run's `envy-build-report` artifact, validates
the run/attempt/commit/component, imports it with a scoped reporting token,
creates or updates a composition, binds the clean frontend checkout's exact SHA,
builds it with the resolved API URL, and serves `dist` on loopback.

Prerequisites: Python 3.9+, `gh` authenticated to the private repository, Git,
Node/npm, a configured local Envy API, registered catalog/source mappings,
registry access and the two token files. The frontend must implement
`npm run build`, consume `VITE_ENVY_API_URL` and output `dist`; this example uses
the shop `/products` endpoint and its allowed origin `http://localhost:4174`.

Copy `config.example.json` to `.envy/github-setup/workflow.json` after completing
the local GitHub setup. Adjust repository/project names for another setup.
File paths resolve relative to the config file. Keep config, credentials and
state under ignored `.envy`, and never put token values in the config.

```sh
python3 integrations/local-preview/preview.py \
  --config .envy/github-setup/workflow.json --run ACTIONS_RUN_ID
```

The command remains in the foreground to serve the frontend. It prints a JSON
result containing the composition, generation, build, URLs and expiry. Browser
validation is still a separate caller check; the helper does not claim a passing
browser test merely because the local server started.

Ctrl-C stops the local server and retains the preview. Run the same command with
a newer successful run ID to update, or an older run ID to roll back. Composition
identity and URL remain stable, unrelated overrides are retained, and updates
use the observed desired generation. A conflict fails for inspection/retry.
Replaying the same run skips an unnecessary update. TTL is not extended.

```sh
# Build without launching/publishing the frontend (useful for scripted checks).
python3 integrations/local-preview/preview.py \
  --config .envy/github-setup/workflow.json --run ACTIONS_RUN_ID --no-serve

# Stop the foreground server first, then remove this workflow's composition.
python3 integrations/local-preview/preview.py \
  --config .envy/github-setup/workflow.json --destroy
```

Cleanup waits for Envy's `destroyed` phase. State and evidence remain for
inspection. Use a new `state_dir` to start a fresh preview after destruction.
Frontend names include the composition ID because bindings cannot be reassigned
to another composition. The shared baseline and external GitHub artifacts remain.

State is written immediately after accepted creation and includes a persisted
idempotency key for retrying a crash during creation. Keep this directory until
cleanup. Failures retain the composition for inspection instead of deleting it
implicitly. Waiting is bounded (120 seconds by default, `--timeout` up to 600).
A local lock prevents overlapping workflow invocations, including while serving.

This is an operator-driven local workflow. It does not dispatch CI, expose the
API publicly, run an agent, or install a webhook. A remote automated PR workflow
still needs an explicit connectivity and runner design.

```sh
python3 -m unittest discover -s integrations/local-preview -p 'test_*.py'
```

See [live validation results](VALIDATION.md) for the exercised create, replay,
rollback, lost-acknowledgement recovery, browser and cleanup path.
