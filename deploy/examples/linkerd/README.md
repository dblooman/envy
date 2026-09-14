# Linkerd profile — blocked

These examples prepare the implemented adapter with Envoy Gateway. They are not
a supported installation path yet: pinned Linkerd edge-26.9.1 omits
`observedGeneration` in HTTPRoute conditions. Envy deliberately keeps compositions
unready until the expected controller reports current-generation acceptance.
See `docs/mesh-installation.md` and the shared test-version manifest. Do not bypass
this gate or count API-resource tests as evidence of working routing.
