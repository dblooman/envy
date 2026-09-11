# Live local workflow check — 2026-09-11

Environment: private `dblooman/envy-test-pricing` and `envy-test-web` repositories,
private GHCR images, the dedicated `envy-dev` kind cluster, and loopback Envy API.

The pricing source change `6ede0296617719def9a50a571dae9a3eb8c9cea7`
changed the standard price from GBP 12.00 to GBP 10.90. Its successful Actions
run was `34585787882`. The prior GBP 12.00 build came from run `34583453838`.
The frontend checkout was `e9bf16babf48bdb20518ecc734da24bdf3994e4b`.

Verified with the helper and live browser:

- Create from the new run: ready composition `76c53bff7451272282788306`, generation 1.
- Replay the same run: same composition and generation; no unnecessary rollout.
- Chrome at localhost:4174: GBP 10.90 with the correct composition at both hops.
- Interleaved baseline calls: GBP 12.00 and empty composition context.
- Concurrent invocation while serving: rejected by the local workflow lock.
- Rollback to the earlier run: generation 2 at the same composition URL; Chrome
  returned GBP 12.00 and retained the correct composition context.
- Lost-acknowledgement simulation: remove the saved composition ID while retaining
  its original create request and idempotency key. Replaying recovered the same
  composition, then updated to the newer run at generation 3 without duplication.
- Cleanup: the helper reached `destroyed`; the hostname returned 404 and the
  composition namespace was absent. The baseline stayed at GBP 12.00 and the
  earlier independent pricing-v2 preview stayed at GBP 9.90.

Unit tests additionally check preserving other components' build/image overrides
without copying source provenance into update requests, and rejecting reports
with mismatched run IDs, attempts, commits, components or unsuccessful runs.
