---
title: Model Context Protocol (MCP) Reference
description: Guide for Envy's Model Context Protocol (MCP) stdio server, tool definitions, and AI agent configuration.
---

Envy includes a first-class **Model Context Protocol (MCP)** stdio server. This enables autonomous AI coding agents (such as Claude Code, Cursor, Antigravity, and Windsurf) to spin up ephemeral compositions from a deployed reference baseline, verify microservice changes against live ingress, inspect container logs, and tear down resources cleanly.

---

## Server Executable & Configuration

The MCP server is distributed as a single compiled Go binary located at:
`.envy/bin/envy-mcp`

The MCP server translates JSON-RPC stdio calls into authenticated HTTP requests against the Envy REST API. **The adapter requires zero Kubernetes credentials or cluster-admin rights.**

### Environment Variables

| Variable              | Required | Description                                                      |
| :-------------------- | :------- | :--------------------------------------------------------------- |
| `ENVY_API_URL`        | **Yes**  | Base URL of the Envy API server (e.g., `http://127.0.0.1:8081`). |
| `ENVY_API_TOKEN_FILE` | Optional | Named machine-token file; overrides saved CLI login.             |
| `ENVY_API_TOKEN`      | Optional | Raw API Bearer token string.                                     |

---

## Authentication and remote MCP

Local dev mode runs as Admin without credentials. For password or Google installations, run `delivery auth login --api-url https://envy.example.com` before starting the local adapter. CLI and stdio MCP share saved credentials and refresh them automatically. Missing login produces a CLI login instruction; the adapter never opens a browser itself.

For URL-based clients, add `https://envy.example.com/mcp` as a Streamable HTTP MCP server. An OAuth-capable client discovers Envy's authorization endpoints, opens browser login, and asks you to approve full installation access. Google accounts must match the configured Workspace-domain or verified-email allowlist. Envy issues its own MCP credentials; Google tokens are never passed through.

Browser sessions last seven days; agent access tokens last fifteen minutes with refresh for at most thirty days. Use Installation → Sign out everywhere to revoke all of your browser and agent sessions. Existing machine tokens remain available for unattended automation.

## Agent Client Setup

### Claude Desktop (`claude_desktop_config.json`)

Add Envy to your Claude Desktop configuration:

```json
{
  "mcpServers": {
    "envy": {
      "command": "/absolute/path/to/envy/.envy/bin/envy-mcp",
      "env": {
        "ENVY_API_URL": "http://127.0.0.1:8081"
      }
    }
  }
}
```

### Cursor (`.cursor/mcp.json`)

```json
{
  "mcpServers": {
    "envy": {
      "command": "/absolute/path/to/envy/.envy/bin/envy-mcp",
      "args": [],
      "env": {
        "ENVY_API_URL": "http://127.0.0.1:8081"
      }
    }
  }
}
```

---

## MCP Tools Reference

### 1. Composition Lifecycle Tools

#### `create_composition`

Creates a temporary composition combining baseline services with 1 to 3 microservice image overrides.

**Arguments**:

```json
{
  "project": "demo",
  "baseline": "staging",
  "name": "pr-auth-fix",
  "overrides": {
    "service-b": { "image": "envy/service-b:v2" }
  },
  "ttl": "4h"
}
```

**Returns**:

```json
{
  "id": "cmp-8f3a12",
  "phase": "created",
  "generation": 1,
  "endpoints": {
    "public": {
      "url": "http://cmp-8f3a12.envy.localhost:8080",
      "ready": false
    }
  }
}
```

---

#### `wait_for_composition`

Polls until the composition reaches `ready` phase (or times out/fails).

**Arguments**:

- `id` (string, required): ID returned by `create_composition`.
- `timeout_seconds` (number, optional): Maximum seconds to wait (default `30`, max `60`).

**Returns**:

- Full composition object with `endpoints.public.ready: true`.

---

#### `get_composition`

Retrieves the full desired and observed state of a composition.

**Arguments**:

- `id` (string, required)

---

#### `get_composition_endpoints`

Retrieves public ingress and internal cluster endpoints for an active composition.

**Arguments**:

- `id` (string, required)

---

#### `update_composition`

Triggers an atomic rolling update to one or more overridden component images.

**Arguments**:

- `id` (string, required)
- `expected_generation` (number, required): Prevents race conditions with other agents.
- `overrides` (object, required): Updated map of component names to image tags.

---

#### `destroy_composition`

Initiates graceful, asynchronous teardown of an ephemeral composition.

**Arguments**:

- `id` (string, required)

---

### 2. Diagnostics & Logs Tools

#### `get_component_logs`

Fetches bounded container log snapshots for an overridden or shared component.

**Arguments**:

- `id` (string, required)
- `component` (string, required): Microservice name (e.g. `service-b` or `gateway`).
- `tail_lines` (number, optional): Number of recent log lines per pod (default `200`, max `1000`).
- `max_bytes` (number, optional): Total response cap (default `65536`, max `262144`).
- `since_seconds` (number, optional): Whole-second lookback, up to `86400`.
- `previous` (boolean, optional): Read the last terminated container instance.

> **Note on Shared Baseline Logs**: If inspecting an unmodified baseline service (like `gateway`), returned logs are explicitly marked `shared-baseline` and are not filtered by composition baggage.

---

#### `list_composition_events`

Retrieves durable lifecycle events stored in PostgreSQL (e.g. creations, route reconciliations, image updates, teardown milestones).

**Arguments**:

- `id` (string, required)
- `limit` (number, optional): Page size (default `20`).
- `after` (string, optional): `next_cursor` from the preceding page.

---

### 3. Catalog Discovery Tools

Agents use these tools to discover existing projects and valid components before creating a composition:

| Tool                | Purpose                                                               |
| :------------------ | :-------------------------------------------------------------------- |
| `list_projects`     | Lists registered projects and their descriptions.                     |
| `list_components`   | Lists approved overridable components in a project.                   |
| `get_component`     | Retrieves port and protocol profile for a component.                  |
| `list_baselines`    | Lists registered reference baselines (the local demo uses `staging`). |
| `list_compositions` | Lists active and recently expired compositions.                       |

---

### 4. Frontend & Preview Binding Tools

Allows agents to bind a static frontend build (e.g., Cloudflare Pages) to a backend composition:

- `bind_frontend`: Associates a Git commit SHA and frontend project with a backend composition.
- `resolve_frontend`: Waits for backend composition readiness and returns the public API URL.
- `publish_frontend`: Records deployed preview URL.
- `report_frontend_check`: Submits automated browser / Playwright test results (`passed` or `failed`) with evidence logs.

---

### 5. Recipe Tools

- `export_recipe`: Exports an active composition's intent into a declarative recipe.
- `validate_recipe`: Verifies recipe syntax and target infrastructure.
- `recreate_recipe`: Creates a new composition from saved intent. Shared baseline data may have changed since export.
