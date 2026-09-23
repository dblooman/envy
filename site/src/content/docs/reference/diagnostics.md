---
title: Diagnostics & Error Codes
description: Reference guide for HTTP error responses, conflict codes, and diagnostic troubleshooting in Envy.
---

This reference explains how to inspect recorded blockers and verification
evidence, then use HTTP errors, logs, and events to troubleshoot a composition.

Structured diagnosis and baseline-drift freshness were merged after the v0.8.0
release. Use a newer source build until a release includes them; older releases
still support bounded logs and lifecycle events.

## Start with the diagnosis

Open a live preview's **Overview → What is blocking this preview?**, or run:

```bash
envy composition diagnosis <id>
envy composition verification <id> --limit 20
```

The diagnosis groups observed workload, messaging, routing, verification,
baseline-observation, and cleanup conditions. Each blocker includes a scope,
observation time, and next action. Several blockers may be independent; their
order does not establish a root cause. Declared workload prerequisites appear
first when known. Shared application dependencies are declarations, not proof
that a database or broker is healthy or isolated. Inspect those dependencies
through the operator's own checks and telemetry.

`state` describes the diagnosis (`pending`, `blocked`, `healthy`, `unknown`, or
`cleanup`). Cleanup is separate from serving readiness. A missing observation
or unavailable evidence store never means that deletion succeeded.

Verification records are retained across updates and destruction. Their
`freshness` is derived when read:

| Freshness          | Meaning                                                                                                   |
| ------------------ | --------------------------------------------------------------------------------------------------------- |
| `current`          | Passed evidence covers this generation, verification contract, selected workloads, and observed baseline. |
| `stale`            | The generation, contract, selected workload, or observed baseline identity changed.                       |
| `failed`           | A fingerprint-covered check ran and failed.                                                               |
| `unavailable`      | The baseline observation is unavailable or has not refreshed within two minutes.                          |
| `unknown_coverage` | Historical or incomplete evidence cannot establish current coverage.                                      |

An inherited image or Service routing change can make prior evidence stale
without changing the preview generation. Envy requests a fresh check, but does
not freeze inherited services or shared data. An HTTP check proves reachability
and expected status only; service hops appear only when an instrumented checker
observed them. For full history, use **Overview → Verification evidence** and
select **Refresh evidence** after an observation changes.

Operators can configure links to external logs, traces, and dashboards. Envy
uses a generation or request ID only when that context was observed and
validated; absent telemetry does not block preview readiness. Bounded inherited
logs are labelled **shared-baseline** and are not filtered to one preview.

---

## HTTP Status Codes

| Code  | Name                | Scenario                                                                                                     |
| :---- | :------------------ | :----------------------------------------------------------------------------------------------------------- |
| `200` | OK                  | State query, log snapshot, or successful event query.                                                        |
| `201` | Created             | Resource registered in catalog (project, component profile, baseline).                                       |
| `202` | Accepted            | Composition creation, update, or deletion intent accepted.                                                   |
| `400` | Bad Request         | Invalid JSON, disallowed image or component, or invalid TTL.                                                 |
| `401` | Unauthorized        | Missing, expired, or invalid authentication credentials.                                                     |
| `404` | Not Found           | Target composition ID or catalog entity does not exist.                                                      |
| `409` | Conflict            | Generation mismatch (`expected_generation`), idempotency key payload conflict, or active update in progress. |
| `410` | Gone                | Composition or binding is no longer available.                                                               |
| `429` | Too Many Requests   | Configured capacity exceeded.                                                                                |
| `503` | Service Unavailable | Required infrastructure or service is unavailable.                                                           |

---

## Diagnosing Common Issues

### 1. Generation Conflict (`409 Conflict`)

```json
{
  "error": {
    "code": "conflict",
    "message": "stale expected generation 1; composition is currently at generation 2"
  }
}
```

**Fix**: Always fetch the latest composition status with `envy composition get <id>` before issuing updates to ensure your `expected_generation` is current.

---

### 2. Composition Remains in `provisioning` Phase

If `envy composition wait` times out:

1. **Read observed blockers**:
   ```bash
   envy composition diagnosis <id>
   ```
2. **Check Pod Logs**:
   ```bash
   envy composition logs <id> --component <service>
   ```
3. **Inspect Cluster Diagnostics**:
   Verify whether your container image exists in the registry, whether image pull secrets are configured, or if the container is crashing during startup.
4. **Inspect Durable Events**:
   ```bash
   envy composition events <id>
   ```
