-- name: GetFrontendBinding :one
SELECT body FROM frontend_bindings WHERE project=$1 AND frontend=$2 AND revision=$3;

-- name: GetFrontendBindingForUpdate :one
SELECT body FROM frontend_bindings WHERE project=$1 AND frontend=$2 AND revision=$3 FOR UPDATE;

-- name: InsertFrontendBinding :execrows
INSERT INTO frontend_bindings(project,frontend,revision,composition,body) VALUES ($1,$2,$3,$4,$5)
ON CONFLICT (project,frontend,revision) DO NOTHING;

-- name: UpdateFrontendBinding :exec
UPDATE frontend_bindings SET body=$4 WHERE project=$1 AND frontend=$2 AND revision=$3;

-- name: CountFrontendBindings :one
SELECT count(*) FROM frontend_bindings WHERE composition=$1;

-- name: ListFrontendBindings :many
SELECT body FROM frontend_bindings WHERE composition=$1
AND frontend || ':' || revision > sqlc.arg('after')::text
ORDER BY frontend, revision LIMIT sqlc.arg('limit')::int;
