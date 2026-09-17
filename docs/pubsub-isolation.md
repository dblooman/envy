# Pub/Sub message isolation

A composition can enable `message_isolation` when it is created. The setting is
fixed for its lifetime. It defaults to false for existing clients and recipes.
The agent chooses it from schema changes, event behavior, downstream effects and
uncertainty; an explicit user setting wins. A consumer deployment is optional.

With isolation enabled, publishers add `envy_composition=<composition-id>` to
Pub/Sub **message attributes**, not HTTP request headers or message data. Envy
creates independent pull subscriptions before starting preview workloads or
publishing the preview ingress route. With isolation disabled, messages use
baseline envy. Shared topics, data stores, credentials and external effects
are not independently isolated by this feature.

## Operator onboarding

1. Enable the controller provider using `ENVY_PUBSUB_ENABLED=true`, the server
   configuration field `pubsub_enabled: true`, or Helm `pubsub.enabled=true`.
   The default is disabled; a disabled controller rejects isolated creation.
2. Configure Google Application Default Credentials for the controller. Use an
   operator-managed identity with topic get/listSubscriptions/attachSubscription
   and subscription get/create/delete permissions in the registered projects.
   Existing cloud IAM must also allow inspecting attached subscriptions in other
   projects. Envy does not create IAM grants or read message payloads. Identity
   federation or externally supplied ADC configuration is operator-managed.
3. Prepare every baseline subscription to exclude the isolation attribute, then
   register topics and consumers as part of the baseline. Broker resource names
   are fully qualified. Publishers and consumers must be registered components.
4. Integrate the application contract below into **all** participating paths,
   including inherited publishers, scheduled jobs and subsequent events. Configure
   application cloud credentials separately through approved workload mechanisms.
   Catalog literal environment values must not contain credentials. Deployment
   profile dependency approval remains explicit; Envy does not infer permission
   to copy a baseline cloud identity.
5. Run the application propagation checks before claiming end-to-end isolation.

Example addition to an existing baseline registration:

```json
{
  "pubsub": {
    "order-events": {
      "topic": "projects/example-dev/topics/order-events",
      "publishers": ["checkout"],
      "consumers": {
        "billing": {
          "subscription": "projects/example-dev/subscriptions/billing-baseline",
          "component": "billing",
          "subscription_env": "ORDER_EVENTS_SUBSCRIPTION",
          "filter": "attributes.kind = \"order\""
        }
      }
    }
  }
}
```

This is a fragment, not a complete baseline document. Each logical consumer needs
its own binding. Replicas of that consumer share its subscription. The first
release supports at most twenty topics and forty consumer bindings per baseline.
Registration snapshots logical names and bindings; existing compositions retain
that snapshot. A business predicate is optional and must fit the broker's
256-byte limit after combination with the isolation predicate.

The exact baseline filter for this example is:

```text
(NOT attributes:envy_composition) AND (attributes.kind = "order")
```

A preview filter is:

```text
(attributes.envy_composition = "<composition-id>") AND (attributes.kind = "order")
```

Envy conservatively requires this canonical form. At onboarding, creation and
reconciliation, it inspects all subscriptions attached to each topic, including
other projects. It rejects unprotected subscriptions and filters outside its
supported baseline/owned-preview forms. Topic and subscription message transforms
are unsupported because they can change routing attributes or payloads; remove
them before onboarding. Detached or redirected preview subscriptions fail readiness.
Envy never patches operator-owned filters.
This is routing isolation inside a trusted development installation, not an IAM
security boundary. Subscription inventory can change after a check; operators
must prevent unapproved unfiltered subscribers. Drift causes readiness failure
and no new workload startup, but cannot undo messages already delivered elsewhere.

### Migrating existing baseline subscriptions

Pub/Sub filters are immutable. Use a coordinated migration rather than replacing
an active subscription and discarding its backlog:

1. Keep isolated previews disabled. Quiesce publishers and stop old consumer
   pulls/push envy, allowing in-flight handlers to settle.
2. Take a snapshot of each old subscription and record its identity. Confirm
   retention and snapshot requirements for the existing subscriptions.
3. Create replacement subscriptions with the canonical exclusion and existing
   business predicate. Preserve the required envy settings for baseline
   consumers. Seek replacements to the snapshots and wait for seek completion.
4. Switch baseline consumer configuration to replacements. Ensure old consumers
   are stopped before resuming replacements; consumers must tolerate redelivery.
5. Resume ordinary publishing and validate baseline processing and backlog
   continuity. While old subscriptions remain, isolated previews remain disabled.
6. Remove retired subscriptions after validation and register the replacements.
   Envy's inventory check must pass before isolated previews are enabled.

If validation fails before retirement, pause publishers and consumers again and
recover using the recorded subscriptions/snapshots. Do not run old and replacement
business consumers simultaneously as a migration shortcut.

