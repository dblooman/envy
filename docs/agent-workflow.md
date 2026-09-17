# Agent workflow kit

The [environment workflow proposal](environment-workflows.md) maps short agent
tasks, evolving multi-repository work, frontend switching and next-day recreation.
It distinguishes current behavior from proposed recipe and agent-kit additions;
it is not an implemented API contract.

The kit now covers demand-driven compute and build-backed source discovery.
[Recipe export, validation and recreation](recipes.md) are available through
`envy recipe` and `export_recipe`, `validate_recipe`, `recreate_recipe` MCP
tools. Recipes save selected intent without keeping workloads alive. Component
membership updates and baseline-only compositions are implemented; see
[update semantics](updates.md). Frontend bindings still require a composition,
which can inherit the complete baseline with no override workloads.

The repository distributes a portable [envy-compose skill](../integrations/agent/skills/envy-compose/SKILL.md)
and an [AGENTS.md template](../integrations/agent/AGENTS.md.template). Copy the skill
directory into your agent's supported skill location and merge the template's
filled-in instructions into each application repository. Nothing is installed
globally by Envy. The kit uses the existing authenticated MCP server or CLI;
it does not start an agent runtime, build service or source-control integration.

MCP discovery includes `list_projects`, `list_components`, `get_component`,
`list_baselines`, and `list_compositions`. Pagination uses `after`/`limit` and
`next_cursor`. Coordinate through an explicit composition ID and current desired
generation. The five original lifecycle tools remain available alongside updates,
logs and lifecycle events.

Frontend tools are `bind_frontend`, `get_frontend_binding`, `resolve_frontend`,
`publish_frontend`, `report_frontend_check`, and `list_frontend_bindings`.
Resolution waits default to 60 seconds and allow at most 300 seconds; cancellation
is read-only. Binding/check tool results expose backend readiness separately from
caller-reported browser evidence. Expired bindings remain inspectable.

## CLI example

Use a committed frontend SHA from its own repository; its branch can differ from
any backend branch. Set `ENVY_API_URL` and `ENVY_API_TOKEN_FILE` using your existing
Envy configuration. Replace the composition ID and repository URL below.

```sh
revision=$(git rev-parse HEAD)
envy frontend bind --project shop --frontend storefront-web \
  --revision "$revision" --composition <composition-id> \
  --repository https://example.com/org/storefront

envy frontend resolve --project shop --frontend storefront-web \
  --revision "$revision" --timeout 60s
```

The resolver returns `api_url`, `composition_generation`, `binding_version` and
`expires_at`. Use these fields for the build and subsequent evidence. Publish and
check require `--expected-version`; check also requires
`--composition-generation`, `--status passed|failed`, and `--message` describing
what was actually tested. Use `envy frontend get` to fetch the current version
and `envy frontend list --composition <id>` for discovery.

A new URL invalidates prior frontend checks; a backend generation change makes
checks stale. Readiness is never a substitute for the repository's application
tests. See [frontend bindings](frontend-bindings.md) for lifecycle semantics and
the [Pages adapter](../integrations/cloudflare-pages/README.md) for build setup.

For asynchronous checks, the skill chooses [Pub/Sub isolation](pubsub-isolation.md)
from event schemas, behavior and downstream effects. Subscription capture does not
require a consumer override; processing tests can attach one later.
