---
title: Onboard an Application
description: How to register microservice projects, approved component profiles, and baselines into Envy's catalog.
---

Envy uses declarative catalog profiles to define which components belong to a
project, which components may be overridden, and how HTTP traffic or
endpoint-free execution is verified from a deployed reference baseline.

Preparation drafts and server-side preview plans were merged after the v0.8.0
release. Use a newer source build until a release includes them; older servers
report these optional steps as unavailable.

---

Before registering a catalog, deploy the baseline application and its configured mesh and ingress, configure Envy API access, and ensure services forward W3C Baggage. Catalog registration does not deploy the baseline. The complete local shop setup is available with `make dev-shop` after the [quickstart](/getting-started/quickstart/).

For an evaluation or production cluster, [install Envy with Helm and open its dashboard](/getting-started/installation/) first. The terminal examples below use the optional [Envy CLI](/reference/cli-installation/); CLI setup is separate from installing the server.

For an existing Argo-managed application, use [deployment-derived onboarding](/integrations/argo-cd/#onboard-an-existing-service) to reuse approved Deployment configuration. The example below uses the legacy `http-small` profile with explicit settings.

## Guided dashboard onboarding

In a connected dashboard, choose **Catalog & baselines → Onboard application**.
The flow is intended for a platform engineer who knows the existing Services and
ingress and can arrange required permissions. Discovery inspects named,
approved resources; it does not offer unrestricted cluster browsing or install
infrastructure.

1. Describe the project and complete baseline: Service names and ports, baseline
   images, namespace, Gateway, entry component, and verification settings. HTTP
   verification defaults to `/` and status `200`; select an application path
   that returns the expected status. Instrumented applications can select an
   ordered Envy chain instead.
2. Choose each workload profile. Deployment-derived profiles obtain configuration
   during preparation. Manual `http-small` profiles collect health/readiness
   paths, literal environment settings, and approved registry Secret names.
   Never enter credentials in catalog-visible values.
3. Validate, review the returned checks, and explicitly **Register baseline**.
   This atomically saves immutable catalog entries without creating a preview.
   Matching entries are reused; conflicting changes require correcting the
   submitted configuration, not overwriting the saved catalog.
4. Prepare deployment-derived services: discover configuration, review blockers
   and named source-read rules, arrange permissions externally, and retry.
   Enter supported literal environment or ConfigMap-key replacements as needed.
   Confirm connectivity and shared side effects, then approve the inspected
   configuration. Edits or source/revision conflicts require fresh discovery and
   another review. Manual profiles need no additional preparation.
5. Continue to the existing preview creation screen with the baseline selected.
   Choose a published build or image and review the lifetime before **Create
   preview**. Deployment-derived overrides require an approved profile and an
   immutable image digest or published build. The request guards the reviewed
   profile revisions. Pending services can stay inherited.

During the first two steps, use **Save preparation** to keep a draft and **Load
saved preparation** to resume it after reloading. Drafts are scoped to the
installation, project, and authenticated author; installations using a shared
token have a shared draft author. Environment values, selected ConfigMap values,
credentials, and messaging configuration are not saved. Re-enter them before
validation. Saving an outdated draft revision returns a conflict, so reload and
review the current revision before saving again. **Discard saved preparation**
removes the draft; saving alone never registers a baseline or approves a profile.

Registered baselines can be reopened through **Prepare overrides** on their
Catalog card. Registration and completed approvals survive reloads, while
unsaved preparation edits do not. **Advanced registration** retains the JSON
editor. Demo mode cannot onboard a real application. Older installations that do
not support drafts report them as unavailable; continue with the existing
registration flow.

Before creating a preview, review the server plan in the creation screen. It
shows selected images and approved profile revisions, inherited components,
declared dependencies, destination, lifetime, an upper-bound resource estimate,
and blockers with next actions. Correct blockers and refresh the plan after
changing a selection. Planning creates no resources and reserves no capacity;
creation checks the current catalog, approvals, and capacity again. Developers
can reuse the operator's approved profiles without repeating registration.

Read the composition's actual verification level after creation: HTTP
reachability is not proof of downstream routing, and Job completion is not
proof of application correctness. Run application checks appropriate to the
selected [workload type](/guides/workload-types/).

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
  - `port`, `protocol`, `health_path`, and `readiness_path`: Required for HTTP profiles; omitted for endpoint-free Jobs.
  - `profile`: Provider workload profile. See [Workload Types](/guides/workload-types/) for Job and scheduled-Job settings.
  - `overridable`: Whether compositions may replace this component.
- **`baseline`**:
  - `id`: Registered baseline identifier. It can represent any approved deployed reference environment; `staging` is only the local demo's identifier.
  - `project`: Owning project ID.
  - `revision`: Immutable revision of the registered baseline bindings; it does not freeze the deployed workload or identify the current Git commit.
  - `endpoint`: Public baseline endpoint for HTTP baselines; omitted for Job-only baselines.
  - `routing`: Namespace, gateway, and entry component used for ingress; Job-only baselines need only the namespace.
  - `components`: Runtime service host, port, and image bindings.
  - `verification`: Readiness contract with `kind` set to `envy-chain`, `http`, or `none` for endpoint-free Job-only baselines.

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

## Enable PR previews

After a smoke preview passes application checks and cleanup, continue with
[GitHub App & PR Previews](/integrations/github-app/). Register your source
repository and approved image mappings, configure build reporting, and enable a
policy before adding `envy-preview` to a same-repository PR.

For asynchronous applications, complete [Google Pub/Sub onboarding](/guides/pubsub-isolation/)
before requesting message isolation.
