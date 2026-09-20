# Kubernetes orchestration and preview connectivity

PostgreSQL remains the source of desired state. Envy watches Kubernetes execution
state and uses a deduplicating work queue keyed by project and baseline. Four
baseline domains can progress concurrently; changes to aggregate mesh routing
remain serialized and use fresh database intent. PostgreSQL notifications wake
the queue after committed changes. A 30-second recovery scan repairs lost
notifications, and expiry is checked every second. Healthy previews are refreshed
every 30 seconds; provisioning and cleanup retain the configured reconciliation
interval. Persisted failure backoff survives process restarts.

Informer caches must synchronize before work starts. They supply observations,
not mutation authorization: mutations still check live resource ownership,
resource versions, and the leadership guard. Workload references include the
expected image and Deployment generation so a stale cache cannot verify a
previous rollout. Losing leadership cancels the worker session and watches.

## Review order

1. **RBAC:** Helm and local manifests separate read-only Pods and pod logs from
   controller-managed resources. No workload exec permissions are granted.
2. **Discovery:** `connectivity.go`, preview discovery, and the optional OpenAPI
   report field add reviewed DNS suggestions without changing approval inputs.
3. **Namespace policies:** `namespace_policy.go`, installation migration `014`,
   server configuration, and chart examples define the persisted isolation mode.
4. **Field ownership:** `kubeapply` and the Kubernetes/mesh providers introduce
   guarded, non-forcing apply while retaining normal initial creation.
5. **Reconciliation:** `queue.go`, `watch.go`, and migration `015` add notifications,
   observation caches, domain queues, recovery scans, and cancellation.

## Service addresses when moving namespaces

A call to `http://checkout:8080` resolves in the caller's namespace. In a preview,
that is the preview namespace, not the original baseline namespace. Use the
registered baseline address, such as
`http://checkout.shop-staging.svc.cluster.local:8080`. The mesh still selects the
appropriate override using propagated composition context.

Preview discovery returns optional `connectivity` findings with `location`,
`hostname`, `message`, and, when unambiguous, `replacement`. Review replacements,
put accepted values into the existing `selection.env` or
`selection.config_map_keys`, rediscover, then approve the new inspection token.
Discovery does not silently modify values or establish network reachability.

The heuristic recognizes whole addresses, not arbitrary embedded configuration.
It does not inspect Secret values or suggest credential-bearing URLs. Query
parameters are preserved only for the known noncredential parameters `page`,
`limit`, `offset`, and `sort`; other query-bearing URLs are left for manual review.
Application code, opaque files, credentials, and undeclared dependencies still
require the existing connectivity confirmation. There are no runtime probe pods
or new workload exec permissions.

## Namespace policies

Fresh installation databases default to `isolated`. Upgraded databases retain
`legacy`. The choice is persisted; Helm install/upgrade flags do not select it.
The server's `namespace_policy` configuration is exposed as `namespacePolicy` in
Helm values. An omitted `mode` uses the persisted choice.

An isolated preview receives a NetworkPolicy before its workloads are created.
It permits same-preview and registered-baseline namespace communication. All
other ingress and egress require explicit operator rules. Configure both
`ingress` and `egress` using standard Kubernetes NetworkPolicy rules, including
DNS, mesh control-plane traffic, ingress and control-plane verification sources,
and any approved external dependencies. IP blocks and named/numeric ports are
supported. Envy never installs policies in the baseline namespace.

The mesh examples in `deploy/examples/{istio,cilium,linkerd}/values.json` supply
infrastructure allowances for their documented namespace layouts. Adjust them to
your installation. External dependencies are intentionally absent. Cilium examples explicitly enable `namespacePolicy.cilium_ingress`, adding a
namespaced CiliumNetworkPolicy for the reserved `ingress` identity. Cilium
Gateway traffic cannot be represented by an ordinary namespace selector; this
allowance does not permit direct traffic from other preview pods. See
[Cilium gateway identity](https://docs.cilium.io/en/stable/network/servicemesh/gateway-api/gateway-api/).
Node-local DNS and custom gateway placements require corresponding explicit allowances.
`GET /v1/installation` exposes the effective `namespace_policy_mode` and whether
infrastructure allowances are configured (`namespace_policy_ready`). This is
configuration readiness, not evidence of network enforcement.

The cluster must use a network plugin that enforces NetworkPolicy. Confirm both
allowed and denied traffic in your environment. Policies restrict direct network
access; they do not isolate shared data, prevent requests routed through the
baseline, or turn preview workloads into hostile-tenant sandboxes.

To change mode or policy configuration:

1. Destroy active previews and wait for completed cleanup.
2. Stop all Envy server replicas.
3. Update configuration and start the new server version.

Configuration changes are fingerprinted. A running reconciler or undrained
preview prevents the transition. A stale server cannot mutate using a previous
policy configuration. An unchanged upgrade does not require draining.

Namespaces default to Pod Security Admission `restricted` warning and audit,
pinned to `v1.36`. Set `pod_security.enforce` only after verifying your mesh's
injected containers and init containers satisfy that level. Select a supported
pinned version for your cluster. Conflicting existing pod-security labels are
reported rather than overwritten; unrelated namespace labels are preserved.

## Kubernetes field ownership

Envy creates new resources normally and uses Server-Side Apply for subsequent
mutable changes, with managers `envy-runtime` and `envy-routing`. It never forces
conflicts. `apply_conflict` diagnostics retain the API's conflicting fields and
manager information. Resolve ownership with the other controller and retry.

On first apply, Envy transfers its own Update field entries to Apply using UID
and resource-version checked patches. The historical released `envy-server`
manager is recognized for upgrades. Entries belonging to other managers remain
untouched. Installations that previously used a custom executable/field-manager
name may need an operator-reviewed ownership migration if conflicts occur.
Immutable copied Secrets and ConfigMaps keep their existing creation and identity
checks. Baseline objects remain externally managed.

## Validation

Run the normal Go suite with `ENVY_TEST_DATABASE_URL` pointing at a disposable
PostgreSQL database, plus race checks for reconciliation and providers. Chart
checks render all three profiles. `TestOrchestrationApplyResources` runs only
with `ENVY_TEST_ORCHESTRATION=1` and an explicit `ENVY_KUBE_CONTEXT` beginning
`kind-envy-test-`; it validates live SSA migration, owned-field removal, preservation
of another manager's fields, no-op writes, and conflicts.

Full mesh acceptance remains available through `make test-mesh MESH=...`.
These tests use explicit policy allowances. Network denial tests additionally
require an enforcing network plugin; a kind cluster using kindnet alone does not
prove isolation. No controller metrics endpoint is introduced.

For enforcing-CNI acceptance with Istio or Linkerd, set
`ENVY_TEST_ENFORCE_POLICY=1` when running `deploy/testing/e2e.sh`. The disposable
fixture installs Cilium with pod socket load balancing disabled, as required by
[Istio integration](https://docs.cilium.io/en/stable/network/servicemesh/istio/)
and [Linkerd cluster configuration](https://linkerd.io/docs/reference/cluster-configuration/).
When the Istio upgrade fixture is enabled, it first asserts that the upgrade
retains legacy mode, then drains and stops Envy before explicitly switching to
isolated mode for connectivity acceptance.
The mesh runner also runs `TestOrchestrationNetworkBoundary` with `ENVY_TEST_NETWORK_POLICY=1`
and the same disposable kubeconfig/context; these can also be set for a manual run. Its positive control reaches the
baseline, both previews, and an external HTTPS endpoint; isolated probes must
reach their own preview and baseline but cannot reach another preview or that
external endpoint. The external control requires test-runner internet access.
