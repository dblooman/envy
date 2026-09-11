---
title: What is Envy?
description: An introduction to Envy, ephemeral microservice compositions, and selective sharing from a deployed reference baseline on Kubernetes.
---

**Envy** creates temporary, isolated compositions of a distributed microservice application by combining a deployed reference baseline with selected workload overrides on Kubernetes.

Developers, CI systems, and external AI coding agents use Envy via **REST**, **Model Context Protocol (MCP)**, or the **`delivery` CLI** to spin up environments, inspect readiness, obtain verified URLs, stream logs, and tear them down.

---

## The Problem: The Microservice Preview Dilemma

When engineering teams build microservice architectures on Kubernetes, validating changes before merging to production becomes notoriously painful:

```text
Traditional Namespace Cloning:
PR for Service B ──► Duplicate: Gateway + Auth + Service A + Service B + Service C + Redis + Postgres
                      (10-15 minutes setup, massive cloud cost, empty databases, zombie environments)
```

1. **Massive Cloud Waste**: To test a 10-line change in a single microservice, traditional tooling clones the entire Kubernetes namespace (often 20 to 50 pods, databases, and caches). A team with 15 active PRs ends up running hundreds of idle pods.
2. **Slow Feedback Loops**: Booting dozens of containers, mounting persistent volumes, and running database migrations takes 10 to 15 minutes per PR.
3. **Stale Mock Data**: Cloned databases are either completely empty or seeded from days-old dumps, causing subtle integration regressions to slip through.
4. **Poor AI Agent Integration**: Autonomous coding agents (like Claude Code, Cursor, Antigravity, and Windsurf) need lightweight, scriptable environments they can spin up, test, inspect logs for, and destroy in seconds without cluster-admin permissions.

---

## The Solution: Selective Overrides with Zero-Copy Baseline

Envy flips the script. Instead of duplicating the universe, Envy uses **one authoritative deployed reference baseline** in your cluster. That baseline can represent `main`, a release, a production-like environment, or another approved deployment. When you or an agent test changes to one or two services, Envy creates an ephemeral **composition**:

```text
Reference Baseline: gateway-v1 ──► service-a-v1 ──► service-b-v1 (Shared)
                                                        │
Composition URL:   gateway-v1 ──► service-a-v1 ──► service-b-v2 (Override)
```

- **Only changed workloads are deployed**: If you only modified `service-b`, only `service-b-v2` is spawned in an isolated composition namespace (`envy-cmp-<id>`).
- **Unchanged workloads are shared**: `gateway` and `service-a` remain shared from the baseline.
- **Wire-speed request routing**: Requests entering the preview URL (`http://cmp-<id>.envy.localhost:8080`) are tagged with standard W3C `baggage: composition=<id>`. Istio sidecars inspect the baggage header at the network level and route traffic to your override pod automatically.
- **Zero code changes**: Standard OpenTelemetry / W3C TraceContext and Baggage headers propagate transparently across HTTP hops.

---

## Core Capabilities at a Glance

| Capability | Description |
| :--- | :--- |
| **Selective Overrides** | Select 1 to 3 approved component overrides per composition. |
| **Zero-Copy Baseline** | Share baseline services, caches, databases, and third-party credentials. |
| **Istio Ingress & Routing** | Ingress gateways allocate deterministic preview hostnames and manage W3C baggage headers. |
| **Native MCP Server** | First-class Model Context Protocol stdio tools designed specifically for AI coding agents. |
| **Delivery CLI** | Single Go binary with JSON outputs, generation tracking, and rolling updates. |
| **Out-of-Band Control Plane** | PostgreSQL persists state; the Envy control plane never sits in the critical request path. |
| **Durable Lifecycle** | Automatic TTL expiry (8 hours default), idempotent retries, and transactional audit history. |
| **Full-Stack Previews** | Link frontend branches (Cloudflare Pages) with backend compositions by Git commit SHA. |

---

## Next Steps

- Check out the [Architecture & Mechanics](/overview/architecture/) to understand the control plane and data plane split.
- Follow the [Local Quickstart](/getting-started/quickstart/) to run Envy locally on Docker and Kind in under 5 minutes.
- Discover how AI agents use Envy via the [Model Context Protocol (MCP)](/agents/mcp-server/).
