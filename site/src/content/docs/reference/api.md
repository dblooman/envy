---
title: REST API Reference
description: Common Envy REST endpoints, request examples, and response behavior.
---

The REST API is Envy's authoritative control surface. It operates over HTTP with JSON payloads and conforms to the formal OpenAPI 3.0 specification ([api/openapi.yaml](https://github.com/dblooman/envy/blob/main/api/openapi.yaml)). This page summarizes common composition endpoints; the OpenAPI file includes the complete catalog, source, frontend, installation, and recipe contracts. Response examples below show selected fields.

---

## Authentication & Headers

All `/v1` endpoints require a Bearer token in the `Authorization` header:

```http
Authorization: Bearer <token>
```

### Global Headers

- `Authorization: Bearer <token>`: Required for all `/v1` routes.
- `Content-Type: application/json`: Required for `POST` and `PATCH` requests.
- `Idempotency-Key: <string>`: Optional unique identifier (up to 128 printable ASCII characters) to ensure safe retries.

---

## Endpoints Summary

| Method   | Path                                                | Description                                   | Status         |
| :------- | :-------------------------------------------------- | :-------------------------------------------- | :------------- |
| `POST`   | `/v1/compositions`                                  | Create a new ephemeral composition            | `202 Accepted` |
| `GET`    | `/v1/compositions`                                  | List active compositions (paginated)          | `200 OK`       |
| `GET`    | `/v1/compositions/{id}`                             | Get full desired and observed state           | `200 OK`       |
| `GET`    | `/v1/compositions/{id}/status`                      | Get lightweight phase and latest operation    | `200 OK`       |
| `GET`    | `/v1/compositions/{id}/endpoints`                   | Get preview URLs and routing readiness        | `200 OK`       |
| `PATCH`  | `/v1/compositions/{id}`                             | Rolling image update with expected generation | `202 Accepted` |
| `DELETE` | `/v1/compositions/{id}`                             | Destroy composition asynchronously            | `202 Accepted` |
| `GET`    | `/v1/compositions/{id}/components/{component}/logs` | Fetch bounded container log snapshots         | `200 OK`       |
| `GET`    | `/v1/compositions/{id}/events`                      | Durable audit events log                      | `200 OK`       |
| `POST`   | `/v1/catalog/validate`                              | Dry-run catalog validation                    | `200 OK`       |
| `POST`   | `/v1/catalog/apply`                                 | Atomic catalog registration                   | `200 OK`       |
| `GET`    | `/v1/projects`                                      | List registered projects                      | `200 OK`       |
| `GET`    | `/v1/projects/{project}/components`                 | List approved component profiles              | `200 OK`       |
| `GET`    | `/v1/projects/{project}/baselines`                  | List registered baselines                     | `200 OK`       |

---

## Detailed Endpoint Schemas

### 1. Create Composition

```http
POST /v1/compositions
Authorization: Bearer <token>
Content-Type: application/json
Idempotency-Key: ci-pr-42-attempt-1

{
  "project": "demo",
  "baseline": "staging",
  "name": "pr-auth-fix",
  "overrides": {
    "service-b": { "image": "envy/service-b:v2" }
  },
  "ttl": "8h"
}
```

#### Response: `202 Accepted`

```http
Location: /v1/compositions/cmp-84f1a09

{
  "id": "cmp-84f1a09",
  "project": "demo",
  "baseline": "staging",
  "name": "pr-auth-fix",
  "generation": 1,
  "observed_generation": 0,
  "phase": "created",
  "expires_at": "2026-09-12T04:20:00Z",
  "created_at": "2026-09-11T20:20:00Z",
  "endpoints": {
    "public": {
      "url": "http://cmp-84f1a09.envy.localhost:8080",
      "ready": false
    }
  },
  "latest_operation": {
    "id": "op-01J98X7",
    "kind": "create",
    "status": "pending"
  }
}
```

---

### 2. Update Composition (Rolling Image Update)

```http
PATCH /v1/compositions/cmp-84f1a09
Authorization: Bearer <token>
Content-Type: application/json

{
  "expected_generation": 1,
  "overrides": {
    "service-b": { "image": "envy/service-b:v3" }
  }
}
```

#### Optimistic Concurrency & Errors

- If the current generation does not match `expected_generation`, the API responds with **`409 Conflict`**.
- The preview URL stays the same. Wait for the new generation to become ready before testing the updated images.

---

### 3. Fetch Container Logs

```http
GET /v1/compositions/cmp-84f1a09/components/service-b/logs?tail_lines=50&since_seconds=600
Authorization: Bearer <token>
```

#### Response: `200 OK`

```json
{
  "message": "returned log snapshots",
  "component": "service-b",
  "source": "override",
  "composition_filtered": false,
  "streams": [
    {
      "pod": "cmp-84f1a09-service-b-7f4d",
      "container": "service-b",
      "text": "2026-09-11T20:22:01Z [info] server listening on :8080\n",
      "truncated": false
    }
  ],
  "partial": false,
  "truncated": false
}
```

If querying an unmodified baseline service (for example `gateway`), the
response identifies `source` as `"shared-baseline"` and the returned logs are
not filtered by composition baggage.

---

### 4. Delete Composition

```http
DELETE /v1/compositions/cmp-84f1a09
Authorization: Bearer <token>
```

#### Response: `202 Accepted`

The composition phase transitions to `destroying`. Cleanup is asynchronous, repeatable, and idempotent.
