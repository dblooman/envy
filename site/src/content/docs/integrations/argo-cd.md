---
title: Argo CD & Deployment Previews
description: Keep Argo CD deploying main while Envy runs selected images using approved configuration from staging.
---

Keep Argo CD managing staging and ask Envy: **“Run staging’s pricing service with this new image.”** Envy discovers the existing Deployment’s configuration and, after approval, creates a separate preview Deployment. Main continues through your normal delivery pipeline.

This integration is opt-in. Its first release supports stateless HTTP Deployments with one application container and platform-injected Istio. Existing `http-small` profiles keep their behavior.

## Who owns what?

| Owner                                      | Responsibility                                                                                                |
| ------------------------------------------ | ------------------------------------------------------------------------------------------------------------- |
| Argo CD or your existing delivery pipeline | Baseline Deployments, Services, configuration, and promotion of main                                          |
| Platform team                              | Kubernetes, Istio, shared Gateway, DNS/TLS, and installation permissions                                      |
| Envy                                       | Composition namespaces, preview Deployments and Services, approved configuration copies, routing, and cleanup |
| CI                                         | Build and publish immutable images, request previews, run application tests, and destroy disposable previews  |

A pricing preview runs beside staging. Requests through its preview URL select the new pricing image; inherited services still use the registered staging Services. Applications must [propagate request context](/reference/routing/#context-propagation) for downstream overrides to work.

Keep Envy-generated resources out of the baseline’s Git manifests and Argo tracking configuration. Give each Kubernetes object one owner. The repository includes [separate Applications and AppProjects](https://github.com/dblooman/envy/tree/main/integrations/argocd) for platform resources, the baseline, and the Envy installation.

## Onboard an existing service

### 1. Register the baseline and enable previews

Start with [catalog onboarding](/getting-started/onboarding/). Register the existing Service identities and ingress, then enable `preview.enabled: true` in the Envy Helm values.

For a new component that should inherit its deployed configuration, use this component entry in your catalog manifest:

```json
{
  "id": "pricing",
  "project": "shop",
  "protocol": "http",
  "port": 8080,
  "profile": "deployment",
  "overridable": true
}
```

Use the baseline Service port. Omit `health_path`, `readiness_path`, `env`, and `image_pull_secrets`; discovery obtains those settings from the Deployment. Registering this component does not authorize previews until approval succeeds.

Existing `http-small` components can acquire an approved deployment-derived profile explicitly. That approval changes new compositions; existing compositions retain their captured configuration.

### 2. Discover and review

With your delivery CLI configured to access Envy:

```sh
delivery preview-profile discover --project shop --baseline staging \
  --component pricing > discovery.json
```

Envy reads the Deployment’s Pod template, rather than copying a running Pod. The report describes supported container configuration, dependency references, proposed transformations, required source-read permissions, and blockers. Secret values are never returned.

If the Service selects multiple Deployments, pass an explicit selection with `--file selection.json`. Envy verifies that the selected Deployment serves the registered Service:

```json
{
  "deployment": "pricing",
  "container": "app"
}
```

Review the report’s `source_read_rules`. An operator grants those named `get` permissions through a Role in the source namespace and a RoleBinding to Envy’s controller service account. Repeat discovery after granting access. Enabling previews does not grant cluster-wide Secret reads; see the [source RBAC example](https://github.com/dblooman/envy/blob/main/docs/deployment-derived-previews.md#one-time-onboarding).

### 3. Resolve namespace-dependent configuration

The preview has its own namespace. A short address such as `checkout` may no longer reach staging. Review service addresses, service-link environment assumptions, external dependencies, and shared side effects before approving.

You can supply literal environment replacements and ConfigMap text-key replacements during discovery:

```json
{
  "deployment": "pricing",
  "container": "app",
  "env": {
    "CHECKOUT_URL": "http://checkout.shop-staging.svc.cluster.local:8080"
  },
  "config_map_keys": {
    "pricing-settings": {
      "checkout-url": "http://checkout.shop-staging.svc.cluster.local:8080"
    }
  }
}
```

Save this as `selection.json` and repeat discovery with `--file selection.json`. Replacements must name existing literal variables or existing text keys in referenced ConfigMaps. These values are public configuration: do not put credentials in them. Envy does not infer addresses from Secrets or rewrite arbitrary configuration files.

### 4. Approve the inspected configuration

After resolving blockers and confirming connectivity, create `approval.json` using the exact `selection` and `inspection` returned by your latest discovery:

```json
{
  "selection": {
    "deployment": "pricing",
    "container": "app"
  },
  "inspection": "REPLACE_WITH_RETURNED_INSPECTION",
  "expected_revision": 0,
  "confirm_connectivity": true
}
```

Include your reviewed overrides in `selection` if you supplied them. Use revision `0` for first approval; use the current approved revision when reapproving.

```sh
delivery preview-profile approve --project shop --baseline staging \
  --component pricing --file approval.json

delivery preview-profile inspect --project shop --baseline staging \
  --component pricing
```

Approval rechecks the source and rejects stale inspections. It saves an audited profile scoped to project, baseline, and component. REST and MCP expose the same discover, approve, and inspect operations; callers need no Kubernetes credentials or local Helm checkout.

## Run a preview from your pipeline

Build and push your candidate image through the existing CI job, then create a composition:

```sh
delivery create --project shop --baseline staging --name pricing-pr-123 \
  --override pricing=registry.example/shop/pricing@sha256:REPLACE_WITH_64_HEX_DIGEST \
  --expected-preview-revision pricing=1 --ttl 1h
```

The optional revision guard catches an unexpected approval change. You can also select a published build ID through Envy’s build-selection interfaces.

Envy creates a separate Deployment and Service with unique selectors, one application replica, and an unprivileged service account without an API token. It copies approved ConfigMaps, application Secrets, and registry credentials into component-scoped names and lets the preview namespace inject Istio afresh. Source ownership and tracking metadata are removed.

The [GitHub Actions integration](/integrations/github-actions/#run-tests-against-a-preview) provides two caller examples:

- **Disposable CI tests:** build/report, check baseline health, create, wait for the accepted generation, test, and destroy.
- **Retained previews:** an explicit request creates or updates a preview and leaves it available until deletion or TTL expiry.

Test downstream selection and application behavior, not just a successful HTTP response. Readiness is not a lock: coordinate writers when several people or jobs share a retained preview. After merge, Argo CD promotes main through the normal pipeline; it does not need to deploy the branch into staging for preview tests.

## What happens when main changes?

| Event                                                                       | Existing preview                                                               | New preview                                                        |
| --------------------------------------------------------------------------- | ------------------------------------------------------------------------------ | ------------------------------------------------------------------ |
| Main changes its image, probes, bounded resources, or configuration values  | Keeps its captured override template and configuration copies                  | Adopts current configuration if it satisfies the approved contract |
| A dependency reference, workload identity, or execution requirement changes | Keeps its captured configuration                                               | Requires review and approval again                                 |
| A Secret rotates                                                            | Keeps its copied Secret; credentials can still expire or be revoked externally | Captures the current approved dependency version                   |
| The preview image is updated                                                | Keeps its captured template and copies, using the newly selected image         | Not applicable                                                     |

Recreate a preview to adopt newer configuration. If a copy disappears, Envy can reconstruct it only while the recorded source version remains available; otherwise provisioning reports a dependency failure. An intervening source change during copying also requires recreation.

Three different concepts appear in composition metadata:

- **Baseline binding revision:** identifies registered routing bindings. It does not freeze main or identify its current Git commit.
- **Deployment provenance:** records the selected preview-profile revision and source Deployment identity/version used for an override.
- **Shared state:** inherited services, databases, caches, and application side effects remain live. Configuration copies do not clone a full environment.

## Current boundaries

Supported configuration includes environment references, commands, arguments, ports, probes, resources, supported security settings, and configuration mounts. Workloads must satisfy installation policy, including non-root execution, explicit `allowPrivilegeEscalation: false`, and positive resource requests and limits. Quotas include rollout allowance and configured mesh overhead.

Persistent volumes, application init containers or sidecars, host access, application Kubernetes/cloud identity, and unsupported Pod settings block onboarding. Pod-template annotations currently require a separate integration and are rejected. This is not general Helm-chart compatibility.

The [disposable acceptance scenario](https://github.com/dblooman/envy/blob/main/docs/deployment-derived-previews.md#acceptance) covers Argo synchronization, two concurrent image previews, configuration rotation, controller restart, workload recovery, and TTL cleanup while baseline traffic remains available. Hosted GitHub Actions execution has not been validated as part of that scenario.
