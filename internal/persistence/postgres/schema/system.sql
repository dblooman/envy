-- System catalog views and runtime metadata tables not managed by migration scripts

CREATE VIEW pg_locks AS SELECT
    ''::text AS locktype,
    0 AS pid,
    0 AS classid,
    0::oid AS objid,
    0 AS objsubid,
    false AS granted;

CREATE TABLE IF NOT EXISTS envy_schema_migrations (
    name text PRIMARY KEY,
    applied_at timestamptz NOT NULL DEFAULT now()
);
