-- name: GetSourceRepository :one
SELECT body FROM source_repositories WHERE project=$1 AND id=$2;
-- name: LockSourceRepository :one
SELECT body FROM source_repositories WHERE project=$1 AND id=$2 FOR SHARE;
-- name: ListSourceRepositories :many
SELECT body FROM source_repositories WHERE project=$1 AND id > sqlc.arg(after) ORDER BY id LIMIT sqlc.arg('limit');
-- name: InsertSourceRepository :execrows
INSERT INTO source_repositories(project,id,body) VALUES ($1,$2,$3) ON CONFLICT DO NOTHING;
-- name: EnableSourceRepository :one
UPDATE source_repositories SET body=jsonb_set(body,'{enabled}',to_jsonb(sqlc.arg(enabled)::boolean)) WHERE project=$1 AND id=$2 RETURNING body;
-- name: ClaimSourceComponent :exec
INSERT INTO source_component_claims(project,component,repository) VALUES($1,$2,$3);
-- name: InsertBuild :execrows
INSERT INTO builds(id,project,repository,component,revision,body) VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING;
-- name: GetBuild :one
SELECT body FROM builds WHERE project=$1 AND id=$2;
-- name: ListBuilds :many
SELECT body FROM builds WHERE project=$1 AND repository=$2 AND component=$3 AND revision=$4 AND id > sqlc.arg(after) ORDER BY id LIMIT sqlc.arg('limit');
