---
title: Agent Workflow Kit
description: How autonomous AI coding agents use Envy to build, verify, debug, and ship microservice code safely.
---

The Envy Agent Workflow Kit provides guidelines, prompt instructions, and reusable skills so AI coding agents can work autonomously with Kubernetes preview environments.

---

## The Autonomous Verification Loop

```text
┌────────────────────────────────────────────────────────┐
│                   Autonomous Agent Loop                │
│                                                        │
│  1. Code Modification                                  │
│     Agent modifies service code on a Git branch.       │
│                                                        │
│  2. Build & Tag                                        │
│     Builds or receives a prebuilt image digest.        │
│                                                        │
│  3. Spin up Preview (`create_composition`)             │
│     Overrides only the modified service.               │
│                                                        │
│  4. Ingress Polling (`wait_for_composition`)           │
│     Waits until the declared verification succeeds.   │
│                                                        │
│  5. Functional & Browser Testing                       │
│     Executes curl, API tests, or Playwright checks.    │
│                                                        │
│  6. Diagnostic Recovery (`get_component_logs`)         │
│     Inspects container logs on failure to self-heal.   │
│                                                        │
│  7. Teardown (`destroy_composition`)                   │
│     Deletes preview and posts results to PR.           │
└────────────────────────────────────────────────────────┘
```

---

## Adding Instructions to Application Repositories

Envy provides an `AGENTS.md` template designed to be placed in each application repository. This file teaches coding agents how to interact with Envy:

```markdown
# Agent Instructions for Envy Preview Environments

## Pre-Requisites
- The Envy MCP server is configured via your IDE or CLI environment.
- Always inspect approved components before attempting an override.

## Workflow Rules
1. Call `list_components` for project `shop` before creating an override.
2. When creating a composition, use an explicit `name` indicating your task.
   Select the approved deployed reference baseline for the project; the local
   demo calls its fixture `staging`:
   `create_composition(project="shop", baseline="staging", name="agent-<task>", overrides={...})`
3. Always wait for readiness using `wait_for_composition` before making HTTP calls.
4. If requests return 500 or timeout:
   - Call `get_component_logs(id=..., component=...)`
   - Read the stack trace, fix the issue, and call `update_composition`
5. Always call `destroy_composition` when verification completes.
```

---

## Self-Healing and Log Inspection Pattern

When an AI agent introduces a bug (such as an unhandled nil pointer exception or database connection timeout), standard CI reports a generic red build.

With Envy, the agent can diagnose the issue directly:

1. **Test Failure**:
   ```bash
   GET http://cmp-7f39a.envy.localhost:8080/checkout
   HTTP 500 Internal Server Error
   ```
2. **Agent Calls `get_component_logs`**:
   ```json
   {
     "id": "cmp-7f39a",
     "component": "service-b",
     "tail_lines": 50
   }
   ```
3. **Log Diagnostic**:
   ```text
   panic: runtime error: invalid memory address or nil pointer dereference
   goroutine 18 [running]:
   github.com/org/service-b/internal/handler.HandleCheckout(...)
       /app/internal/handler/checkout.go:42 +0x3a
   ```
4. **Self-Healing Update**:
   The agent modifies `checkout.go`, builds a new image tag, and calls
   `update_composition(id=..., expected_generation=1, ...)`. The preview
   updates in place without losing its URL.

---

## Recipe Export and Recreation

Agents can serialize complex multi-override environments into portable **recipes**:

```bash
delivery recipe export cmp-7f39a > recipe.json
```

Another agent or team member can recreate the exact same environment the next day:

```bash
delivery recipe validate --file recipe.json
delivery recipe recreate --file recipe.json \
  --name agent-recreated \
  --idempotency-key recipe-cmp-7f39a
```
