# Linkerd profile

These examples prepare Linkerd with Envoy Gateway preview ingress. Pinned Linkerd
edge-26.9.1 omits `observedGeneration` in producer HTTPRoute conditions, so Envy
uses immutable, content-addressed producer routes and accepts conditions only on
fresh owned objects. See `docs/mesh-installation.md` and the shared test-version
manifest. The real-controller traffic suite remains the acceptance evidence.
