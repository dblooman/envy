# ADR 009: Concrete mesh installation profiles

Status: Accepted

Envy selects one immutable mesh profile per metadata database: Istio, Cilium, or
Linkerd. Existing populated databases without a binding are legacy Istio. New
profiles require fresh installations; changing a config string is not a route
migration strategy. The existing Istio adapter and ownership model remain intact.

Gateway API is a shared implementation for Cilium and Linkerd, not a fourth mesh.
Linkerd uses Envoy Gateway for preview ingress. Each composition/service receives a
stable producer HTTPRoute plus a shared fallback per Service. This avoids the
16-rule limit without composition movement between shards. Grants precede route
creation and cover only required backends. Current-generation controller acceptance
precedes application verification and is not itself evidence of correct traffic.

Native CiliumEnvoyConfig authoring is removed. Envy does not install meshes, own
Gateway API CRDs, modify baseline workloads, or implement an application proxy.
Profile conformance is established through pinned real-controller traffic tests.
Cilium host-network exposure is excluded from the tested profile because observed
GAMMA listener-port collisions break multiple overrides on same-port Services.

Linkerd remains blocked: its pinned policy controller omits observedGeneration
on route conditions. Retain strict current-generation checks rather than infer
acceptance from stale status. Envoy Gateway ingress readiness alone does not make
this profile supported; controller conformance and the full traffic suite must pass.
