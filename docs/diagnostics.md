# Component logs and lifecycle events

For Istio, Cilium, and Linkerd prerequisites, examples, and acceptance status, see
[mesh installation profiles](mesh-installation.md). Istio-specific instructions below apply only to the Istio profile.

REST owns both diagnostics interfaces. CLI and MCP use the private HTTP client.
The web frontend can consume the same JSON endpoints.

`GET /v1/compositions/{id}/components/{component}/logs` reads a bounded snapshot
of the approved application container from at most three current pods, newest
first within a bounded discovery page of up to 100 selected pods. More candidates
are reported as truncation. It never streams indefinitely or reads sidecar/init-container logs.
Options are `tail_lines` (default 200, 1–1000 per pod), `max_bytes` (default 65536,
1–262144 across pods), optional `since_seconds` (1–86400), and `previous` (default
false, last terminated container instance). Requests have a ten-second deadline.
Pod and byte caps are reported as truncation; per-pod read failures return a
partial result with structured errors. An empty pod inventory is an empty result.
Logs are read from Kubernetes on demand and are not stored by Envy. They can
disappear on pod deletion or log rotation. Destroyed compositions return 409.

Override logs resolve only to the persisted namespace, Service, and Deployment,
checking their ownership tokens and recorded identities. Inherited logs resolve
through the registered baseline Service selector. Callers cannot supply arbitrary
namespaces, pods, containers, or label selectors. Inherited results have
`source: shared-baseline` and `composition_filtered: false`: they include traffic
from baseline and other compositions. Override results use `source: override`
and also do not claim request-level filtering. Routing context is not log access
authorization; the existing trusted-organisation API credential controls access.

`GET /v1/compositions/{id}/events` returns lifecycle events oldest first, with
`limit` (default 20, 1–100) and `after` (the preceding page's `next_cursor`). Event
IDs are decimal strings to avoid JavaScript integer precision loss. Each event
includes composition/project, generation, phase, operation, conditions, diagnostic
error when present, type, and timestamp. These are Envy lifecycle events, not
Kubernetes Events or application logs.

A PostgreSQL trigger records events in the same transaction as desired-state and
observation changes. It distinguishes create/update/destroy requests, expiry,
and observed status changes. Identical reconciliation polls, retry scheduling,
and stale or rejected writes produce no events. Row locking gives ordered commits
per composition; page cursors remain usable while new events arrive. History
survives restarts and destruction with the retained composition tombstone.
Migration records one `snapshot` event for each existing composition; it does not
invent history predating this feature. Event retention follows tombstone retention
in this development slice; a configurable retention policy remains future work.

CLI commands are `envy composition logs <id> --component service-b` and
`envy composition events <id> --limit 20 --after <cursor>`. Log flags mirror
REST, with `--since 1h` converted to seconds. MCP adds `get_component_logs` and
`list_composition_events`, with typed JSON results and concise compatibility text.


In the web frontend, open a composition's Inspect dialog in Live Mode. Select an
application component and use Read logs, or Load history and Next event page.
Results load on demand and are cleared when the composition, server, credentials,
or mode changes. Simulation mode displays an availability message instead of
fabricated diagnostics. Log content is rendered as plain text.

## Gateway API profiles

For Cilium, inspect current-generation `Accepted` and `ResolvedRefs` conditions on
each HTTPRoute and `Programmed` on its Gateway/listener. Linkerd producer routes
use `linkerd.io/policy-controller` and are content-addressed generation-one objects;
their `Accepted` and `ResolvedRefs` conditions intentionally omit a generation.
Preview ingress uses Envoy Gateway and remains generation-checked.
A route accepted by a different controller does not satisfy Envy readiness.

A ReferenceGrant authorizes only the specified backend Service and source
namespace. Missing grants, rejected parent references, disallowed route namespaces,
and stale controller status keep readiness false. Linkerd ServiceProfiles are
reported as conflicts before route changes. Missing `istio-proxy` is relevant only
to the Istio profile; Linkerd requires `linkerd-proxy`, and Cilium requires no proxy
container. Cilium host-network Gateway exposure is excluded by the pinned profile
because its GAMMA listener ports can collide when several Services use the same port.
