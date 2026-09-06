-- name: AdvisoryXactLock :exec
SELECT pg_advisory_xact_lock($1);

-- name: GetComposition :one
SELECT body, runtime, deletion_requested
FROM compositions
WHERE id = $1;

-- name: GetCompositionForUpdate :one
SELECT body, runtime, deletion_requested
FROM compositions
WHERE id = $1
FOR UPDATE;

-- name: GetIdempotencyKey :one
SELECT request_hash, composition_id
FROM idempotency_keys
WHERE key = $1;

-- name: CountActiveCompositions :one
SELECT count(*)
FROM compositions
WHERE phase <> 'destroyed';

-- name: InsertComposition :exec
INSERT INTO compositions(
    id, project, baseline, generation, deletion_requested, phase, expires_at, body, runtime
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9
);

-- name: InsertIdempotencyKey :exec
INSERT INTO idempotency_keys(
    key, request_hash, composition_id
) VALUES (
    $1, $2, $3
);

-- name: UpsertOperation :exec
INSERT INTO operations(id, composition_id, body)
VALUES ($1, $2, $3)
ON CONFLICT (id) DO UPDATE SET
    body = excluded.body,
    updated_at = now();

-- name: ListCompositions :many
SELECT body, runtime, deletion_requested
FROM compositions
WHERE (sqlc.arg('project')::text = '' OR project = sqlc.arg('project')::text)
  AND id > sqlc.arg('after')::text
ORDER BY id
LIMIT sqlc.arg('limit')::int;

-- name: ListActiveCompositions :many
SELECT body, runtime, deletion_requested
FROM compositions
WHERE phase <> 'destroyed'
ORDER BY id;

-- name: UpdateCompositionObservation :execrows
UPDATE compositions
SET phase = $2, body = $3, runtime = $4
WHERE id = $1 AND generation = $5 AND deletion_requested = $6;

-- name: UpdateCompositionDeletion :exec
UPDATE compositions
SET generation = $2, deletion_requested = true, phase = $3, body = $4, runtime = $5
WHERE id = $1;

-- name: ListExpiredCompositionsForUpdate :many
SELECT body, runtime, deletion_requested
FROM compositions
WHERE expires_at <= $1
  AND NOT deletion_requested
  AND phase <> 'destroyed'
FOR UPDATE SKIP LOCKED;

-- name: UpdateCompositionDesired :exec
UPDATE compositions
SET generation = $2, phase = $3, body = $4, runtime = $5
WHERE id = $1;
