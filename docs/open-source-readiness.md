# Open-source readiness audit

Reviewed 2026-09-14 against `2e3ebd9` plus the readiness changes in this working tree.
Scope: source publication, GitHub Pages, contributor setup, dependency/secret checks,
and obvious unsafe defaults. This is not a comprehensive penetration test or a
claim that arbitrary tenant workloads are isolated from one another.

## Findings and disposition

| Priority | Finding | Disposition |
| --- | --- | --- |
| High | Site assumed a root domain, so custom links and favicon would break at `/envy/`. | Fixed with a shared base-path helper, Markdown transform, canonical/404 fixes, build crawler, and browser checks. |
| High | No fast PR gate covering the two frontends, and no Pages deployment. | Added CI for Go, PostgreSQL, both frontends, integration/chart checks, secrets, and vulnerabilities; Pages deploys only after these pass on `main`. |
| Medium | Developer prerequisites and simulator instructions were incomplete. | Added contribution guide, tool pins/engine checks, setup/help/doctor targets, preflight errors, and instructions matching the sidebar's simulation switch. |
| Medium | React titles required a mouse; labels and Hook dependencies were not checked. | Added lint/type/format checks, keyboard-accessible title buttons, label awareness for shared inputs, and regression tests preserving drafts and applied filters. |
| Medium | Vitest 3.2.7 / `@vitest/mocker` matched [GHSA-82fw-gwwq-j7x9](https://github.com/advisories/GHSA-82fw-gwwq-j7x9). | Updated to Vitest 4.1.11; frontend tests pass and pnpm reports no known vulnerabilities. |
| Medium | Required Go modules matched [GO-2026-5841](https://pkg.go.dev/vuln/GO-2026-5841) and [GO-2026-4610](https://pkg.go.dev/vuln/GO-2026-4610), although vulnerable code was not called. | Updated `klauspost/compress` to 1.18.7 and Docker CLI to 29.2.0; govulncheck reports no vulnerabilities. |
| Medium | CI action tags were mutable and mesh CI inherited default token permissions. | Pinned actions to verified commit SHAs, set read-only defaults, disabled persisted checkout credentials, and isolated Pages write permissions. |
| Low | Frontend formatting could rewrite the generated mesh matrix. | Protected only the generated block from formatting; generation and format checks now agree. |
| Low | Each demo variant repeated Go compilation. | Added BuildKit compilation caches to demo, shop, and server Dockerfiles. |
| Low | Local environment files could enter Docker build contexts. | Excluded local `.env` files and Python caches; expanded Git ignores for local state. |
| Low | ESLint 9 and the old Base UI package have maintenance notices. | Kept the compatible ESLint 9 ecosystem because `eslint-plugin-jsx-a11y` declares support through ESLint 9. Track ESLint 10 compatibility and the Base UI rename via dependency updates; neither is a known vulnerability in the final scans. |

## Secret, configuration, and license review

- Gitleaks 8.30.0 scanned all available history (72 reachable commits; 68 commits
  with scanned changes) and a separate snapshot containing only publishable source
  files, including new files. No credentials were found after reviewing three
  matches in `internal/routing/baggage_test.go`. The allowlist covers only that file
  and its exact dummy composition identities; all other rules remain enabled.
- Historical filenames contained no committed `.envy/`, `.env`, kubeconfig, private
  key, dependency tree, or `.DS_Store` artifacts. Private state under `.envy/` was
  intentionally excluded from publication and scanning outputs remain local/redacted.
- Reviewed local bootstrap/token generation, Vite proxy configuration, HTTP auth,
  server timeouts, Helm roles, and reusable workflow execution boundaries. Local
  ports remain loopback-bound; credentials have restrictive permissions; the Vite
  token is server-side; identity-proxy requests require a trusted peer and secret.
- Envy's controller has substantial Kubernetes privileges and previews share live
  baseline services/data. Those are documented product boundaries, not a sandbox
  for hostile workloads. A deeper multi-tenant security review remains separate work.
- MIT remains unchanged. The [license inventory](third-party-licenses.md) records
  Go and JavaScript findings, native/build-time dependencies, and reproduction commands.

## Validation evidence

- Frozen installs succeeded under Node 24.8.0 and pnpm 10.20.0 in a separate source
  checkout with no pre-existing `node_modules` or `.envy` state. Its fast check suite
  passed; follow-up regression tests were also run against the final sources.
- Go formatting, vet, and default tests passed. PostgreSQL-backed validation and
  the disposable-cluster lifecycle acceptance also passed (details below).
- Both frontends pass lint, formatting, and production builds. Astro type checks,
  base-path/crawler regression tests, six diagram tests, and the 18-page link/anchor/
  asset/sitemap crawl pass. Dashboard tests include draft preservation, exact build
  selection, keyboard title activation, and explicit activity filter application.
- Browser checks: homepage and direct quickstart routes; Pagefind query `quickstart`
  and result URLs under `/envy/`; mobile menu at 390×844; light/dark themes; no page
  overflow at that mobile width; keyboard code tabs and skip-to-content; custom 404
  recovery. Dashboard sidebar simulation displays sample compositions, and the
  Installation view exposes labeled connection controls.
- Lightweight integration tests, Helm lint/rendering, all three chart profiles and
  invalid configurations, and generated-version consistency pass. Actionlint passes.
- Final pnpm audits for both packages and govulncheck report no known vulnerabilities.
- Local validation does not establish GitHub-hosted CI or deployment success; verify
  the pull request checks and the first Pages deployment separately.
  This local pass must not be represented as a successful GitHub deployment.

### Live acceptance

PostgreSQL 18.6: `go test -count=1 ./internal/persistence/postgres` passed against a
separate loopback-bound disposable container, including migrations and persistence
lifecycle tests. That container was stopped and removed after the tests.

Istio: `TestCompositionLifecycle` passed in 101.12 seconds after provisioning
`envy-oss-acceptance-20260914` on ports 28080/28081. It verified baseline traffic,
authenticated preview creation, idempotency/conflicts, request routing, unknown-host
404s, failure visibility when an override is offline, restart recovery, unavailable
image failure, and idempotent deletion without replacing baseline workloads.
The harness deleted its cluster successfully. The pre-existing `envy-dev` cluster
was retained. Initial image downloads and builds accounted for most of the run.

The broader Cilium/Linkerd, expiry, and mesh-upgrade suites were not rerun in this
launch-readiness pass. Existing mesh acceptance CI remains separate; Linkerd's
known readiness limitation remains documented.

## Reproduce

```sh
make setup
make doctor
make check
# Optional local database coverage: set this to a disposable PostgreSQL database.
ENVY_TEST_DATABASE_URL='postgres://envy:envy-ci@127.0.0.1:25432/envy?sslmode=disable' \
  go test -count=1 ./internal/persistence/postgres
ENVY_CLUSTER_NAME=envy-oss-acceptance ENVY_PREVIEW_PORT=28080 ENVY_API_PORT=28081 \
  ENVY_E2E_TEST_RUN=TestCompositionLifecycle make test-e2e

go install github.com/zricethezav/gitleaks/v8@v8.30.0
go install golang.org/x/vuln/cmd/govulncheck@v1.8.0
go install github.com/rhysd/actionlint/cmd/actionlint@v1.7.12
gitleaks git --redact --log-opts=--all
govulncheck ./...
actionlint
(cd web && pnpm audit --audit-level moderate)
(cd site && pnpm audit --audit-level moderate)
```

Never run a directory secret scan over `.envy/` or installed dependencies to
assess source publication; scan a clean source snapshot so local state and upstream
fixtures do not obscure the actual publishable files. Scan Git history separately.

No unresolved code-level launch blockers were found within this audit scope.
The GitHub-side publication steps below remain unverified until performed.

## Publication checklist

Repository visibility was confirmed private. Pages and private-reporting API
lookups returned 404, so their configuration could not be verified.

- [ ] Merge the readiness changes and require the fast CI checks on `main`.
- [ ] Decide when to make the repository public; this audit does not change visibility.
- [ ] Enable GitHub private vulnerability reporting, secret scanning, and push protection.
- [ ] Set **Settings → Pages → Source → GitHub Actions**, leave the custom domain empty,
      and restrict the `github-pages` environment to `main`.
- [ ] Run **CI and Pages** on `main` and verify the deployment at
      `https://dblooman.github.io/envy/`, including search and direct/404 routes.
- [ ] Confirm ownership of the existing SVG branding; scanners cannot establish authorship.
- [ ] Before distributing release binaries/images, generate notices for the actual
      shipped dependencies, as described in the license inventory.

No repository visibility change, credential rotation, history rewriting,
release publication, or GitHub settings mutation was performed by this work.
