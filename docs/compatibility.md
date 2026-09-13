# Compatibility matrix

| Envy server/chart | Kubernetes | Istio | PostgreSQL | Notes |
| --- | --- | --- | --- | --- |
| 0.3.x / chart 0.1.x | 1.36 | 1.31 | 18 | Single controller replica; PostgreSQL is external and migrations are additive. |

The server image and chart must be upgraded together within the same minor line.
The chart runs its migration Job before server pods start. A newer chart can add
schema that an older server does not understand, so Helm rollback is never a
database rollback and should be paired with the restore procedure in
[operations](operations.md).

This matrix records the local development version line. Chart rendering and
isolated database startup and HTTPS proxy authentication against the deployed API
have been exercised. Real Istio HTTPS listener acceptance covers certificate
trust, exact-host API/website access and route removal on the local gateway.
A separate baseline exercises REST composition creation, control-plane CA trust,
three-hop HTTPS preview verification and destruction.
The complete fresh-cluster, nondefault-revision, external-authentication, upgrade,
recovery, and private-registry gate is still pending.
It is not a claim that Envy
installs or owns Kubernetes, Istio, PostgreSQL, DNS/TLS, or authentication
proxies. Operators validate their cluster-specific gateway, injection revision,
external origin, and ingress path through `delivery installation check` and the
optional in-cluster preflight Job.
