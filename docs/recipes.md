# Saved environment recipes

A recipe is a versioned JSON file containing selected environment intent. It
allocates no resources until a caller recreates it. PostgreSQL remains canonical
for compositions; Envy does not store a new recipe or task-workspace entity.

The CLI and MCP use a shared private-client helper over existing REST get,
create and frontend-binding operations. Structural validation is local. The
create API still checks catalog permissions, build provenance, registry access,
TTL and capacity before accepting a composition.

## Export and validate

```sh
delivery recipe export COMPOSITION_ID > recipe.json
# Optionally select exact existing frontend bindings; repeat --frontend.
delivery recipe export COMPOSITION_ID \
  --frontend storefront-web=FULL_GIT_SHA > recipe.json
delivery recipe validate --file recipe.json
```

Export works with a live composition or retained tombstone. It preserves project,
baseline ID and binding revision, one to three override selections and the original
requested lifetime. Exporting a frontend requires that its selected binding
belongs to that composition. No frontends are selected by default; this avoids
choosing among historical revisions on the caller's behalf.

Overrides preserve either a build ID or an immutable direct-image digest.
Tag-only direct images are rejected: resolve and explicitly select an immutable
image first. Build IDs are local to an Envy installation and remain subject to
current repository access and artifact retention. There is no implicit conversion
to a direct image if a saved build becomes unavailable.

```json
{
  "api_version": "envy/recipe-v1",
  "project": "shop",
  "baseline": "staging",
  "baseline_revision": "shop-v1",
  "ttl": "2h",
  "overrides": {
    "pricing": {
      "image": "registry.example.com/shop/pricing@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
    }
  },
  "frontends": []
}
```

Each optional frontend entry contains `name`, an HTTPS `repository` URL, and a
full lowercase Git `revision`. At most twenty selections are accepted. Recipes
exclude endpoints, readiness, generation, credentials, Kubernetes identities,
source observations and browser evidence. Unknown fields, unsupported versions
and files larger than 64 KiB are rejected. Validation success means the document
is structurally valid, not that its artifacts exist or the environment can start.

## Recreate and resume

```sh
delivery recipe recreate --file recipe.json --name pricing-tomorrow \
  --idempotency-key UNIQUE_KEY_FOR_THIS_RECREATION
```

Save the key before calling. Retry with the same key, name and recipe for a
single recreation; choose a fresh key to intentionally start another composition.
The returned `composition` includes its new ID, URL, operation, generation and
expiry. Creation returns accepted intent; wait for readiness before using it.

The helper sends `expected_baseline_revision` to `POST /v1/compositions`. A
different registered binding revision returns 409 before creation is accepted.
Inspect the current baseline and explicitly amend the recipe to use its revision
if that is the intended change. Deployments and data behind unchanged bindings
remain live; a recipe does not reproduce a frozen staging environment.

The helper then creates fresh frontend binding identities derived from the new
composition and saved frontend identity. Use returned `bindings[].binding` keys
with the existing resolve/build/publish/check workflow. It does not rebuild or
host frontends, preserve old URLs or copy passing evidence. Multiple selected
revisions of one frontend can target the same composition.

Creation and frontend binding are separate writes. If bindings fail, the result
still includes the accepted composition and `binding_errors`; CLI returns exit
code 1 alongside that structured result. MCP returns the same structured fields
and an explanatory message. Retry the same recreation key to recover bindings
without another composition. A cancelled caller can recover the accepted create
using the same key. Keep the returned ID for explicit cleanup even if frontend
work fails. No automatic rollback or cleanup is implied by a partial failure.

Recipe keys cover the backend create request. Editing frontend selections with
the same key may add bindings to the recovered composition; it does not change
its backend, lifetime or previously published bindings. Use separate recipes and
keys when intending independent environments.

Use `delivery composition destroy ID` and wait for `destroyed` when finished.
The recipe survives cleanup in the location chosen by the caller. The default
server TTL maximum and live-composition cap apply as usual; no TTL renewal,
suspend/resume or automatic PR controller is introduced.

## MCP

- `export_recipe`: `{id, frontends?: [{name, revision}]}` → recipe.
- `validate_recipe`: `{recipe}` → structural validation result; no network writes.
- `recreate_recipe`: `{recipe, name, idempotency_key}` → composition, bindings and
  binding errors. Then use the usual bounded wait, frontend and cleanup tools.

The agent kit describes when no preview, explicit baseline use, reuse, an image
update or a replacement is appropriate. Adding/removing override components
still requires a new composition. See [environment workflows](environment-workflows.md)
for the broader product proposal and [frontend bindings](frontend-bindings.md)
for evidence and lifecycle semantics.

## Validation — 2026-09-11

`go test -race ./...` passed; the PostgreSQL integration environment was not set
for that unit run. The live smoke check used the existing PostgreSQL-backed
control plane and private GHCR builds in `envy-dev`:

- Exported destroyed composition `76c53bff7451272282788306`, including one explicitly
  selected frontend revision, through CLI and an actual MCP stdio client.
- A deliberately changed baseline revision was rejected with conflict.
- Recreated as `628adf754c43da0945cbdd8f`; replay through MCP retained the same
  composition and fresh frontend binding with no copied publication/check.
- Resolved the new frontend binding to the new ready URL; interleaved HTTP calls
  returned GBP 10.90 for the saved pricing build and GBP 12.00 for the baseline,
  with the expected context at both services.
- Destroyed the smoke composition; its hostname returned 404 and namespace was
  absent. The saved recipe remains in the operator's ignored setup directory.

Unit coverage includes immutable intent validation, unknown fields/versions,
baseline revision guards, explicit frontend selection, partial binding retry,
CLI key requirements and all three recipe tools through the MCP SDK. Skill
frontmatter validation passed. Runtime membership changes and frontend hosting
are not part of this check.
