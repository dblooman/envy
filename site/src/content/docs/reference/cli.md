---
title: Delivery CLI Reference
description: Command-line interface guide for the Envy delivery binary.
---

The `delivery` CLI provides command-line control of Envy compositions, catalog configurations, frontend bindings, and diagnostics.

```bash
delivery [command] [subcommand] [flags]
```

---

## Global Environment & Flags

| Variable              | Description                                                             |
| :-------------------- | :---------------------------------------------------------------------- |
| `ENVY_API_URL`        | Base URL of the Envy API (default: `http://127.0.0.1:8081`).            |
| `ENVY_API_TOKEN_FILE` | Path to file containing bearer token (e.g. `.envy/envy-dev/api-token`). |
| `ENVY_API_TOKEN`      | Bearer token string (used if token file is not provided).               |

Commands return JSON by default; there is no `--json` flag. For a complete list of commands and flags, run `delivery --help` or add `--help` after a subcommand. This reference covers common preview workflows.

For scripting (this example requires `jq`):

```bash
delivery composition list | jq '.items[].id'
```

---

## `delivery composition` Subcommands

### `create`

Creates a new ephemeral composition.

```bash
delivery composition create [flags]
```

**Flags**:

- `--name <string>`: Human-readable composition name (**required**).
- `--project <string>`: Catalog project name (default: `demo`).
- `--baseline <string>`: Registered reference baseline (default: `staging` in the local demo).
- `--image <string>`: Shorthand for overriding the demo service.
- `--override <component=image>`: Key-value override pair. Repeatable up to 3 times:
  `--override orders=repo/orders:v2 --override payments=repo/payments:v2`
- `--ttl <duration>`: Expiration time limit (for example `2h` or `8h`; server default when omitted, maximum `24h`).
- `--idempotency-key <string>`: Optional unique idempotency key for safe retries.

---

### `wait`

Blocks until a composition reaches `ready` phase or times out.

```bash
delivery composition wait <composition-id> [--timeout 60s]
```

**Flags**:

- `--timeout <duration>`: Maximum wait duration (default: `30s`, maximum: `60s`).

---

### `update`

Applies an atomic rolling image update with generation concurrency control.

```bash
delivery composition update <composition-id> \
  --expected-generation <number> \
  --override <component=image>
```

**Flags**:

- `--expected-generation <int>`: (Required) Must match current composition generation. Returns `409 Conflict` if the generation changed.
- `--override <component=image>`: Complete map of updated image overrides.

---

### `get` & `inspect`

Inspects desired and observed state.

```bash
delivery composition get <composition-id>
delivery composition inspect <composition-id>
```

---

### `endpoints`

Retrieves allocated ingress URLs and readiness status.

```bash
delivery composition endpoints <composition-id>
```

---

### `logs`

Fetches a bounded snapshot of recent container logs for any microservice in the request path.

```bash
delivery composition logs <composition-id> --component <service-name> [flags]
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
delivery composition events <composition-id> [--limit 20]
```

---

### `destroy`

Initiates asynchronous teardown and resource reclamation.

```bash
delivery composition destroy <composition-id>
```

---

## `delivery catalog` Subcommands

### `validate`

Validates an application catalog manifest against cluster infrastructure without modifying database state.

```bash
delivery catalog validate --file application.json
```

---

### `apply`

Atomically registers projects, components, and baselines into Envy's database.

```bash
delivery catalog apply --file application.json
```

---

## `delivery frontend` Subcommands

Integrates static frontend branches (e.g. Cloudflare Pages or Vercel) with backend compositions:

- `delivery frontend bind --project <p> --frontend <name> --revision <sha> --composition <id>`
- `delivery frontend resolve --project <p> --frontend <name> --revision <sha> --timeout 60s`
- `delivery frontend publish --project <p> --frontend <name> --revision <sha> --expected-version <version> --url <url>`
- `delivery frontend check --project <p> --frontend <name> --revision <sha> --expected-version <version> --composition-generation <generation> --status passed|failed --message "Playwright passed"`
