-- name: SeedProject :exec
INSERT INTO projects(id, body)
VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: SeedComponent :exec
INSERT INTO components(project, id, body)
VALUES ($1, $2, $3)
ON CONFLICT DO NOTHING;

-- name: SeedBaseline :exec
INSERT INTO baselines(project, id, body)
VALUES ($1, $2, $3)
ON CONFLICT DO NOTHING;

-- name: SeedBaselineHostClaim :exec
INSERT INTO baseline_host_claims(host, project, baseline)
VALUES ($1, $2, $3)
ON CONFLICT DO NOTHING;

-- name: CheckProjectExists :one
SELECT EXISTS(
    SELECT 1 FROM projects WHERE id = $1
);

-- name: GetComponent :one
SELECT body FROM components
WHERE project = $1 AND id = $2;

-- name: GetBaseline :one
SELECT body FROM baselines
WHERE project = $1 AND id = $2;

-- name: ListProjects :many
SELECT body FROM projects
WHERE id > $1
ORDER BY id
LIMIT $2;

-- name: ListComponents :many
SELECT body FROM components
WHERE project = $1 AND id > $2
ORDER BY id
LIMIT $3;

-- name: ListBaselines :many
SELECT body FROM baselines
WHERE project = $1 AND id > $2
ORDER BY id
LIMIT $3;

-- name: InsertProject :exec
INSERT INTO projects(id, body)
VALUES ($1, $2);

-- name: InsertComponent :exec
INSERT INTO components(project, id, body)
VALUES ($1, $2, $3);

-- name: InsertBaseline :exec
INSERT INTO baselines(project, id, body)
VALUES ($1, $2, $3);

-- name: InsertBaselineHostClaim :exec
INSERT INTO baseline_host_claims(host, project, baseline)
VALUES ($1, $2, $3);
