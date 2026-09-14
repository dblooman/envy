---
title: Local Quickstart Guide
description: Set up a local Envy cluster and create, test, update, and remove a preview.
---

For an existing Cilium or Linkerd cluster, start with [Choose Your Mesh](/getting-started/mesh-installation/). This local quickstart uses the Istio profile.

This guide walks you through setting up a dedicated local development cluster with Docker, Kind, and Istio, spinning up the baseline demo application, and creating your first composition.

---

## Prerequisites

Ensure the following tools are installed on your host machine:

- **Docker Desktop** or Colima (must be running; start with 4 CPUs and 8 GiB RAM allocated)
- **Go 1.27.1+**
- **`kubectl`**
- **Python 3** and **`curl`**
- For dashboard/site development: **Node 24.8+ (Node 24)** and **pnpm 10.20.0**
- For chart checks: **Helm 3.19+ (Helm 3)**

Allow around 20 GiB free disk for images and builds. These resource allocations are starting recommendations, not measured minimums. Run `make setup` for development dependencies and `make doctor` for tool, Docker, and port checks. See [the contributor guide](https://github.com/dblooman/envy/blob/main/CONTRIBUTING.md) for focused development paths.

Setup automatically downloads checksum-verified **Kind 0.33.0** and **Istio 1.31.0**, using Kubernetes 1.36.4 and PostgreSQL 18.6 in an isolated Kind environment.

---

## Step 1: Provision the Development Cluster

Clone the repository and run `make dev`. This builds the Go control plane, boots a dedicated `envy-dev` Kind cluster, deploys the baseline microservices, and compiles the `delivery` CLI:

```bash
git clone https://github.com/dblooman/envy.git
cd envy
make dev
```

During setup, Envy creates:

- Dedicated cluster: `envy-dev`
- Local API server: `http://127.0.0.1:8081`
- Baseline ingress: `http://baseline.envy.localhost:8080`
- Admin token: `.envy/envy-dev/api-token`
- Compiled CLI: `.envy/bin/delivery`
- Stdio MCP server: `.envy/bin/envy-mcp`

---

First-time setup downloads images and builds binaries; its duration depends on your machine and network.

## Step 2: Configure Environment Variables

Load the generated API token into your shell and add the delivery CLI to your `PATH`:

```bash
# Load authentication token
export ENVY_API_TOKEN_FILE="$PWD/.envy/envy-dev/api-token"
export ENVY_API_URL="http://127.0.0.1:8081"
export PATH="$PWD/.envy/bin:$PATH"
```

Verify connectivity:

```bash
delivery composition list
```

---

## Step 3: Verify the Deployed Reference Baseline

The product model is a deployed reference baseline: it can represent a
deployed `main` branch, a release, a production-like environment, or another
approved environment. The local demo registers that baseline with the catalog
identifier `staging`; that name is a demo fixture, not a required Envy concept.

Before creating any overrides, verify the running baseline chain:

```bash
curl -s http://baseline.envy.localhost:8080/
```

**Expected Response**:

```json
{
  "chain": [
    {
      "service": "gateway",
      "version": "v1",
      "composition": "",
      "workload_id": "baseline-gateway",
      "deployment_composition": "baseline"
    },
    {
      "service": "service-a",
      "version": "v1",
      "composition": "",
      "workload_id": "baseline-service-a",
      "deployment_composition": "baseline"
    },
    {
      "service": "service-b",
      "version": "v1",
      "composition": "",
      "workload_id": "baseline-service-b",
      "deployment_composition": "baseline"
    }
  ]
}
```

---

## Step 4: Create Your First Composition

Now, create an ephemeral composition overriding `service-b` with version `v2`:

```bash
delivery composition create \
  --name preview-b2 \
  --image envy/service-b:v2
```

Envy responds immediately with the allocated composition object:

```json
{
  "id": "cmp-8f3a12",
  "phase": "created",
  "generation": 1,
  "endpoints": {
    "public": {
      "url": "http://cmp-8f3a12.envy.localhost:8080",
      "ready": false
    }
  }
}
```

The response above is abbreviated. Replace `cmp-8f3a12` in the remaining commands with the ID returned by your create command; the preview URL also uses that ID.

Wait for ingress verification:

```bash
delivery composition wait cmp-8f3a12 --timeout 60s
```

Once ready, query your preview URL:

```bash
curl -s http://cmp-8f3a12.envy.localhost:8080/
```

**Notice the difference**:

```json
{
  "chain": [
    {
      "service": "gateway",
      "version": "v1",
      "composition": "cmp-8f3a12",
      "workload_id": "baseline-gateway",
      "deployment_composition": "baseline"
    },
    {
      "service": "service-a",
      "version": "v1",
      "composition": "cmp-8f3a12",
      "workload_id": "baseline-service-a",
      "deployment_composition": "baseline"
    },
    {
      "service": "service-b",
      "version": "v2",
      "composition": "cmp-8f3a12",
      "workload_id": "cmp-8f3a12-service-b",
      "deployment_composition": "cmp-8f3a12"
    }
  ]
}
```

Only `service-b` was executed on `v2`. The `gateway` and `service-a` were reused directly from the deployed reference baseline!

---

## Step 5: Rolling Updates

Suppose your developer or agent builds a new revision, `service-b:v3`. You can apply a rolling update to the existing preview without changing its URL:

```bash
delivery composition update cmp-8f3a12 \
  --expected-generation 1 \
  --image envy/service-b:v3

delivery composition wait cmp-8f3a12 --timeout 60s
```

The composition generation increments to `2`, and requests to the URL now route to `service-b:v3`.

---

## Step 6: Cleanup

Delete your composition when testing is complete:

```bash
delivery composition destroy cmp-8f3a12
```

When you are done with local development, tear down the Kind cluster:

```bash
make dev-down
```
