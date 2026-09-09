CREATE UNIQUE INDEX compositions_project_id ON compositions(project, id);
CREATE TABLE frontend_bindings (
  project text NOT NULL,
  frontend text NOT NULL,
  revision text NOT NULL,
  composition text NOT NULL,
  body jsonb NOT NULL,
  PRIMARY KEY (project, frontend, revision),
  FOREIGN KEY (project, composition) REFERENCES compositions(project, id)
);
CREATE INDEX frontend_bindings_composition ON frontend_bindings(composition, frontend, revision);
