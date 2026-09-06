# Delivery CLI

Build with `make build`, or let `make dev` build it while starting the local
environment. The executable is `.envy/bin/delivery`. It needs API credentials,
not Kubernetes credentials.

```sh
export ENVY_API_URL=http://127.0.0.1:8081
export ENVY_API_TOKEN_FILE="$PWD/.envy/envy-dev/api-token"

.envy/bin/delivery composition create \
  --project demo --baseline staging --name service-b-test \
  --image envy/service-b:v2 --ttl 8h --idempotency-key my-first-composition
```

Use the returned `id` in the following commands. Flags follow the ID. Successful
requests emit exactly one JSON value to stdout, matching the REST response.
Create, update, and destroy accept asynchronous work; use `wait` or `get` to
inspect progress.

```sh
.envy/bin/delivery composition get <id>
.envy/bin/delivery composition inspect <id>   # Alias for get
.envy/bin/delivery composition wait <id> --timeout 60s
.envy/bin/delivery composition endpoints <id>
.envy/bin/delivery composition list --project demo --limit 20
```

Open `endpoints.public.url` once ready. Baseline remains at
`http://baseline.envy.localhost:8080`. `make dev` loads genuine v2 and v3 service-b
images. To update, pass the current desired `generation` from `get`:

```sh
.envy/bin/delivery composition update <id> \
  --expected-generation 1 --image envy/service-b:v3
.envy/bin/delivery composition wait <id> --timeout 60s
.envy/bin/delivery composition destroy <id>
.envy/bin/delivery composition wait <id> --timeout 60s
```

The URL and expiry stay the same. Updates require a ready or failed composition.
A stale generation returns a structured conflict and exit 1. Read the composition
before deciding whether to retry; the CLI never automatically replaces your
expected generation. A failed rolling update may leave the previous override
serving while the new image is unavailable. See [update semantics](updates.md).

`wait` defaults to 30 seconds and allows at most 60 seconds per call. Its stdout
always contains the latest composition when one was obtained successfully. Exit
0 means ready or destroyed; exit 1 means a failed composition or command error;
exit 2 means the wait timed out; exit 130 means cancellation. Command errors use
the REST-style `{"error":{"code":...,"message":...,"retryable":...}}` envelope
on stderr. Cancelling never destroys a composition. `get` exits 0 even when the
retrieved composition reports a failure, because the inspection succeeded.

Pass `--after <next_cursor>` to continue a list. List pages are bounded to 100.
Each command supports `--api-url`, `--token-file`, and `--help`; help is JSON.
`ENVY_API_TOKEN` is supported when no token file is configured. A token file
takes precedence over the token environment variable. No credentials are stored
by the CLI, and HTTP redirects are refused.

## Diagnostics

```sh
.envy/bin/delivery composition logs <id> --component service-b --tail-lines 100 --max-bytes 32768
.envy/bin/delivery composition logs <id> --component gateway --since 1h
.envy/bin/delivery composition logs <id> --component service-b --previous
.envy/bin/delivery composition events <id> --limit 20
.envy/bin/delivery composition events <id> --limit 20 --after <next_cursor>
```

Log output remains JSON with pod identities and explicit `override` or
`shared-baseline` source labels. Inherited logs are not composition-filtered.
Inspect `partial`, `truncated`, and per-pod `error` fields; a successfully retrieved
partial snapshot exits 0. `--since` requires whole seconds and allows at most
24 hours. Event pages persist after destruction; pod logs do not. The same
[diagnostic limits](diagnostics.md) apply to REST, CLI, MCP, and the web frontend.
