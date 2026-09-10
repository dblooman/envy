CREATE TABLE source_repositories (
 project text NOT NULL REFERENCES projects(id),
 id text NOT NULL,
 body jsonb NOT NULL,
 PRIMARY KEY(project,id)
);
CREATE UNIQUE INDEX source_repository_identity ON source_repositories(project, (body->>'github_repository'));
CREATE TABLE source_component_claims (
 project text NOT NULL,
 component text NOT NULL,
 repository text NOT NULL,
 PRIMARY KEY(project,component),
 FOREIGN KEY(project,component) REFERENCES components(project,id),
 FOREIGN KEY(project,repository) REFERENCES source_repositories(project,id)
);
CREATE TABLE builds (
 id text PRIMARY KEY,
 project text NOT NULL,
 repository text NOT NULL,
 component text NOT NULL,
 revision text NOT NULL,
 body jsonb NOT NULL,
 FOREIGN KEY(project,repository) REFERENCES source_repositories(project,id)
);
CREATE INDEX builds_lookup ON builds(project,repository,component,revision,id);
