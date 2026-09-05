# ADR 005: Composition context

Status: Accepted

Date: 2026-09-05

## Decision

Carry a canonical globally unique composition ID in W3C baggage. Configure TraceContext and Baggage propagation explicitly and instrument inbound/outbound HTTP with OpenTelemetry.

## Alternatives

A proprietary context header is simpler but loses standard propagation support. Deriving composition identity from a workload environment variable fails for inherited shared services.

## Consequences

Preview ingress replaces external baggage; baseline ingress removes it. Each hop reports request-observed identity. The mesh regex recognizes exact baggage-member boundaries, whitespace, and member properties. A single unambiguous composition member is required. Baggage does not authorize access.

## Revisit conditions

Revisit ingress normalization when authenticated callers need external baggage preservation. Async support must solve worker selection as well as propagation.
