# ADR 001: Product boundary

Status: Accepted

Date: 2026-09-05

## Decision

Envy composes external infrastructure through APIs. Callers own builds, source control, coding agents, and test orchestration.

## Alternatives

A combined CI/agent platform could own the full delivery lifecycle, but would couple composition to particular tools and substantially expand the product.

## Consequences

The create interface accepts prebuilt images. Source-repository provenance may be recorded as metadata without defining composition identity. No agent runtime, build service, or test execution service is introduced.

## Revisit conditions

Revisit only when repeated external integrations establish a concrete missing composition capability; keep build and agent execution external.
