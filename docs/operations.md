# Operations: backup, restore, and uninstall

PostgreSQL is Envy's canonical desired state. Back up its database together with
the stable installation ID and the operator-managed Secret references used by
the chart. Do not include token, proxy-secret, database-password, or registry
credential values in Envy exports or repository files.

Use the PostgreSQL operator's consistent backup mechanism. Before an upgrade,
run the chart migration Job and record the chart version, server image digest,
and applied migration names. Helm rollback does not roll back PostgreSQL.

If restoring an older database backup, scale the Envy Deployment to zero first.
The restored desired state can disagree with later composition namespaces and
Istio routes. Inspect the recorded workload inventory and live labels, then
restart Envy and explicitly decide which compositions should be destroyed or
recreated. Never infer that a resource is orphaned only because a database row
is absent after restore.

Uninstall has two intentional paths:

1. **Retain environments and database.** Remove the chart only. Composition
   namespaces, routes, and PostgreSQL remain so the same installation ID can be
   reinstalled and reconciliation can resume.
2. **Clean up environments.** Use Envy to destroy every composition and wait
   for each tombstone to reach `destroyed`; then uninstall the chart. Retain or
   remove PostgreSQL according to the team's database retention policy.

Neither path removes borrowed baselines, Istio gateways, authentication proxies,
DNS/TLS resources, or externally managed Secrets. An interrupted cleanup is
visible through composition status and can be resumed after reinstallation.
