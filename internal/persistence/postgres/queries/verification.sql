-- name: LatestVerification :many
SELECT id, body FROM verification_evidence WHERE composition_id = $1 ORDER BY id DESC LIMIT 1;

-- name: InsertVerification :exec
INSERT INTO verification_evidence(composition_id, body) VALUES ($1, $2);

-- name: UpdateVerification :exec
UPDATE verification_evidence SET body = $2 WHERE id = $1;

-- name: ListVerification :many
SELECT id, body FROM verification_evidence
WHERE composition_id = $1 AND (sqlc.arg('before')::bigint = 0 OR id < sqlc.arg('before')::bigint)
ORDER BY id DESC LIMIT sqlc.arg('limit')::int;
