CREATE TABLE composition_update_idempotency (
    composition_id text NOT NULL REFERENCES compositions(id),
    key text NOT NULL,
    request_hash text NOT NULL,
    operation_id text NOT NULL REFERENCES operations(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (composition_id, key)
);
