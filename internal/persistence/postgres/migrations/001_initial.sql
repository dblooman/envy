CREATE TABLE IF NOT EXISTS projects (
  id text PRIMARY KEY,
  body jsonb NOT NULL
);
CREATE TABLE IF NOT EXISTS components (
  project text NOT NULL REFERENCES projects(id),
  id text NOT NULL,
  body jsonb NOT NULL,
  PRIMARY KEY (project, id)
);
CREATE TABLE IF NOT EXISTS baselines (
  project text NOT NULL REFERENCES projects(id),
  id text NOT NULL,
  body jsonb NOT NULL,
  PRIMARY KEY (project, id)
);
CREATE TABLE IF NOT EXISTS compositions (
  id text PRIMARY KEY,
  project text NOT NULL REFERENCES projects(id),
  baseline text NOT NULL,
  generation bigint NOT NULL CHECK (generation > 0),
  deletion_requested boolean NOT NULL DEFAULT false,
  phase text NOT NULL,
  expires_at timestamptz NOT NULL,
  body jsonb NOT NULL,
  runtime jsonb NOT NULL,
  FOREIGN KEY (project, baseline) REFERENCES baselines(project, id)
);
CREATE INDEX IF NOT EXISTS compositions_active ON compositions(phase, expires_at);
CREATE INDEX IF NOT EXISTS compositions_project ON compositions(project, id);
CREATE TABLE IF NOT EXISTS idempotency_keys (
  key text PRIMARY KEY,
  request_hash text NOT NULL,
  composition_id text NOT NULL REFERENCES compositions(id)
);
CREATE TABLE IF NOT EXISTS operations (
  id text PRIMARY KEY,
  composition_id text NOT NULL REFERENCES compositions(id),
  body jsonb NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now()
);
