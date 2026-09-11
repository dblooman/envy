CREATE TABLE activity_events (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    occurred_at timestamptz NOT NULL DEFAULT now(),
    project text NOT NULL DEFAULT '',
    actor_id text NOT NULL,
    action text NOT NULL,
    outcome text NOT NULL,
    resource_type text NOT NULL,
    resource_id text NOT NULL,
    body jsonb NOT NULL
);

CREATE INDEX activity_events_project_id_idx ON activity_events(project, id);
CREATE INDEX activity_events_actor_id_idx ON activity_events(actor_id, id);
CREATE INDEX activity_events_resource_idx ON activity_events(resource_type, resource_id, id);

CREATE TABLE composition_revisions (
    composition_id text NOT NULL REFERENCES compositions(id) ON DELETE CASCADE,
    generation bigint NOT NULL,
    project text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    body jsonb NOT NULL,
    PRIMARY KEY (composition_id, generation)
);

CREATE INDEX composition_revisions_project_idx ON composition_revisions(project, composition_id, generation);
