---
title: Envy CLI Reference
description: Command-line interface guide for the Envy CLI binary.
---

The `envy` CLI provides command-line control of Envy compositions, catalog configurations, frontend bindings, and diagnostics.

```bash
envy [command] [subcommand] [flags]
```

---

## Global Environment & Flags

| Variable              | Description                                                  |
| :-------------------- | :----------------------------------------------------------- |
| `ENVY_API_URL`        | Base URL of the Envy API (default: `http://127.0.0.1:8081`). |
| `ENVY_API_TOKEN_FILE` | Optional machine-token file; overrides saved browser login.  |
| `ENVY_API_TOKEN`      | Bearer token string (used if token file is not provided).    |

Commands return JSON by default; there is no `--json` flag. For a complete list of commands and flags, run `envy --help` or add `--help` after a subcommand. This reference covers common preview workflows.

Follow [Install the CLI](/reference/cli-installation/) for platform archives, checksum verification, PATH setup, and connecting to your server. The CLI is optional for Helm installation and browser use. Run `envy version` to confirm the installed release and commit.

## Browser login

Run `envy auth login` against a password or Google installation. Login opens the browser and saves credentials shared with the local MCP adapter. `envy auth status` reports the effective identity; `envy auth logout` revokes the saved grant. Local dev mode needs no login or token. Ordinary commands never open a browser automatically.

Access tokens last fifteen minutes and refresh for up to thirty days. Credentials are kept in an owner-only file under the OS user-config directory, keyed by server origin. Explicit token/file settings take precedence. Browser instructions go to stderr, preserving JSON stdout.

For scripting (this example requires `jq`):

```bash
envy composition list | jq '.items[].id'
```

---

## `envy composition` Subcommands

### `create`

Creates a new ephemeral composition.

```bash
envy composition create [flags]
```

**Flags**:

- `--name <string>`: Human-readable composition name (**required**).
- `--project <string>`: Catalog project name (default: `demo`).
- `--baseline <string>`: Registered reference baseline (default: `staging` in the local demo).
- `--image <string>`: Shorthand for a single image override.
- `--component <string>`: Component for `--image` (default: `service-b`).
- `--build <component=build_id>`: Select a published build with server-resolved provenance. Repeat or mix with `--override` for distinct components, up to three in total.
- `--inherit-all`: Create a preview URL with no override workloads. Cannot be combined with `--image`, `--component`, `--override`, or `--build`.
- `--message-isolation`: Enable [Google Pub/Sub isolation](/guides/pubsub-isolation/) at creation. Defaults to false, requires operator/application setup, and cannot be changed by update.
- `--expected-preview-revision <component=revision>`: Guard an approved deployment-derived profile revision; see [deployment previews](/integrations/argo-cd/).
- `--override <component=image>`: Key-value override pair. Repeatable up to 3 times:
  `--override orders=repo/orders:v2 --override payments=repo/payments:v2`
- `--ttl <duration>`: Expiration time limit (for example `2h` or `8h`; server default when omitted, maximum `24h`).
- `--idempotency-key <string>`: Optional unique idempotency key for safe retries.

---

### `wait`

Blocks until a composition reaches `ready` phase or times out.

```bash
envy composition wait <composition-id> [--timeout 60s]
```

**Flags**:

- `--timeout <duration>`: Maximum wait duration (default: `30s`, maximum: `60s`).

---

### `update`

Applies the complete desired override set with generation concurrency control. Add, remove, or change components while keeping the same preview URL and original expiry. Rolling updates are not an atomic cutover across services.

```bash
envy composition update <composition-id> \
  --expected-generation <number> \
  --override <component=image>
```

**Flags**:

- `--expected-generation <int>`: (Required) Must match current composition generation. Returns `409 Conflict` if the generation changed.
- `--override <component=image>`: Repeat for every desired image override. Omitted components return to the baseline.
- `--build <component=build_id>`: Select published builds; can mix with image overrides for different components.
- `--image <string>` and `--component <string>`: Single-image shorthand (component defaults to `service-b`).
- `--inherit-all`: Remove all overrides and inherit the whole baseline; incompatible with other override selection flags.
- `--expected-preview-revision <component=revision>`: Guard a deployment-derived profile revision.

