CREATE TABLE preview_profiles (
 project text NOT NULL,
 baseline text NOT NULL,
 component text NOT NULL,
 revision bigint NOT NULL CHECK (revision > 0),
 body jsonb NOT NULL,
 PRIMARY KEY (project,baseline,component,revision),
 FOREIGN KEY (project,baseline) REFERENCES baselines(project,id),
 FOREIGN KEY (project,component) REFERENCES components(project,id)
);
