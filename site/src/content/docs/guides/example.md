---
title: Multi-Service Overrides Guide
description: How to compose and test changes across multiple interconnected microservices simultaneously.
---

While most pull requests only modify a single microservice, complex cross-cutting features often require testing simultaneous changes across multiple services (for example, updating both the `gateway` and an internal `orders` service).

Envy supports selecting **up to 3 approved component overrides** in a single composition.

---

## Defining Multiple Overrides via CLI

Use repeated `--override` flags with `delivery composition create`:

```bash
delivery composition create \
  --project shop \
  --baseline staging \
  --name feat-checkout-v2 \
  --override gateway=registry.internal/shop/gateway:pr-92 \
  --override orders=registry.internal/shop/orders:pr-92 \
  --ttl 6h
```

---

## Request Routing with Multiple Overrides

When both `gateway` and `orders` are overridden:

```text
Preview Ingress (cmp-84f1a)
    │
    ▼
gateway-v2 (Override) ──► service-a-v1 (Shared) ──► orders-v2 (Override)
```

1. **Ingress Route**: The preview hostname routes directly to the overridden `gateway-v2` container instead of the baseline gateway.
2. **Intermediate Hops**: `gateway-v2` calls `service-a`, which remains shared from the deployed reference baseline. The local demo uses `staging` as that baseline's catalog ID.
3. **Downstream Mesh Route**: When `service-a` calls `orders`, the Istio mesh VirtualService inspects the `baggage: composition=cmp-84f1a` header and redirects the call to `orders-v2`.

---

## Updating Multi-Service Compositions

You can update individual images independently during rolling updates by providing the complete desired override set:

```bash
delivery composition update cmp-84f1a \
  --expected-generation 1 \
  --override gateway=registry.internal/shop/gateway:pr-92 \
  --override orders=registry.internal/shop/orders:pr-92-fix2
```
