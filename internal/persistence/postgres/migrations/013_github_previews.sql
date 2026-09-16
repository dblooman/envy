CREATE TABLE github_preview_policies (
 project text NOT NULL, repository text NOT NULL, github_repository_id bigint NOT NULL,
 enabled boolean NOT NULL, body jsonb NOT NULL,
 PRIMARY KEY(project, repository),
 FOREIGN KEY(project,repository) REFERENCES source_repositories(project,id)
);
CREATE UNIQUE INDEX github_preview_enabled_repository ON github_preview_policies(github_repository_id) WHERE enabled;
CREATE TABLE github_pr_previews (
 id text PRIMARY KEY, project text NOT NULL, repository text NOT NULL,
 number integer NOT NULL, version bigint NOT NULL DEFAULT 1,
 composition_id text UNIQUE REFERENCES compositions(id), body jsonb NOT NULL,
 UNIQUE(project,repository,number),
 FOREIGN KEY(project,repository) REFERENCES source_repositories(project,id)
);
CREATE TABLE github_preview_history (
 preview_id text NOT NULL REFERENCES github_pr_previews(id), lifecycle bigint NOT NULL,
 body jsonb NOT NULL, PRIMARY KEY(preview_id,lifecycle)
);
CREATE TABLE github_webhook_deliveries (
 id text PRIMARY KEY, event text NOT NULL, body jsonb NOT NULL,
 received_at timestamptz NOT NULL DEFAULT now(), processed_at timestamptz,
 error text NOT NULL DEFAULT ''
);
CREATE TABLE github_preview_health (
 id boolean PRIMARY KEY DEFAULT true CHECK(id),
 reconciled_at timestamptz, error text NOT NULL DEFAULT ''
);
