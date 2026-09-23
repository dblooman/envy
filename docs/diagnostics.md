# Component logs and lifecycle events

For Istio, Cilium, and Linkerd prerequisites, examples, and acceptance status, see
[mesh installation profiles](mesh-installation.md). Istio-specific instructions below apply only to the Istio profile.

REST owns both diagnostics interfaces. CLI and MCP use the private HTTP client.
The web frontend can consume the same JSON endpoints.

`GET /v1/compositions/{id}/components/{component}/logs` reads a bounded snapshot
of the approved application container from at most three current pods, newest
first within a bounded discovery page of up to 100 selected pods. More candidates
are reported as truncation. It never streams indefinitely. Composite overrides
also permit captured, approved sidecar and init-container selection using
`container`; omitted selects the application. Mesh containers remain excluded.
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
namespaces, pods, unapproved containers, or label selectors. Inherited results have
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

Use `--container bootstrap` or `--container database-proxy --previous` for
approved supporting containers. See [composite previews](composite-previews.md).

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

## Preview workspace and verification evidence

The preview workspace separates requested changes, observed deployment state, and
request evidence. Overview groups preview-owned and shared services. Changes
shows provenance and revisions; Logs provides bounded snapshots; History combines
actor requests and lifecycle records without discarding their original identities.
Legacy `section=Diagnostics` and `section=Activity` URLs open Logs and History.
Manual mutations and reported frontend checks are under Manage.

`GET /v1/compositions/{id}/verification?limit=20&after=<next_cursor>` returns
checks newest first. Each check records its generation, contract kind, outcome,
first/last checked times, baseline/preview HTTP status (zero means no response),
structured failure, and observed service-hop identities where supported. Identical
consecutive results coalesce; changed outcomes create a new record. Evidence is
committed in the same generation-fenced transaction as its observation and retained
with the composition tombstone. Migration does not invent checks for older previews.
HTTP checks prove reachability only. Chain results are scoped to that request,
not all application workflows. An older generation never verifies a newer one.

CLI: `envy composition verification <id> --limit 20 --after <cursor>`.
MCP: `list_verification_evidence`. Deploy migration 016 and the API before the UI
if releasing separately; older APIs show unsupported evidence in the UI.

## External observability links

Operators may add `observability` to the server JSON configuration selected by
`ENVY_CONFIG_FILE`. Links are project-scoped and do not affect preview readiness:

```json
{
  "observability": [
    {
      "project": "shop",
      "label": "Service logs",
      "kind": "logs",
      "url": "https://logs.example/explore?preview={preview}&service={component}&from={from}&to={to}"
    },
    {
      "project": "shop",
      "label": "Preview dashboard",
      "kind": "dashboard",
      "url": "https://grafana.example/d/previews?var-project={project}&var-preview={preview}"
    }
  ]
}
```

Kinds: `logs`, `traces`, `dashboard`. Placeholders: `{installation}`, `{project}`,
`{preview}`, `{component}`, `{generation}`, `{from}`, `{to}`. `generation` is the
current composition generation, not a trace or request identity. Substitutions are encoded for the URL
path or query. `from` is fifteen minutes before the composition's last update and
`to` is resolution time (UTC RFC3339). Component templates appear only in component
scope. Hosts cannot contain placeholders. URLs must use HTTP(S), without userinfo,
fragments, or credential query parameters. Never place secrets in templates;
external tools authenticate users independently. The server does not fetch these URLs.

`GET /v1/compositions/{id}/observability?component=pricing` returns resolved links.
CLI: `envy composition observability <id> --component pricing`.
MCP: `get_observability_links`. Empty configuration returns an empty list. Users
cannot change templates through composition requests.

Instrumentation is optional: propagate incoming W3C trace context and Envy preview
baggage on downstream calls, and record a preview identifier on spans explicitly
(baggage is not automatically a searchable span attribute). Include service name,
version, and workload identity using your observability system's conventions.
Do not attach request bodies, credentials, or arbitrary baggage to telemetry.
Envy supplies links, not an embedded trace search, collector, or telemetry store.
Declared dependencies, configured destinations, and observed request paths remain
distinct; absence of instrumentation never implies absence of a dependency.

Helm installations use the same list under the `observability` values key. The chart
passes it into the server configuration; invalid templates fail server startup.

Preview profile provenance also reports declared `shared_dependencies` captured by
an approved composite policy. The UI displays these declarations; they are not
runtime dependency discovery and do not establish that other dependencies are isolated.
