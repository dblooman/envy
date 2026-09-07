# Image updates and delivery CLI

Image updates use the REST API through the CLI, MCP or frontend. Catalog
registration is described in [catalog](catalog.md); logs and events are in
[diagnostics](diagnostics.md). Compositions support one to three overrides.

`PATCH /v1/compositions/{id}` accepts `expected_generation` and the complete
`overrides` map, retaining every existing component key. PostgreSQL locks the composition row, checks
the expected generation and lifecycle, increments the generation, and commits
an update operation before any provider changes. Only ready or failed, unexpired
compositions can be updated. Stale generations, concurrent rollouts, and deletion
return 409. A same-image update is accepted as a new verification generation;
it does not force a pod restart. Retrying an accepted PATCH with its old generation
returns 409; read the composition to resolve an uncertain response.

The composition ID, hostname, expiry, baseline bindings, ownership token, namespace,
Deployment, Service, and routing destination stay stable. Readiness is cleared
until the new generation passes workload and ingress verification. Each update
has its own persisted provisioning start time, including across process restarts.
Existing records use their creation time until their first update.

Kubernetes uses ordinary rolling deployment semantics. During rollout the URL can
serve the previous or new override. A failed new image can leave the previous
override serving; it never causes routing to switch to the shared baseline.
Readiness requires the one-replica rollout to finish and a ready endpoint whose
pod image matches the desired image for every override. The ingress checker must
observe each of those pods. Unchanged image templates do not roll. There is no
atomic cutover across components or automatic rollback. A new update can repair a failed
composition. Deletion and expiry retain their existing precedence and cleanup.

`delivery composition create/get/list/wait/endpoints/update/destroy` uses the
private HTTP client, with JSON results on stdout and JSON errors on stderr.
`inspect` aliases `get`. Configuration uses `ENVY_API_URL`, `ENVY_API_TOKEN_FILE`
or `ENVY_API_TOKEN`, as MCP does. Update requires `--expected-generation` rather
than silently fetching and overwriting a newer caller's work. Waiting is bounded
to 60 seconds and returns the latest composition: exit 0 for ready/destroyed,
exit 1 for failure, exit 2 for timeout, and exit 130 for cancellation. Cancelling
a wait does not destroy the composition.

Acceptance creates v2 using the compiled CLI, updates to a separately built v3,
restarts the controller during rollout, verifies the same URL and object identities,
and checks baseline pod identities throughout. It also exercises stale generation,
failed-update recovery, deletion conflicts, and the MCP update tool.
