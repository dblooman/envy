-- name: CheckMigrationApplied :one
SELECT EXISTS(
    SELECT 1 FROM envy_schema_migrations WHERE name = $1
);

-- name: RecordMigration :exec
INSERT INTO envy_schema_migrations(name) VALUES ($1);
