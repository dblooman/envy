# ADR 008: Envy surface and licensing

Status: Accepted

Date: 2026-09-05

## Decision

Publish an MIT-licensed Go control plane with an authoritative REST API and a thin official-Go-SDK stdio MCP server. Defer the CLI until the following milestone.

## Alternatives

Separate business logic inside MCP or CLI creates incompatible behavior. A public SDK would freeze interfaces before experience with the REST and MCP flows.

## Consequences

The first five MCP tools create, get, wait, fetch endpoints, and destroy via the private HTTP client. Waiting is bounded and cancellation does not delete. Read-only discovery is available through REST. No catalog writes, updates, logs, UI, or public SDK are promised in this slice.

## Revisit conditions

Add CLI, catalog management, updates, logs/events, and discovery tools after the first acceptance gate; consider a public SDK when the API has stabilized.

## Subsequent implementation

The initial acceptance gate passed. The Envy CLI now exposes create, get,
list, wait, endpoints, update, and destroy through the private REST client. MCP
adds `update_composition` as its sixth tool. A separately added web frontend also
calls REST. Bounded component logs and paginated lifecycle events are now
available through REST, CLI, MCP, and the frontend. MCP has eight tools. Catalog
management and discovery MCP tools remain subsequent work.
