---
title: Diagnostics & Error Codes
description: Reference guide for HTTP error responses, conflict codes, and diagnostic troubleshooting in Envy.
---

This reference details the HTTP status codes, error models, and diagnostic steps for troubleshooting Envy compositions.

---

## HTTP Status Codes

| Code | Name | Scenario |
| :--- | :--- | :--- |
| `200` | OK | State query, log snapshot, or successful event query. |
| `201` | Created | Resource registered in catalog (project, component profile, baseline). |
| `202` | Accepted | Composition creation, update, or deletion intent accepted. |
| `400` | Bad Request | Invalid JSON, disallowed image or component, or invalid TTL. |
| `401` | Unauthorized | Missing or invalid Bearer token in `Authorization` header. |
| `404` | Not Found | Target composition ID or catalog entity does not exist. |
| `409` | Conflict | Generation mismatch (`expected_generation`), idempotency key payload conflict, or active update in progress. |
| `410` | Gone | Composition or binding is no longer available. |
| `429` | Too Many Requests | Configured capacity exceeded. |
| `503` | Service Unavailable | Required infrastructure or service is unavailable. |

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

**Fix**: Always fetch the latest composition status with `delivery composition get <id>` before issuing updates to ensure your `expected_generation` is current.

---

### 2. Composition Remains in `provisioning` Phase

If `delivery composition wait` times out:

1. **Check Pod Logs**:
   ```bash
   delivery composition logs <id> --component <service>
   ```
2. **Inspect Cluster Diagnostics**:
   Verify whether your container image exists in the registry, whether image pull secrets are configured, or if the container is crashing during startup.
3. **Inspect Durable Events**:
   ```bash
   delivery composition events <id>
   ```