Updates accept zero to three overrides. Read the current generation, submit the complete intended selection, then wait for readiness again. A failed rollout can leave the previous override serving; it does not silently switch that component to the baseline. Only ready or failed, unexpired compositions can be updated. App-owned previews must be managed through their GitHub lifecycle.

See [Evolving Previews & Overrides](/guides/example/) for baseline-only and build-backed examples.

---

### `get` & `inspect`

Inspects desired and observed state.

```bash
envy composition get <composition-id>
envy composition inspect <composition-id>
```

---

### `endpoints`

Retrieves allocated ingress URLs and readiness status.

```bash
envy composition endpoints <composition-id>
```

---

### `logs`

Fetches a bounded snapshot of recent container logs for any microservice in the request path.

```bash
envy composition logs <composition-id> --component <service-name> [flags]
```

**Flags**:

- `--component <string>`: Logical component name (default: `service-b`).
- `--tail-lines <int>`: Number of recent log lines per pod (default: `200`, maximum: `1000`).
- `--max-bytes <int>`: Total log response cap (default: `65536`, maximum: `262144`).
- `--since <duration>`: Only return logs newer than a whole-second duration, up to `24h`.
- `--previous`: Read the last terminated container instance.

---

### `events`

Fetches durable lifecycle event history stored in PostgreSQL.

```bash
envy composition events <composition-id> [--limit 20]
```

---

### `destroy`

Initiates asynchronous teardown and resource reclamation.

```bash
envy composition destroy <composition-id>
```

---

## Installation preflight

```sh
envy installation check --file installation.json
```

This optional, read-only check inspects the target infrastructure from an adapted installation specification. It does not install Envy, and Helm does not require it. Exit 0 means checks passed, 1 means failure, and 2 means incomplete evidence. See [mesh diagnostics](/getting-started/mesh-installation/#troubleshooting-and-upgrades) for details.

## Source and published-build selection

```sh
envy source list --project shop
envy source resolve --project shop --repository backend --component pricing --ref main
envy composition create --project shop --baseline staging --name pricing-review \
  --build pricing=BUILD_ID
```

Select a returned published build ID for the resolved commit. An empty build list means CI must publish that revision first; Envy does not start CI or substitute an older build. Direct images do not provide source provenance. See the [source-build reference](https://github.com/dblooman/envy/blob/main/docs/source-builds.md) for repository registration and reporting.

## `envy catalog` Subcommands

### `validate`

Validates an application catalog manifest against cluster infrastructure without modifying database state.

```bash
envy catalog validate --file application.json
```

---

### `apply`

Atomically registers projects, components, and baselines into Envy's database.

```bash
envy catalog apply --file application.json
```

---

## `envy pr-preview` Subcommands

Manage [GitHub App-owned previews](/integrations/github-app/):

```sh
envy pr-preview list --project shop --limit 20
envy pr-preview get PREVIEW_ID
envy pr-preview stop PREVIEW_ID
envy pr-preview restart PREVIEW_ID
envy pr-preview policy --file preview-policy.json
```

List supports `--after` for pagination. Stop and restart initiate asynchronous
lifecycle work. Restart requires an open, labelled PR and an enabled policy;
it creates a fresh URL and TTL after old-resource cleanup. Generic composition
updates cannot change an owned preview; generic deletion also records a stop.

## `envy frontend` Subcommands

Integrates static frontend branches (e.g. Cloudflare Pages or Vercel) with backend compositions:

- `envy frontend bind --project <p> --frontend <name> --revision <sha> --composition <id>`
- `envy frontend resolve --project <p> --frontend <name> --revision <sha> --timeout 60s`
- `envy frontend publish --project <p> --frontend <name> --revision <sha> --expected-version <version> --url <url>`
- `envy frontend check --project <p> --frontend <name> --revision <sha> --expected-version <version> --composition-generation <generation> --status passed|failed --message "Playwright passed"`
