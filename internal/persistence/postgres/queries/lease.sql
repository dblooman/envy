-- name: TryAdvisoryLock :one
SELECT pg_try_advisory_lock($1);

-- name: CheckAdvisoryLock :one
SELECT EXISTS(
    SELECT 1 FROM pg_locks
    WHERE locktype = 'advisory'
      AND pid = pg_backend_pid()
      AND classid = 0
      AND objid = ($1::bigint)::oid
      AND objsubid = 1
      AND granted
);

-- name: AdvisoryUnlock :one
SELECT pg_advisory_unlock($1);
