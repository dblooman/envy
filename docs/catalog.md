# Catalog registration

Envy accepts authenticated POST requests at `/v1/projects`,
`/v1/projects/{project}/components`, and `/v1/projects/{project}/baselines`.
Registrations are immutable: duplicate IDs return 409. Bindings select existing,
live Kubernetes Services; registration never adopts or changes baseline workloads.
A composition persists its approved component profiles and baseline bindings.
Image updates retain that plan and cannot add or remove overridden components.

Components use HTTP with a declared port, liveness and readiness paths, and an
approved map of literal environment variables. The `http-small` deployment
profile fixes resource limits, a non-root identity, read-only root filesystem,
and an unprivileged service account without an API token. Secrets and arbitrary
pod settings are not accepted. Environment values are catalog-visible and must
not contain credentials.

A baseline declares its routing namespace, existing Istio Gateway, entry
component, ordered verification chain, and Service FQDN/port/image bindings.
Only the `envy-chain` verification contract is supported: every hop reports
service, version, workload identity, deployment identity and request-observed
composition context. This is not a general application health parser.
Registration checks injected namespaces, HTTP Services with ready sidecars,
Gateway host coverage, baseline ingress normalization and real baseline
connectivity. Conflicting mesh hosts are rejected. Database claims prevent two
registered baselines from owning the same mesh Service host. Providers repeat
ownership checks before reconciliation.

Routing aggregates are compiled per registered Service host, with deterministic
composition matches followed by that Service's baseline destination. Preview
routes resolve the entry component explicitly, including an overridden entry.
All compositions retain their globally unique preview hostname. The development
installation supports a single ingress gateway deployment and loopback HTTP
endpoint domain. Baseline endpoint URLs must use that configured domain/port.

Catalog discovery is paginated in REST and MCP (`list_projects`,
`list_components`, `get_component`, `list_baselines`). The frontend can register
catalog entries and select a project, baseline and one to three approved overrides. Shared state and live
inheritance semantics are unchanged. Catalog database operations are defined in
`internal/persistence/postgres/queries/catalog.sql` and generated into type-safe Go
code with `sqlc` (`make sqlc`).
