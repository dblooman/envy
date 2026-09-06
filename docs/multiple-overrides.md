# Multiple component overrides

A composition accepts one to three approved component image overrides. Every
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

PATCH supplies the complete existing override map with an expected generation.
It can change one or more images but cannot add or remove component keys. Only
changed Deployment templates roll; identity, URL and expiry remain stable.
Rolling deployments are not an atomic application-wide cutover.

Deletion withdraws the one public hostname, verifies withdrawal, drains, removes
all aggregate route entries, then deletes the owned namespace and verifies its
absence. Legacy single-override compositions are migrated without changing
workload identities or restarting pods solely because of the migration.

REST and MCP use the existing override map. CLI create/update accepts repeated
`--override component=image` flags; legacy `--component` with `--image` remains
available for one override. The frontend selects multiple approved components
and supplies all images on update.
