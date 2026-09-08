# Onboard the tea shop

This two-service HTTP application returns a product and its price. The storefront
propagates W3C baggage with OpenTelemetry; pricing v2 changes the price from 1200
to 990 minor units. The JSON is application owned, not an Envy response schema.

From the repository root, with `envy-dev` running:

```sh
make build
make dev-shop
export ENVY_API_TOKEN_FILE="$PWD/.envy/envy-dev/api-token"
.envy/bin/delivery catalog validate --file examples/shop/application.json
.envy/bin/delivery catalog apply --file examples/shop/application.json
.envy/bin/delivery composition create --project shop --baseline staging \
  --name pricing-preview --component pricing --image envy/shop:v2
.envy/bin/delivery composition wait <id> --timeout 60s
```

Baseline requests use `http://shop.envy.localhost:8080/products`; preview requests
use `<endpoints.public.url>/products`. The root redirects browsers to `/products`.
For machines without wildcard localhost DNS, curl can preserve the HTTP Host
while dialing loopback:

```sh
curl --resolve shop.envy.localhost:8080:127.0.0.1 \
  http://shop.envy.localhost:8080/products
curl --resolve cmp-<id>.envy.localhost:8080:127.0.0.1 \
  http://cmp-<id>.envy.localhost:8080/products
.envy/bin/delivery composition destroy <id>
```

The configuration file is portable JSON. Copy it, change the project and bindings,
and keep it beside your application's source. Literal environment values are
catalog-visible: credentials and secrets do not belong in this file. Logical
component IDs must match their application container names in the baseline.
Existing baseline Services must declare HTTP and select ready injected pods.
`validate` never creates workloads or catalog entries. `apply` is atomic and can
be repeated; existing entries must match exactly. A changed immutable definition
needs a new catalog ID, not a silent overwrite.

`http` verification checks status at `/products` through both ingress hostnames.
It reports **reachability**, not proof of downstream routing or propagation.
Shop's diagnostic response headers (`X-Shop-*-Workload` and `X-Shop-*-Context`)
let the external acceptance test independently assert which pods handled each
request. Other applications should use their own evidence and checks. A missing
propagator can still produce a successful HTTP response from shared baseline
services; it must not be interpreted as an isolated test result.

`make test-e2e` installs the shop in its fresh cluster and exercises CLI onboarding,
immutable conflicts, twenty concurrent previews, admission limits, baseline
identity preservation, context propagation, controller restart and cleanup. It
records readiness and request latency under `.envy/envy-e2e/capacity.json`.
