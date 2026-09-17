---
title: Choose Your Mesh
description: Install Envy with an existing Istio, Cilium, or Linkerd mesh.
---

# Choose an existing mesh

Start with [Install a Release](/getting-started/installation/) for CLI downloads and the team adoption path. This page covers mesh-specific prerequisites and verification.

Envy has three installation profiles: `istio`, `cilium`, and `linkerd`.

Linkerd is supported with its pinned controller, which omits `observedGeneration`
on Service-attached HTTPRoute conditions. Envy uses immutable, content-addressed
producer routes for Linkerd and accepts generation-less conditions only for a fresh,
owned generation-one route. Envoy Gateway ingress remains generation-checked.

Cilium and Linkerd do not require Istio, its CRDs, or its ingress gateway.
The chart installs Envy only. Operators own the mesh, Gateway API CRDs,
ingress controllers, PostgreSQL, authentication, DNS, certificates, and baselines.

The pinned fixture versions and acceptance state are recorded in
[versions.json](https://github.com/dblooman/envy/blob/main/deploy/testing/versions.json). A configured profile is not a
claim that every controller version or cluster configuration has passed acceptance.

| Profile | Application routing                              | Preview ingress           | Baseline preparation                                          |
| ------- | ------------------------------------------------ | ------------------------- | ------------------------------------------------------------- |
| Istio   | Istio sidecar VirtualServices                    | Istio Gateway             | Ready Istio proxies; configured injection revision            |
| Cilium  | Gateway API producer HTTPRoutes, Cilium GAMMA    | Cilium Gateway controller | Cilium-managed endpoints, kube-proxy replacement and L7 proxy |
| Linkerd | Gateway API producer HTTPRoutes, Linkerd proxies | Envoy Gateway             | Ready Linkerd proxies in baseline callers and overrides       |

<!-- prettier-ignore-start -->

<!-- mesh-versions:start -->
| Profile | Kubernetes | Mesh | Gateway API | Ingress | Acceptance |
| --- | --- | --- | --- | --- | --- |
| istio | 1.36.4 | 1.31.0 | — | Istio 1.31.0 | HTTPS traffic and legacy upgrade passed |
| cilium | 1.36.4 | 1.20.1 | 1.6.2 | Cilium 1.20.1 | HTTPS traffic passed |
| linkerd | 1.36.4 | edge-26.9.1 | 1.6.2 | Envoy Gateway 1.8.4 | HTTPS traffic passed with immutable producer routes |
<!-- mesh-versions:end -->

<!-- prettier-ignore-end -->

## 1. Prerequisites

All profiles require HTTP Services with named application containers, ready pods,
and applications that propagate W3C baggage between downstream requests. A mesh
cannot copy context between separate application requests. Direct Pod-IP calls,
application-encrypted service traffic, asynchronous message routing, and Istio
ambient mode are outside this contract.

For **Istio**, retain your injection revision and ingress selector. The HTTPS
Gateway uses SIMPLE termination; its certificate credential must be available
to the ingress workload. Envy preserves existing Istio defaults and route names.

For **Cilium**, enable `kubeProxyReplacement`, `l7Proxy`, and `gatewayAPI.enabled`.
Install the compatible Gateway API CRDs before the controller. Provide a working
LoadBalancer/Service ingress exposure. Host-network Gateway exposure is rejected: the pinned Cilium version can assign colliding GAMMA listener ports across services. Envy currently
checks the conventional `kube-system/cilium-config`, `cilium` DaemonSet, and
ready CiliumEndpoint records for baseline pods.
See [Cilium GAMMA prerequisites](https://docs.cilium.io/en/stable/network/servicemesh/gateway-api/gamma/).

For **Linkerd**, install its control plane and inject/restart baseline workloads
before registration. Envy injects its own override workloads automatically, with
proxy requests of 100m CPU/64Mi memory and limits of 1 CPU/256Mi memory
to satisfy composition namespace quotas.
Remove ServiceProfiles for managed Service FQDNs: they take precedence over
HTTPRoutes, including profiles in caller namespaces. Install Envoy Gateway
separately and use its GatewayClass (normally `eg`). A Linkerd GatewayClass is
not an ingress controller. Other Gateway API ingress controllers are not in the
acceptance matrix. Any alternative would need HTTPS termination, Gateway/listener
status, current-generation HTTPRoute acceptance, cross-namespace backend references,
and request/response header modifiers with the same baggage behavior. It must pass
the full traffic contract before support is claimed. See [Linkerd HTTPRoute behavior](https://linkerd.io/docs/reference/httproute/).

All profiles need wildcard DNS and certificates covering `*.preview.example.com`.
Preview ingress replaces untrusted baggage; the baseline ingress removes it.
Allow ingress-to-entry and baseline-to-override network traffic, mesh control-plane
connectivity and DNS. Account for Cilium ingress identities or Linkerd authorization
policies where enabled. Envy does not rewrite operator security policies.

## 2. Prepare the profile examples

Use `deploy/examples/istio`, `deploy/examples/cilium`, or
`deploy/examples/linkerd`. Each contains Helm values, a baseline workload template, Gateway and baseline-route
manifests, a catalog, and an installation-check specification. JSON is accepted
by both Helm and kubectl.

Replace the example domains, Service names/images, proxy CIDRs, and database and
certificate Secret references. Create the baseline workloads and `staging`
namespace first. For Gateway API, create `preview-tls` in the Gateway namespace.
For Istio, place the credential where the ingress workload resolves it.
Apply the Gateway and exact-host baseline route after adapting them.

Set `runtime.ingressURL` to the actual reachable ingress Service or address.
Cilium and Linkerd require it explicitly; do not use the Envy API Service.
Discover Envoy Gateway's generated data-plane Service with its
`gateway.envoyproxy.io/owning-gateway-name` label. The server connects to this
internal address while using the public hostname for Host and TLS verification.
For private CAs, reference a PEM bundle with `runtime.caConfigMap`.

A Gateway may live in another namespace: set baseline `routing.gateway_namespace`
and configure its listener's `allowedRoutes` for the baseline namespace. Optional
`routing.gateway_section_name` selects a Gateway API listener. These fields do
not move baseline Services or workload namespaces. Istio does not accept a
Gateway API listener section.

## 3. Preflight and install Envy

```sh
# Install the matching released CLI before running preflight.
envy installation check --file deploy/examples/cilium/installation.json
helm upgrade --install envy oci://registry-1.docker.io/davey/envy-chart \
  --version 0.3.0 --namespace envy-system --create-namespace \
  --values deploy/examples/cilium/values.json
kubectl -n envy-system rollout status deployment/envy-envy
```

Substitute your chosen profile directory. The OCI chart defaults to the matching
`davey/envy:0.3.0` image, so this path does not build Envy from source. Pin the
chart version and install the matching CLI release for production. Installation check is read-only; exit 1
means a failure and exit 2 means incomplete evidence. The optional `catalog`
field checks baseline participation and routing for the selected provider before
installation. Controller-to-database/ingress reachability remains unknown until
tested from a pod; enable the chart's `preflight.enabled` connectivity Job.
An HTTP health probe proves reachability, not downstream override selection.

The chart supplies only the selected provider's RBAC. It does not install or
upgrade Gateway API or mesh CRDs. Keep the server and chart versions together.

## 4. Register, create, verify, destroy

Configure the CLI API URL and credential, then run the same workflow for each mesh:

```sh
envy catalog validate --file deploy/examples/cilium/catalog.json
envy catalog apply --file deploy/examples/cilium/catalog.json
envy composition create --project example --baseline staging --name smoke \
  --override api=registry.example.com/api:preview
```

Use the returned composition ID to inspect status/endpoints and destroy the
composition using the standard CLI or REST API. Verify the actual application
response through the preview hostname and check that the baseline response stays
unchanged. Use an `envy-chain` verification contract for instrumented demo services;
the example's `http` contract only checks application health. Applications need
their own assertions to establish deeper behavior.

## Troubleshooting and upgrades

Route status reports the responsible controller and its `Accepted`/`ResolvedRefs`
conditions at the current generation. A pending controller does not make a
composition ready. Check Gateway/listener conditions, backend Services, restricted
ReferenceGrants, network policy, and baggage propagation before retrying.

The database records the installation ID and selected mesh. Switching either on
the same database is rejected. Existing populated databases without the record
are treated as Istio installations. Generic `gateway-api`, `nativeCEC`, and manual
Linkerd-injection flags are retired: drain experimental compositions with the
previous server, then recreate their catalog in a fresh installation. Removing a
configuration field does not safely migrate live routes between controllers.

## Reproducible development checks

`make test-mesh MESH=cilium` (or `istio`, `linkerd`) creates a disposable cluster,
installs the pinned prerequisites, and exercises real traffic. These scripts are
separate from production installation. They refuse an existing cluster by default.
Ordinary `go test ./...` never selects a developer's Kubernetes context.
