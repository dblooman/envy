-- name: ListLifecycleEvents :many
SELECT id, occurred_at, kind, body
FROM lifecycle_events
WHERE composition_id = $1 AND id > $2
ORDER BY id
LIMIT $3;
