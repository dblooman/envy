# Frontend binding validation

Validated on 10 September 2026 using Go 1.27.1, PostgreSQL 18.6, the local kind
cluster and Chrome. The feature adds no resource provider or hosting service.

## Automated checks

- Go race tests across the repository, with `ENVY_TEST_DATABASE_URL` set to a
  disposable PostgreSQL instance, passed. Migration replay, immutable binding
  retries, project scope, concurrent publication fencing, pagination, generation
  staleness, check invalidation and expiry without a controller were exercised.
- REST contract checks cover authentication, strict request bodies, invalid URLs
  and check states, plus emitted frontend schemas against the OpenAPI document.
- CLI tests and official MCP SDK tests exercise all new tools over HTTP, including
  real subprocess stdio. Resolver tests cover deadlines, cancellation and no URL
  on missing, unready or gone associations.
- Six Node adapter tests cover build environment stripping, literal executable
  arguments, exact revision validation, mixed-content rejection, and suppression
  of build/publication after resolution or build failure.
- The web production build and `envy-compose` skill validation passed.

The initial fresh kind run reported nine passes and one failed assertion in
1024.452 seconds. Passing scenarios included frontend CLI/MCP binding, ingress routing,
controller restart persistence, deletion, twenty compositions, and image updates.
The original MCP lifecycle test stopped at its obsolete assertion of twelve
tools; the feature now exposes nineteen. The assertion was corrected before
commit `e05051c`.

A separate fresh `envy-frontend-check` cluster passed both the corrected MCP
lifecycle test (39.28 seconds) and frontend binding test (32.87 seconds), with
72.507 seconds total test time. All ten scenarios therefore have passing results
across the initial run and this targeted rerun; this is not a second full-suite
run. Both temporary clusters were removed. The initial run log is
`.envy/frontend-acceptance.log`; the passing rerun log is
`.envy/frontend-acceptance-rerun.log`.

## Browser evidence

A local working-tree smoke test used composition `9cd42c0ef34c9c9482d52784`,
project `shop`, frontend `storefront-web`. The binding used the current base commit
`0cd24a4ff15058c99888f5ca724f75f2131b8fe5` as test metadata; this was not a hosted
artifact built from a clean checkout of that commit.

1. The actual adapter resolved the binding, built the frontend, and reported
   `http://localhost:4174` as the external URL.
2. Chrome loaded the frontend from that separate origin. Its request to the
   composition `/products` endpoint returned release `v2`, price 990. Exposed CORS
   headers showed the composition ID at both storefront and pricing.
3. An interleaved baseline request returned release `v1`, price 1200.
4. Envy's live composition details showed the repository, full revision, reported
   URL and separate backend readiness. A caller report of this browser check was
   recorded at binding version 3 and backend generation 1.
5. Updating pricing to v1 retained the URL and made that report stale at generation
   2. The live UI displayed this change.
6. After deletion, the composition hostname returned 404, owned namespace absence
   was observed, and the browser displayed a fetch failure with no product data or
   staging fallback. The binding remained inspectable as unavailable with stale
   evidence. The test composition was destroyed.

Local logs and receipts are in ignored `.envy/frontend-*` files. The local static
server is temporary and the recorded loopback link is historical after cleanup.

## Limits

Cloudflare account deployment, remote HTTPS, authentication/cookies and login
callbacks were not tested. A successful build or recorded URL is not proof of
successful hosting. Browser checks are caller reports; Envy neither executes
browser tests nor fetches the recorded frontend URL. The adapter removes Envy
variables from the child environment but does not sandbox trusted build code.
