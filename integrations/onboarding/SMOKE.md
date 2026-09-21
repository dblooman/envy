# HTTP business acceptance

`smoke.py` checks declared JSON responses from a baseline and two existing previews.
Use it after the [readiness review](README.md) and preview creation. It sends only
GET requests to the configured application URLs; it does not call Envy's control
API or manage composition lifecycle. Choose synthetic read scenarios whose GET
handlers have no unwanted effects.

## Configure the scenario

Copy `smoke.example.json` into your own configuration repository. The example
uses synthetic product data and three loopback endpoints. It does not start
servers or assume these ports are already running. Its business fields match the
public [shop example](../../examples/shop/README.md): baseline v1 costs 1200 and
both v2 previews cost 990. Replace the URLs with your existing application endpoints.

Each target requires:

- `url`: an absolute application URL, including the read path. HTTPS is required
  except for exact loopback HTTP hosts (`localhost`, `127.0.0.1`, `::1`). Queries,
  fragments and embedded credentials are rejected. This helper has no wildcard
  localhost DNS override; use HTTPS or an explicitly configured loopback proxy
  for development hosts that require name-based routing.
- `expect`: one to 32 JSON pointer/value assertions. Missing fields fail, even when
  the expected value is `null`. Empty pointer means the full document; `/items/0`
  selects an array item, `~1` escapes `/`, and `~0` escapes `~` in object keys.
- Optional `composition_id` for a preview: sends `baggage: composition=ID` for
  application propagation. The baseline cannot set this. Preview URLs can already
  select a composition without this option. Merely sending baggage does not prove
  the application propagated it.
- Optional `token_file`: a bearer token file relative to the scenario file, or an
  absolute path. Tokens are loaded separately per target before requests begin.
  No Envy API token environment variable is used. Use application credentials
  appropriate to that specific endpoint; never commit tokens or secrets.

Targets must be exactly `baseline`, `preview_a` and `preview_b`, with distinct
URL/composition pairs. The optional `rounds` is 1–10 (default 2), and
`timeout_seconds` is 1–30 (default 10).

`comparisons` contains one to 32 rules with `pointer`, `relation` and an optional
`targets` list. The default is all three targets; an explicit list must select two
or three different target names. `equal` requires every selected value to match;
`distinct` requires every selected value to differ from every other selected value.
The example distinguishes each preview release from baseline while allowing the
two preview releases to be identical. Use a response field with actual workload
identity if the application exposes one and that distinction needs testing.

JSON values are compared by canonical serialization, including nested booleans
versus numbers; numeric forms such as `1` and `1.0` are distinct. Floating-point
values use Python JSON decoding precision; use integers or strings for exact
amounts and identifiers. Duplicate object keys, non-finite numbers and invalid JSON are rejected. Every
endpoint must return HTTP 200 with a JSON content type. Redirects are refused,
including same-origin redirects: a preview falling back to baseline must not
silently pass. Use an explicit read path such as `/products`, rather than a root
path that redirects.

## Run and interpret

```sh
python3 integrations/onboarding/smoke.py \
  --config integrations/onboarding/smoke.example.json --check-config

python3 integrations/onboarding/smoke.py \
  --config /path/to/application/smoke.json \
  --output /path/to/ignored/smoke-report.json
```

`--check-config` validates schema and assertions without reading token files or
sending requests. Normal execution preloads all token files, then each round sends
`baseline → preview_a → baseline → preview_b → baseline`. Every response is checked;
one failed request makes the run fail even if later responses pass. There are no
retries that can hide an intermittent assertion failure. Each comparison uses the
current round's preview responses and final baseline response; failed requests
cannot reuse a previous successful response.

Responses are limited to 1 MiB. The configured timeout applies to network
operations and body reading; it is not a hard process-wide deadline. There are at
most 50 requests. TLS verification stays enabled and redirects cannot forward
credentials to another endpoint.

Exit codes:

- `0`: all configured expectations and comparisons passed.
- `2`: at least one HTTP, JSON or assertion check failed; inspect the report.
- `1`: invalid configuration, credential input or report destination.

Reports identify the scenario, configuration hash, rounds, target labels,
assertion numbers and fixed error codes. They omit URLs, tokens, response bodies,
response headers and actual/expected values. Assertion numbers are one-based in
configuration order. Failed expectation numbers refer to the selected target's
`expect` entries; comparison numbers refer to the `comparisons` list. Preserve the
matching private configuration to interpret a report. Files use owner-only
permissions and atomic replacement; stdout is also supported. Output cannot
replace the scenario or any token file, including through a symlink.

A passing report establishes only the configured HTTP observations. Equal-only
checks cannot distinguish baseline fallback from correct preview selection.
Distinct response values do not automatically establish per-hop identity, baggage
propagation, data isolation or disabled background work. Do not infer cloud IAM or
full application correctness from this result. Add application-owned tests for
those claims. Response data exists transiently in memory, but is not written to
the report; expected values and configuration should use synthetic data.

Run the same scenario after image updates with updated expectations, and retain
separate reports. Use existing Envy lifecycle acceptance or application-owned
checks for restart, failure diagnostics, explicit deletion, TTL and resource
absence. The runner does not perform those operations.

## Validation

Tests use synthetic loopback HTTP servers and cover business mismatches, baseline
regressions, target differences, credential handling and malformed responses.
They require neither a private repository nor cloud access:

```sh
python3 -m unittest discover -s integrations/onboarding -p 'test_*.py'
```
