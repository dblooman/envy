# Deployment-derived previews

Envy can run a registered service with a new image using its deployed Kubernetes
configuration. Argo CD, Helm, or the existing pipeline continues to maintain the
baseline. Envy owns a separate Deployment, Service, namespace, routing, and
approved dependency copies for each composition.

This is opt-in. Existing `http-small` components and existing compositions keep
their behavior. A `deployment` component requires an approved preview profile
before it can be overridden. An existing `http-small` component can also acquire
an approved profile; that affects new compositions only.

## One-time onboarding

1. Install Envy and register the existing project, Services, Gateway and baseline
   through `delivery catalog validate` and `delivery catalog apply`. Set the
   component's `profile` to `deployment`, `protocol` to `http`, and `port` to its
   baseline Service port. Omit `health_path`, `readiness_path`, `env` and
   `image_pull_secrets`: discovery obtains these from the Deployment.
2. Enable `preview.enabled: true` in the Envy Helm values. This permits the
   controller to bind a dependency-access ClusterRole inside owned composition
   namespaces. It does **not** grant cluster-wide Secret reads. The controller is
   trusted to restrict those bindings to its owned namespaces.
3. Discover the component:

   ```sh
   delivery preview-profile discover --project shop --baseline staging \
     --component pricing > discovery.json
   ```

   If a Service matches multiple Deployments, supply a JSON selection using
   `--file selection.json`. The selection can identify `deployment` and
   `container`. Only a single application container is currently supported.
4. Review blockers, configuration and `source_read_rules`. Grant the returned
   rules in a **Role in `source.namespace`**, bound to the Envy control-plane
   ServiceAccount. These are named `get` permissions on dependencies, not
   `list` permissions on all Secrets. Repeat discovery after granting access.
5. Resolve namespace-dependent configuration. Envy does not guess addresses
   inside files or Secret values. For example, discover again with:

   ```json
   {
     "deployment": "pricing",
     "container": "app",
     "env": {"CHECKOUT_URL": "http://checkout.shop-staging.svc.cluster.local:8080"},
     "config_map_keys": {"pricing-settings": {"checkout-url": "http://checkout.shop-staging.svc.cluster.local:8080"}}
   }
   ```

   Environment replacements must name existing literal variables; they cannot
   replace Secret/ConfigMap references. ConfigMap replacements must name existing
   text keys in referenced ConfigMaps. Values in these files are public catalog
   configuration and must not contain credentials.
6. Save an approval containing the exact returned `selection`, `inspection`,
   `expected_revision: 0` for first approval, and `confirm_connectivity: true`.
   The confirmation records that the team has checked dependency addresses,
   service-link environment assumptions, shared side effects, and mesh context
   propagation. Resolve all discovery blockers first.

   ```sh
   delivery preview-profile approve --project shop --baseline staging \
     --component pricing --file approval.json
   delivery preview-profile inspect --project shop --baseline staging --component pricing
   ```

   For later approval, use the current profile revision as `expected_revision`.
   Changed inspection fingerprints and concurrent approvals return conflicts.
   Approval appends a durable revision and an activity event.

A source Role example, populated from discovery:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata: {name: envy-pricing-source, namespace: shop-staging}
rules:
  - apiGroups: [""]
    resources: [configmaps]
    resourceNames: [pricing-settings]
    verbs: [get]
  - apiGroups: [""]
    resources: [secrets]
    resourceNames: [pricing-credentials]
    verbs: [get]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata: {name: envy-pricing-source, namespace: shop-staging}
roleRef: {apiGroup: rbac.authorization.k8s.io, kind: Role, name: envy-pricing-source}
subjects: [{kind: ServiceAccount, name: envy-envy, namespace: envy-system}]
```

## Everyday use

Build outside Envy, then select the published build ID or an immutable image:

```sh
delivery create --project shop --baseline staging --name pricing-task \
  --override pricing=registry.example/shop/pricing@sha256:REPLACE_WITH_64_HEX_DIGEST \
  --expected-preview-revision pricing=1 --ttl 1h
```

REST create/update accept `expected_preview_revisions`, a map from selected
component IDs to approved revision numbers. The guard is optional. Responses
include `preview_profiles` with captured revision and source Deployment UID,
resource version and generation. Updates compare the captured revision and keep
the captured configuration; recreation adopts newer approved configuration.

REST onboarding is scoped under
`/v1/projects/{project}/baselines/{baseline}/components/{component}/preview-profile`:
`POST /discover`, `POST /approve`, and `GET` to inspect. The corresponding MCP
tools are `discover_preview_profile`, `approve_preview_profile`, and
`inspect_preview_profile`. All use the same application service and existing
catalog authentication boundary. No local kubeconfig or chart checkout is needed
for callers.

## What is supported

A completed, ready Deployment rollout with one application container, ordinary
HTTP Service routing and platform Istio injection. Supported settings include
arguments, command, environment references, probes, resource requests/limits,
non-root security configuration, Secret/ConfigMap mounts, emptyDir, downwardAPI,
and approved registry Secrets. The controller renames the application container
to the component ID, removes tracking metadata, disables service-link injection,
and uses a preview service account without an API token.

Persistent storage, host access, application init containers and sidecars,
application service-account/cloud identity, custom scheduling and unsupported
Pod settings are blockers. Pod-template annotations currently require a separate
integration and are rejected. A chart's top-level Argo tracking metadata is not
copied. Merely copying YAML does not establish support for an external operator.

The default application maxima are 2 CPU and 2Gi memory, with explicit positive
requests and limits required. Helm exposes `preview.maxCPU`, `maxMemory` and
`meshRequestCPU`, `meshRequestMemory`, `meshLimitCPU`, `meshLimitMemory`. Defaults
reserve 100m/128Mi requests and 2 CPU/1Gi limits per injected proxy. Set these to
cover the installation's actual injection configuration. Quotas account for two
application replicas and their proxies during rolling updates. Source execution
settings that exceed policy are rejected, never silently clamped.

## Configuration and recovery

New compositions inspect the current Deployment. Image, literal value, probe,
and bounded resource changes may be adopted under the existing contract. Changes
to dependency references, command, identity or other execution requirements need
another approval. Configuration copies remain immutable for each composition.

The stored plan contains the template, approved transformations, source identities
and dependency resource versions. The reconciler reads every missing dependency
at its recorded version before creating copies. A version race causes a visible
provisioning failure requiring recreation; this is not an atomic cluster snapshot.

Secret payloads exist only in Kubernetes and transient controller memory. They
are absent from PostgreSQL, API discovery output, logs, CLI output and artifacts.
If a copy disappears, Envy can recover it only while that source version remains
available. Rotation or replacement of the source otherwise requires recreation.
Existing copies survive rotation, image updates and Envy restarts. Credentials
can still expire or be revoked externally.

Inherited deployments and databases remain live. `baseline_revision` identifies
registered bindings; it does not identify the current Git commit or freeze main.
The captured Deployment provenance describes only an overridden workload's
configuration source. HTTP readiness proves reachability; application tests must
prove downstream selection and expected behavior.

## Acceptance

`make test-derived-e2e` creates a disposable `envy-derived-*` kind cluster, installs
Istio and Argo CD 3.1.0, serves a local fixture Git repository, and checks two
simultaneous previews, actual chain routing, a main/configuration/Secret update,
new-preview adoption, image updates, controller restart, drift recovery, TTL
cleanup, and absence of cluster-wide Secret read permission. Its Git repository
contains synthetic credentials only. The cluster is deleted on exit unless
`ENVY_E2E_KEEP_CLUSTER=1`. Logs remain in `.envy/<cluster>/`.
