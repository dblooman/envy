# ADR 007: Live inheritance and sharing

Status: Accepted

Date: 2026-09-05

## Decision

A composition inherits live baseline deployments. The baseline binding revision records selected logical endpoints, not a frozen environment. The initial guarantee is request routing isolation.

## Alternatives

Cloning an entire environment offers greater resource isolation but defeats the selective-composition cost model. Freezing inherited deployments would require ownership and rollout coordination beyond the initial scope.

## Consequences

Databases, caches, credentials, side effects, and inherited workloads stay shared. Namespaces are ownership boundaries, not an enforced multi-tenant security boundary. Resource overrides cannot redirect startup-configured connection pools without redeploying consumers or adding application support.

## Revisit conditions

Revisit resource provisioning when a concrete provider can coordinate consumer bindings; add tested network and authorization controls before claiming tenant isolation.
