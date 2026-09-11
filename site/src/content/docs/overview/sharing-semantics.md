---
title: Sharing & Isolation Semantics
description: Understanding the boundaries of Envy's request-routing isolation model versus data isolation.
---

Envy operates on the principle of **Request-Routing Isolation**. It isolates the code execution of modified microservices while purposefully sharing underlying cluster resources and dependencies from the deployed reference baseline.

---

## What is Isolated?

When a composition is active, Envy guarantees strict isolation for the following concerns:

### 1. Workload Execution
- Overridden services run as dedicated Kubernetes `Deployments` inside an isolated, temporary namespace named `envy-cmp-<id>`.
- Overridden pods have independent resource allocations, environment configurations, and container images.
- Unhealthy overrides or container crashes never bring down the shared baseline services.

### 2. Ingress & Traffic Routing
- Each composition receives a dedicated, deterministic preview URL:
  `http://cmp-<id>.envy.localhost:8080`
- Normal traffic hitting `http://baseline.envy.localhost:8080` will **never** reach an override pod.
- Only requests that originate from the composition's preview URL (and carry verified W3C baggage) reach the override pods.

### 3. Lifecycle & Expiry
- Compositions have an explicit TTL (defaulting to 8 hours). When expired, their namespaces, routes, and workloads are automatically destroyed without touching baseline pods.

---

## What is Shared?

Because Envy is designed for rapid integration testing rather than multi-tenant sandboxing, the following layers are shared:

### 1. Unmodified Workloads
Any microservice not explicitly listed in the composition's `overrides` map continues to run from the baseline namespace (`envy-baseline`).

### 2. State & Persistence (Databases & Caches)
- Overridden services connect to the same shared baseline databases (e.g., PostgreSQL, MySQL, Redis) as the reference environment.
- **Important**: Any database writes or row mutations made during preview testing will mutate shared baseline data.
- If your test scenario involves destructive database schema drops or data purges, ensure you use test tenant IDs or transactional rollbacks.

### 3. Asynchronous Side Effects & Message Queues
- If an overridden service publishes a message to a shared Kafka topic or SQS queue, existing baseline consumers will receive and process that message unless queue-level context routing is implemented.

---

## Architectural Comparison

```text
┌────────────────────────────────────────────────────────┐
│               Envy Composition: cmp-7f39a              │
│                                                        │
│  [Isolated]  Preview Host: cmp-7f39a.envy.localhost    │
│  [Isolated]  Pod: service-b:v2 in namespace envy-cmp   │
│  [Isolated]  Route: VirtualService baggage match       │
│                                                        │
│  [Shared]    Pod: gateway-v1 (reused from baseline)    │
│  [Shared]    Pod: service-a-v1 (reused from baseline)  │
│  [Shared]    Database: baseline-postgres (reused)      │
│  [Shared]    Cache: baseline-redis (reused)            │
└────────────────────────────────────────────────────────┘
```

---

## Best Practices for Safe Sharing

1. **Use Synthetic Test Entities**: When running automated QA or agent browser tests, prefix test accounts, emails, and entities with `test-envy-<id>`.
2. **Backward-Compatible Schemas**: When migrating databases, apply additive changes (e.g., adding nullable columns) so shared baseline services are not broken.
3. **Short Expiry Windows**: Keep composition TTLs short (e.g., `2h` or `4h`) during active pull request review, allowing Envy's garbage collection to reclaim resources immediately.
