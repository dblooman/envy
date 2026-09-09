# Frontend bindings and agent workflow

A frontend binding maps a project's frontend name and full Git commit SHA to one
composition. Frontend and backend branches need not match. The association is
immutable and repeatable; changing its composition or repository conflicts.
Use a new frontend name or revision for a different association. Binding records
remain inspectable after expiry or destruction, but cannot resolve to a usable
API URL then. Envy does not deploy or delete the external frontend.

## Interfaces

The authenticated REST API is authoritative. Under
`/v1/projects/{project}/frontend-bindings/{frontend}/{revision}`:

- `PUT` binds `{composition, repository}`; identical retries return the original.
- `GET` returns the binding and current composition availability.
- `GET /resolve` returns a build receipt with API URL, composition generation,
  expiry and binding version only when the composition is currently ready.
- `POST /deployment` records `{expected_version, url}` from the caller.
- `POST /check` records a caller-reported browser check with `expected_version`,
  `composition_generation`, `status` (`passed` or `failed`) and `message`.

`GET /v1/compositions/{id}/frontend-bindings` is bounded and paginated. CLI
`delivery frontend bind/get/resolve/publish/check` and matching MCP tools use
these endpoints. MCP also gains composition listing for discovery and reuse.

Binding versions fence concurrent publication/check updates. A new publication
clears earlier browser evidence; a backend generation change makes earlier checks
stale. A reported URL does not prove a hosting deployment succeeded. Browser
checks are caller reports, separate from Envy's workload/ingress verification;
the control plane does not execute tests or fetch arbitrary frontend URLs.

Resolution waits are bounded, cancellable, and read-only. A build may wait for
its exact association to arrive and for readiness, but never selects another
revision, a similarly named composition, or staging. Deleted/expired compositions
fail immediately even if the controller has not processed expiry. A receipt is
an observation, not a lease: expiry and updates can occur after resolution.

## Build integration

The Cloudflare Pages adapter reads `CF_PAGES_COMMIT_SHA` and `CF_PAGES_URL`, as
documented in [Cloudflare's build configuration](https://developers.cloudflare.com/pages/configuration/build-configuration/#environment-variables).
It resolves the exact association, passes the public API URL through a configured
public build variable, runs the application's existing build, and records the
reported frontend URL after a successful build. Hosting can still fail afterward;
a separate browser check establishes caller-reported evidence of the live path.

Envy credentials remain in the adapter environment and are removed from the
child build environment. They are never written to generated frontend config.
Cloud-hosted frontends require a reachable HTTPS Envy API and HTTPS preview API;
the local loopback cluster cannot serve a Cloudflare build or browser remotely.
TLS, CORS, cookies and login callbacks are application/ingress configuration,
not implied by a successful backend probe. The local shop frontend demonstrates
browser CORS and composition selection without claiming generic login support.

## Agent kit

The repository ships an `envy-compose` skill, an adaptable repository-instruction
template, and a frontend build configuration example. The workflow is discover,
resolve prebuilt images, create/reuse an explicit composition, wait, inspect,
bind an exact frontend revision, build/publish externally, verify, and report.
One coordinator owns combined updates using expected generations. External
builds, tests, source-control actions and hosting remain the caller's tools.

Validation covers project scoping, immutable retries, publication conflicts,
stale browser checks, expiry while the controller is stopped, safe build
environment handling, real CLI/MCP use, and a browser-to-shop preview path.
