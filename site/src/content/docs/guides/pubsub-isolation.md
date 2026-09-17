---
title: Google Pub/Sub Isolation
description: Capture preview messages in filtered Google Pub/Sub subscriptions and attach consumers later, with explicit application and baseline prerequisites.
---

A preview can opt into Google Pub/Sub message isolation at creation. Envy creates
filtered pull subscriptions for registered consumers, so you can capture events
before deploying a worker and attach an approved consumer later.

This feature is disabled by default. It isolates message routing through prepared
subscriptions, not topics, databases, credentials, or external side effects. It
is not an IAM security boundary and does not implement Kafka or SQS isolation.

## Prepare the installation and application

Before using `--message-isolation`:

1. Enable the controller provider with Helm `pubsub.enabled=true` or
   `ENVY_PUBSUB_ENABLED=true`, and configure operator-managed Google Application
   Default Credentials. The controller needs topic inspection/attachment and
   subscription inspection/create/delete permissions, including visibility into
   subscriptions attached from other projects. Envy does not grant IAM access.
2. Prepare baseline subscriptions to exclude the `envy_composition` attribute.
   Register fully qualified topics, publisher components, and consumer bindings
   in the baseline catalog. Envy checks attached subscriptions and rejects
   unprotected or unsupported filters; it never patches operator-owned filters.
3. Instrument every participating publisher and consumer, including shared
   baseline publishers, background jobs, and subsequent events. Configure workload
   cloud credentials separately through approved deployment mechanisms.

Pub/Sub subscription filters are immutable. Follow the coordinated migration and
catalog examples in the [operator reference](https://github.com/dblooman/envy/blob/main/docs/pubsub-isolation.md)
to preserve backlog when preparing existing subscriptions. Do not replace a live
subscription just to change its filter.

## Propagate the message context

Ingress supplies W3C baggage containing `composition=<id>` and
`envy_message_isolation=true|false`. Forward both through internal HTTP requests.
For an isolated publish, set the Pub/Sub **message attribute**
`envy_composition=<id>`; do not put it only in HTTP headers or message data.

Consumers recover that context and preserve it on subsequent events and HTTP
calls. Incoming context takes precedence over workload defaults. Background jobs
without incoming context use `ENVY_COMPOSITION_ID` and `ENVY_MESSAGE_ISOLATION`.
Shared publishing omits the isolation attribute. Reject malformed isolation
context instead of silently sending the message to baseline consumers.

For example, with an optional business predicate:

```text
Baseline: (NOT attributes:envy_composition) AND (attributes.kind = "order")
Preview:  (attributes.envy_composition = "<composition-id>") AND (attributes.kind = "order")
```

No application SDK is included. See the operator reference for the full contract,
filter restrictions, and propagation checks.

## Capture messages without a worker

For an onboarded `shop` baseline with instrumented shared publishers:

```sh
envy composition create --project shop --baseline staging \
  --name order-contract-check --inherit-all --message-isolation --ttl 4h
envy composition wait COMPOSITION_ID --timeout 60s
envy composition get COMPOSITION_ID
```

Replace `COMPOSITION_ID` with the returned ID. Alternatively, select a changed
publisher using `--override checkout=example/checkout:new` instead of
`--inherit-all`. Use an approved image that your cluster can pull.

Inspect `message_subscriptions` for generated subscription names, filters,
readiness, retention, and cleanup deadlines. Publish test events through the
preview URL using your instrumented application. With your own Google credentials:

```sh
gcloud pubsub subscriptions pull \
  projects/PROJECT/subscriptions/RETURNED_SUBSCRIPTION_ID \
  --limit=10 --format=json
```

Do not use `--auto-ack` for inspection. Pulling leases messages temporarily and
competes with running workers; acknowledgement removes them from that
subscription's backlog. This is not a nondestructive browsing API.

## Attach or change consumers

Add an approved consumer to the same preview using its current generation:

```sh
envy composition update COMPOSITION_ID --expected-generation GENERATION \
  --override billing=example/billing:new
envy composition wait COMPOSITION_ID --timeout 60s
```

This example starts from the baseline-only preview above. Updates send the
**complete** desired override set: retain any publisher or other consumer you
still need. There are at most three overrides. Adding or removing a worker does
not rename or delete its subscription. Workers still need approved profiles and
HTTP health endpoints.

The isolation choice is immutable and defaults to false; changing it requires a
new preview. Updates keep the URL and original expiry. Recipes preserve the
choice and create new subscriptions when recreated. The CLI, REST, and MCP
interfaces support the same behavior; REST/MCP creation uses
`"message_isolation": true` with `"overrides": {}` for inheritance.

## Readiness and cleanup

`MessagingReady` confirms observed subscriptions and filters. It does not prove
application propagation, schema compatibility, business processing, or absence
of shared writes. Run application checks as well. Unapproved subscription drift
fails readiness and prevents new workload startup, but cannot undo messages
already delivered elsewhere.

Subscriptions retain unacknowledged messages for seven days, but preview expiry
or destruction deletes the subscriptions and remaining backlog earlier. Cleanup
withdraws ingress, drains requests, stops workloads, and removes owned
subscriptions. Stop local or externally hosted consumers yourself. Inspect
cleanup errors and any `backlog_may_be_lost` warning after subscription recreation.

For broader boundaries, read [Sharing & Isolation](/overview/sharing-semantics/).
