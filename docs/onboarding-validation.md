# Guided onboarding validation

Validated on 20 September 2026.

## Automated checks

`PATH="$PWD/.envy/tools:$PATH" make check` passed, including Go lint,
frontend checks, integration helpers, Helm rendering, mesh charts, and quickstart
packaging. No production installation was used.

- `pnpm --dir web check`: lint, formatting, 60 tests, TypeScript and production build.
- `go test ./...`: Go suite, including startup preview-policy regression tests.
- Focused uncached onboarding, catalog, preview and deployment-derived application,
  API and Kubernetes provider tests.
- `pnpm --dir site build` and `pnpm --dir site check:links`: documentation build,
  links, anchors, assets and search output.

Browser client/component coverage includes mixed profiles, immutable registration
conflicts and retries, stale validation, discovery blockers, denied lookup access,
replacement edits, approval conflicts, saved-catalog resumption, connection changes,
creation handoff, digest requirements, profile revision guards, and ignoring late
creation results after the connection-scoped view unmounts. Existing creation tests
cover idempotent retries and concurrent submission prevention.

## Live browser walkthrough

A disposable `envy-derived-onboarding` kind cluster ran Istio, PostgreSQL, Envy,
and the ordinary HTTP shop application. The browser used the current Vite frontend
against that cluster's API. No application catalog was seeded for the shop:

1. Entered `shop/staging`, namespace `envy-shop`, the existing Gateway, storefront
   and pricing Services, and `/products` HTTP verification through the forms.
2. Configured storefront as `http-small`, including `SHOP_ROLE` and its downstream
   address; configured pricing as deployment-derived. Validated and registered.
3. Discovery reported the fixture's non-default service account as a blocker and
   disabled approval. Changed only the disposable fixture to the supported default
   account, reloaded the browser, resumed the saved catalog, and rediscovered.
4. Reviewed and approved pricing revision 1. Reloaded again and verified the saved
   approval was present. Followed the handoff with the project/baseline preselected.
5. Created `shop-onboarding-check` with a pinned v2 pricing image and revision 1.
   The dashboard reported ready with HTTP reachability and routing unverified.
   **Open preview** opened `/products` in a browser tab showing v2 / price 990.
6. Created `shop-manual-check` overriding storefront through the manual profile;
   it also reached ready, with pricing inherited.
7. Destroyed both previews through the dashboard, verified both reached
   `destroyed` and their namespaces disappeared, then removed the disposable cluster.

External HTTP checks independently confirmed that the pricing override returned
v2 / price 990, while baseline calls before and after returned v1 / price 1200.
The storefront workload UID stayed unchanged during the pricing override, and
composition baggage reached pricing. The manual storefront preview returned
baseline pricing while its storefront workload UID matched the new override.
These checks are application evidence, not evidence claimed by HTTP readiness.

Raw local HTTP evidence is retained under `.envy/envy-derived-onboarding/` in
`shop-browser-evidence.json` and `shop-manual-evidence.json`.

## Deployment-derived acceptance gate

The initial Argo acceptance run exposed an existing startup ordering defect:
`configurePreviewPolicy` ran after provider factories captured configuration.
Environment-configured destination dependency permissions reached discovery but
not the runtime, so a copied ConfigMap could not be inspected. Startup now resolves
and validates that policy before constructing providers; regression tests cover
both complete and incomplete policy settings.

The acceptance helper also waits for the source Deployment rollout before each
discovery. Argo application health can briefly describe the previous generation
after sync; treating that as workload readiness caused an unrelated fixture race.

The corrected gate passed in a fresh `envy-derived-onboarding-final` cluster:

```sh
ENVY_CLUSTER_NAME=envy-derived-onboarding-final \
ENVY_PREVIEW_PORT=29080 ENVY_API_PORT=29081 make test-derived-e2e
```

`TestDerivedPreviewsWithArgo` passed in 274.57 seconds, covering copied dependencies,
parallel previews, baseline advancement, captured configuration, restart/update,
drift recovery, expiry and resource absence. The harness deleted its cluster on
completion. The two earlier disposable clusters were also removed. The final log
is retained locally at `.envy/envy-derived-onboarding-final/derived-acceptance.log`.
