# ADR 006: Lifecycle and cleanup

Status: Accepted

Date: 2026-09-05

## Decision

Persist intent, reconcile idempotently, track desired and observed generations, expire through the deletion path, and verify cleanup before recording destroyed status.

## Alternatives

Synchronous imperative API provisioning cannot reliably recover across crashes. Best-effort deletion with no tombstone hides leaked resources and makes retries ambiguous.

## Consequences

Use deterministic names, installation labels, recorded resource identities, capped backoff with jitter, and durable operations. Readiness requires the actual demo ingress chain plus an unaffected baseline. Retain installed override routes on workload failure. Remove and verify ingress first, drain bounded requests, remove mesh entries, then delete owned resources.

## Revisit conditions

Revisit readiness and rollout guarantees before adding replicas, atomic updates, or production operation. Updates will require expected-generation checks and retain composition endpoint identity.