See Google's [filter migration guidance](https://docs.cloud.google.com/pubsub/docs/subscription-message-filter)
and [subscription delivery semantics](https://docs.cloud.google.com/pubsub/docs/subscription-overview).

## Application integration contract

No application SDK or traffic proxy is included. Implement these rules in the
application's publishing and receiving boundary:

- Ingress sets W3C baggage `composition=<id>,envy_message_isolation=true|false`.
  Propagate both members through internal HTTP calls. Shared baseline publishers
  use the incoming request context, not their deployment identity.
- Preview workloads receive `ENVY_COMPOSITION_ID`, `ENVY_MESSAGE_ISOLATION`, and
  the consumer environment keys registered for that component. Background jobs
  without an incoming request/message use the deployment identity and mode.
- Incoming request/message context takes precedence over deployment defaults.
  A composition member without an explicit boolean isolation decision is an
  error. An isolated request missing composition identity is an error. Do not
  silently turn a malformed isolated publish into a baseline publish.
- For an isolated publish, add the exact composition ID as `envy_composition`.
  Preserve business attributes and body; reject conflicting caller-supplied
  routing attributes. Retries must preserve the same routing context.
- For shared publishing, omit `envy_composition`. Do not propagate preview routing
  baggage through a shared event as if it were isolated. Trace identity may be
  propagated separately. An untagged baseline event starts a baseline context.
- Consumers subscribe only to their injected subscription. An empty binding means
  **consumption disabled**, and must never select an application default or
  baseline subscription. This is how registered preview workers remain detached
  when isolation is disabled.
- An isolated consumer checks the received identity against its composition and
  reconstructs both baggage members for downstream HTTP calls or publications.
  Reject mismatches before business handling; do not reroute to baseline.

Illustrative language-neutral publisher logic:

```text
context = incoming_context if present else deployment_context
validate composition identity and explicit isolation mode
attributes = copy business_attributes
if context.isolated:
    require context.composition
    attributes.envy_composition = context.composition
else:
    require no caller-supplied envy_composition
publish(topic, original_body, attributes)
```

The infrastructure cannot recognize a publish path that bypasses this contract.
`MessagingReady` means filters/subscriptions were observed; it does not prove
instrumentation, schema compatibility, successful business processing, or absence
of shared downstream writes. Shared-topic schema enforcement still applies:
a payload rejected by the topic schema cannot bypass validation with an attribute.

## Capture first, attach a worker later

Create a producer-only preview, or an inherited preview whose shared publishers
implement context propagation:

```sh
envy composition create --project shop --baseline staging \
  --name order-contract-check --override checkout=example/checkout:new \
  --message-isolation --ttl 4h

envy composition get COMPOSITION_ID
```

Read `message_subscriptions` from the response. Each entry contains its topic,
consumer, generated subscription name, filter, retention, readiness, and cleanup
deadline. Subscriptions retain unacknowledged messages for seven days; inactive
subscription expiration is disabled. Composition destruction or TTL cleanup
removes subscriptions and their remaining backlog, even before seven days.

Inspect using your own Google credentials:

```sh
gcloud pubsub subscriptions pull \
  projects/PROJECT/subscriptions/RETURNED_SUBSCRIPTION_ID \
  --limit=10 --format=json
```

Do not add `--auto-ack` for inspection. A pull temporarily leases messages and
competes with running workers; messages become available again after the ack
deadline. Explicit acknowledgement removes messages from that subscription's
backlog. Pub/Sub is not a nondestructive browsing API.

To test processing later, run a local consumer against that subscription, or add
an approved consumer override with the existing generation-checked update:

```sh
envy composition update COMPOSITION_ID --expected-generation GENERATION \
  --override checkout=example/checkout:new \
  --override billing=example/billing:new
```

Send the complete desired override set. Adding, removing or updating a consumer
does not rename or delete its subscription. Keep the existing three-override
limit. Workers use current approved profiles, including required HTTP health
endpoints. Isolation and TTL remain unchanged; changing them needs a new preview.
Recipes preserve the isolation choice and recreate new subscriptions.

Cleanup withdraws ingress, drains requests, stops workloads, then removes owned
subscriptions. Local/external consumers must be stopped by their caller. Cleanup
retries until absence is observed. Ownership conflicts and permission errors
remain visible. If an externally deleted subscription is recreated, the persistent
`backlog_may_be_lost` warning reports possible loss of queued messages.

## Verification

```sh
# Unit/provider/lifecycle and protocol tests
go test ./internal/domain ./internal/application ./internal/providers/... \
  ./internal/reconciler ./internal/api ./internal/cli ./internal/mcp

# Disposable pinned emulator; script verifies filtering instead of assuming it
bash deploy/testing/pubsub-emulator.sh

# Real Google Cloud acceptance: explicitly select a disposable test project
ENVY_PUBSUB_TEST_PROJECT=YOUR_TEST_PROJECT \
  go test ./internal/providers/pubsub -run TestBrokerAcceptance -count=1 -v
```

The cloud suite uses caller ADC and uniquely named resources, cleans up only those
resources, and verifies filters, retention/expiration configuration, caller IAM,
schema rejection, and inspection followed by consumption. It never infers a
project from gcloud configuration. The emulator verifies broker filtering,
schema rejection and capture/attachment; it cannot establish cloud IAM or actual
retention expiry. The test fixtures demonstrate propagation without shipping a
production SDK. Run the same contract checks against your real applications.
