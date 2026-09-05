# ADR 004: Request routing

Status: Accepted

Date: 2026-09-05

## Decision

Use Istio sidecars, separate override Services, one aggregate mesh VirtualService per logical baseline host, and an exact-host ingress VirtualService per composition.

## Alternatives

Per-composition mesh VirtualServices compete for the same host and do not provide a supported sidecar merge model. DestinationRule subsets need shared service membership. Ambient adds waypoint requirements; an Envoy extension is unnecessary.

## Consequences

One writer compiles all composition matches in deterministic order with baseline last. Ownership conflicts are errors. Ingress explicitly targets the resolved gateway; setting baggage does not trigger another routing pass. Proxy convergence is verified through real requests.

## Revisit conditions

Revisit Gateway API or another routing provider when its concrete deployment requirements and support match the product. Measure route-table growth before raising the default twenty-live-composition cap.
