ALTER TABLE activity_events ADD COLUMN operation_id text NOT NULL DEFAULT '';
CREATE UNIQUE INDEX activity_events_operation_outcome ON activity_events(operation_id, outcome) WHERE operation_id <> '';
