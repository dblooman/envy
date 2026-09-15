-- Authentication state is independent of desired-state and activity retention.
CREATE TABLE envy_auth_records (
    kind text NOT NULL,
    key text NOT NULL,
    body jsonb NOT NULL,
    expires_at timestamptz NOT NULL,
    PRIMARY KEY (kind, key)
);
CREATE INDEX envy_auth_records_expiry ON envy_auth_records (expires_at);
