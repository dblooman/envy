# Multiple component overrides

A composition accepts zero to three approved component image overrides. An empty
override set retains a usable preview URL that inherits the entire baseline. Every
component must belong to the selected project and have a baseline binding.
PostgreSQL stores all resolved profiles and per-component Kubernetes identities.
The catalog and resource-sharing boundaries remain unchanged.

All overrides share the composition's injected namespace and ownership token.
Each has its own Deployment and Service. Quotas scale with the override count
and allow rolling-deployment headroom. The baseline's entry component can itself
be overridden; preview ingress resolves that destination explicitly.

Initial publication waits for all override Deployments and endpoints. Request
verification checks every overridden hop against its observed pod UID, verifies
inherited hops against the current baseline, and checks context at every hop.
Once published, all explicit override routes are retained when any workload
becomes unhealthy. There is no silent fallback to inherited versions.

PATCH supplies the complete desired override map with an expected generation.
It can add, remove, or change components within the zero-to-three limit. Omitted
components return to inheritance; an empty map retains the URL and inherits the
complete baseline. Removed overrides are withdrawn, drained, and deleted. Only
changed Deployment templates roll; composition identity, URL and expiry remain stable.
See [update semantics](updates.md) for readiness and failure behavior.
Rolling deployments are not an atomic application-wide cutover.

Deletion withdraws the one public hostname, verifies withdrawal, drains, removes
all aggregate route entries, then deletes the owned namespace and verifies its
absence. Legacy single-override compositions are migrated without changing
workload identities or restarting pods solely because of the migration.

REST and MCP use the existing override map. CLI create/update accepts repeated
`--override component=image` flags; legacy `--component` with `--image` remains
available for one override. The frontend selects multiple approved components
and supplies all images on update.

Migration 004 backfills legacy workload/profile maps and expands the installation's
seeded demo catalog approvals to gateway and service-a, with explicit downstream
Service addresses. Existing compositions retain their resolved profiles. This
seed migration leaves borrowed deployments untouched; user-created catalog
registrations remain immutable.
