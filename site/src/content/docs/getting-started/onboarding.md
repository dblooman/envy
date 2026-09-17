---
title: Onboard an Application
description: How to register microservice projects, approved component profiles, and baselines into Envy's catalog.
---

Envy uses declarative catalog profiles to define which microservices belong to a
project, which components may be overridden, and how traffic is routed and
verified from a deployed reference baseline.

---

Before registering a catalog, deploy the baseline application and its configured mesh and ingress, configure Envy API access, and ensure services forward W3C Baggage. Catalog registration does not deploy the baseline. The complete local shop setup is available with `make dev-shop` after the [quickstart](/getting-started/quickstart/).

For an evaluation or production cluster, install the released chart and image without building source:

```sh
helm upgrade --install envy oci://registry-1.docker.io/davey/envy-chart \
  --version X.Y.Z --namespace envy-system --create-namespace \
  --values values.yaml
```

The chart pulls `davey/envy:X.Y.Z` by default. Pin a specific chart version; do not use the rolling `latest` image tag for production.

For an existing Argo-managed application, use [deployment-derived onboarding](/integrations/argo-cd/#onboard-an-existing-service) to reuse approved Deployment configuration. The example below uses the legacy `http-small` profile with explicit settings.

## The Application Manifest (`application.json`)

To onboard a microservice application into Envy, keep an `application.json` manifest beside your application's source code:

```json
{
  "api_version": "envy/v1",
  "project": {
    "id": "shop",
    "name": "Tea shop"
  },
  "components": [
    {
      "id": "storefront",
      "project": "shop",
      "protocol": "http",
      "port": 8080,
      "profile": "http-small",
      "health_path": "/healthz",
      "readiness_path": "/readyz",
      "overridable": true,
      "env": {
        "SHOP_ROLE": "storefront",
        "DOWNSTREAM_URL": "http://pricing.envy-shop.svc.cluster.local:8080/products"
      }
    },
    {
      "id": "pricing",
      "project": "shop",
      "protocol": "http",
      "port": 8080,
      "profile": "http-small",
      "health_path": "/healthz",
      "readiness_path": "/readyz",
      "overridable": true,
      "env": {
        "SHOP_ROLE": "pricing"
      }
    }
  ],
  "baseline": {
    "id": "staging",
    "project": "shop",
    "revision": "shop-v1",
    "endpoint": "http://shop.envy.localhost:8080",
    "routing": {
      "namespace": "envy-shop",
      "gateway": "envy-preview",
      "entry_component": "storefront"
    },
    "verification": {
      "kind": "http",
      "path": "/products",
      "expected_status": 200
    },
    "components": {
      "storefront": {
        "service_host": "storefront.envy-shop.svc.cluster.local",
        "port": 8080,
        "image": "envy/shop:v1"
      },
      "pricing": {
        "service_host": "pricing.envy-shop.svc.cluster.local",
        "port": 8080,
        "image": "envy/shop:v1"
      }
    }
  }
}
```

### Manifest Fields

- **`project`**:
  - `id`: Unique project identifier (lowercase alphanumeric and hyphens).
  - `name`: Human-readable project name.
- **`components`**:
  - Array of approved microservice profiles that can be overridden in compositions.
  - `id`: Component identifier used by overrides.
  - `project`: Owning project ID.
  - `port`: Service port for HTTP mesh communication.
  - `protocol`: Transport protocol (`http`).
  - `profile`: Provider workload profile.
  - `health_path` and `readiness_path`: Probe paths used for workload checks.
  - `overridable`: Whether compositions may replace this component.
- **`baseline`**:
  - `id`: Registered baseline identifier. It can represent any approved deployed reference environment; `staging` is only the local demo's identifier.
  - `project`: Owning project ID.
  - `revision`: Immutable revision of the registered baseline bindings; it does not freeze the deployed workload or identify the current Git commit.
  - `endpoint`: Public baseline endpoint.
  - `routing`: Namespace, gateway, and entry component used for ingress.
  - `components`: Runtime service host, port, and image bindings.
  - `verification`: Readiness contract with `kind` set to `envy-chain` or `http`.

---

## Step 1: Validate Without Side Effects

Run `envy catalog validate` against your manifest. This performs dry-run checks against live Kubernetes and mesh infrastructure without performing any database writes:

```bash
envy catalog validate --file application.json
```

**Validation Checks**:

- Verifies the manifest shape and immutable catalog identifiers.
- Checks baseline and component reachability through the configured provider.
- Normalizes the verification contract and reports limitations or warnings.
- Does not create or adopt workloads.

---

## Step 2: Apply the Catalog Atomically

Once validation succeeds, register the project, components, and baseline:

```bash
envy catalog apply --file application.json
```

The command registers the entire bundle transactionally:

- Registrations are immutable and project-scoped.
- Identical retries are idempotent and return HTTP 200.
- Conflicting updates with changed component profiles will return a 409 error.

---

## Step 3: Verification Contracts

Envy supports two verification contracts before marking a composition `ready`:

### 1. `envy-chain` (instrumented demo responses)

The service returns a `chain` array with one hop per component:

```json
{
  "chain": [
    {
      "service": "storefront",
      "version": "v1",
      "composition": "cmp-8f12",
      "workload_id": "cmp-8f12-storefront",
      "deployment_composition": "cmp-8f12"
    }
  ]
}
```

The chain is useful application evidence, but the verification contract is
defined by the catalog entry and its declared chain. The `http` contract does
not require an `x-envy-route` header.

### 2. `http` (ordinary HTTP applications)

Envy dials the verification path through both the baseline and composition
ingress hosts and requires the declared 2xx status. It checks reachability and
status only; an `x-envy-route` response marker is not proof of downstream
selection.

---

## Multi-Service Overrides

Once onboarded, developers and agents can override up to three services simultaneously using repeated `--override` flags:

```bash
envy composition create \
  --project shop \
  --baseline staging \
  --name shop-preview \
  --override storefront=registry.internal/shop/storefront:v2 \
  --override pricing=registry.internal/shop/pricing:v2
```
