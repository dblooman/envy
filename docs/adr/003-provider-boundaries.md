# ADR 003: Provider boundaries

Status: Accepted

Date: 2026-09-05

## Decision

Use narrow internal runtime and routing interfaces and ordinary constructor injection. Kubernetes types remain in provider implementations. Compile complete routing-domain snapshots.

## Alternatives

A public plugin framework or universal infrastructure interface would speculate about unimplemented providers. Passing Kubernetes API objects through the domain would couple the control plane to its first runtime.

## Consequences

Runtime Ensure/Observe/Delete and routing Reconcile are sufficient for the first slice. Bounded logs will be a separate optional capability. Resource and validation interfaces are documented conceptually and created only alongside a real implementation.

## Revisit conditions

Extend interfaces when a concrete second provider requires a capability; validate routing and connectivity compatibility before accepting compositions.
