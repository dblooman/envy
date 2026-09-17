# Envy CLI

For a released installation, download the matching archive from [GitHub Releases](https://github.com/dblooman/envy/releases), verify it against `SHA256SUMS`, and place `envy` on your `PATH`.

```sh
curl -LO https://github.com/dblooman/envy/releases/download/vX.Y.Z/envy_linux_amd64.tar.gz
curl -LO https://github.com/dblooman/envy/releases/download/vX.Y.Z/SHA256SUMS
sha256sum --ignore-missing -c SHA256SUMS
tar -xzf envy_linux_amd64.tar.gz
install envy_linux_amd64/envy "$HOME/.local/bin/envy"
envy version
```

Choose the archive matching your operating system and architecture (`darwin` or `linux` amd64/arm64, or `windows` amd64 ZIP). Source contributors can instead run `make build`, or let `make dev` build `.envy/bin/envy` while starting the local environment. Deployed installations use browser login or machine credentials; local dev mode needs no credentials.

```sh
export ENVY_API_URL=http://127.0.0.1:8081
unset ENVY_API_TOKEN_FILE ENVY_API_TOKEN # Local dev mode

.envy/bin/envy composition create \
  --project demo --baseline staging --name service-b-test \
  --image envy/service-b:v2 --ttl 8h --idempotency-key my-first-composition
```

For multiple overrides, repeat `--override component=image` (maximum three):

```sh
.envy/bin/envy composition create --name checkout-test \
  --override service-a=envy/service-a:v2 \
  --override service-b=envy/service-b:v2
.envy/bin/envy composition update <id> --expected-generation 1 \
  --override service-a=envy/service-a:v2 \
  --override service-b=envy/service-b:v3
```

Updates require all existing component keys, including unchanged images. These
flags cannot be combined with `--image` or `--component`; the latter remain
available for a single override. Adding/removing components requires a new
composition. The seeded demo approves all three services and includes v2 images
for each, plus service-b v3.

Use the returned `id` in the following commands. Flags follow the ID. Successful
requests emit exactly one JSON value to stdout, matching the REST response.
Create, update, and destroy accept asynchronous work; use `wait` or `get` to
inspect progress.

```sh
.envy/bin/envy composition get <id>
.envy/bin/envy composition inspect <id>   # Alias for get
.envy/bin/envy composition wait <id> --timeout 60s
.envy/bin/envy composition endpoints <id>
.envy/bin/envy composition list --project demo --limit 20
```

Open `endpoints.public.url` once ready. Baseline remains at
`http://baseline.envy.localhost:8080`. `make dev` loads genuine v2 and v3 service-b
images. To update, pass the current desired `generation` from `get`:

```sh
.envy/bin/envy composition update <id> \
  --expected-generation 1 --image envy/service-b:v3
.envy/bin/envy composition wait <id> --timeout 60s
.envy/bin/envy composition destroy <id>
.envy/bin/envy composition wait <id> --timeout 60s
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
takes precedence over the token environment variable; explicit credentials override saved login. HTTP redirects are refused for API requests.

## Browser login

For password or Google installations, run `envy auth login --api-url https://envy.example.com`. The browser opens for login and approval; a loopback callback completes the exchange. `envy auth status` reports the effective identity, and `envy auth logout` revokes the saved agent grant. Ordinary commands never launch a browser. Login prompts go to stderr and command results remain JSON.

The CLI and local MCP adapter share owner-only credentials in the OS user-config directory under `envy/credentials`, keyed by installation origin. Access tokens refresh automatically under a cross-process lock. See [Authentication](authentication-and-activity.md) for session lifetimes and server configuration.

## Diagnostics

```sh
.envy/bin/envy composition logs <id> --component service-b --tail-lines 100 --max-bytes 32768
.envy/bin/envy composition logs <id> --component gateway --since 1h
.envy/bin/envy composition logs <id> --component service-b --previous
.envy/bin/envy composition events <id> --limit 20
.envy/bin/envy composition events <id> --limit 20 --after <next_cursor>
```

Log output remains JSON with pod identities and explicit `override` or
`shared-baseline` source labels. Inherited logs are not composition-filtered.
Inspect `partial`, `truncated`, and per-pod `error` fields; a successfully retrieved
partial snapshot exits 0. `--since` requires whole seconds and allows at most
24 hours. Event pages persist after destruction; pod logs do not. The same
[diagnostic limits](diagnostics.md) apply to REST, CLI, MCP, and the web frontend.

For registered applications, pass `--project`, `--baseline`, and `--component`
to `composition create`. Image updates also accept `--component`; it must match
the existing override. The component flag defaults to `service-b` for demo
compatibility. Discovery and registration are available through REST, and
catalog discovery is also exposed through MCP.

## Register an application configuration

```sh
envy catalog validate --file application.json
envy catalog apply --file application.json
```

Files must contain one `envy/v1` JSON configuration of at most 64 KiB, with no
unknown fields. Both commands use authenticated REST and return JSON. Validate
checks catalog compatibility and live connectivity without writes. Apply repeats
the checks and commits the project, profiles, baseline and host claims atomically.
Identical repeats succeed; changed immutable entries conflict. See the
[shop example](../examples/shop/README.md) and [verification levels](onboarding.md).

## Frontend revisions

`envy frontend bind|get|resolve|publish|check|list` provides JSON-only access
to [frontend bindings](frontend-bindings.md). Bind/get/resolve/publish/check take
`--project`, `--frontend` and a full lowercase Git `--revision`. Bind adds
`--composition` and `--repository`; list takes `--composition`, `--after` and
`--limit`. Resolve defaults to a 60-second wait, accepts up to `5m`, and supports
`--timeout 0` for a single check. Resolution failure is nonzero with a structured
error and no partial API URL. It never selects staging as a fallback.

Publish requires `--expected-version` and `--url`. Check requires
`--expected-version`, `--composition-generation`, `--status passed|failed`, and
`--message`. Both versions refer to the current state returned by `frontend get`;
binding versions are independent of backend desired generations. Browser checks
are caller-reported and become stale when the backend generation changes.
