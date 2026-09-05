# ADR 002: Canonical state

Status: Accepted

Date: 2026-09-05

## Decision

PostgreSQL owns desired state, observations, operations, and idempotency. Kubernetes contains reconciled execution state. Do not create Envy CRDs initially.

## Alternatives

CRDs would make Kubernetes the primary API and persistence layer. An in-memory prototype would be simpler but lose accepted work, expiry, and ownership across restarts.

## Consequences

The API commits intent before provider calls. One reconciler holds a PostgreSQL advisory lock and stops provider mutations on lost connectivity or lock ownership. Periodic scans recover work without depending on process-local notifications. Cleanup is durable and tombstones remain.

## Revisit conditions

Revisit CRDs only if Kubernetes-native operation becomes a product requirement. Revisit scheduling once one worker becomes a measured bottleneck.
