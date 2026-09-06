CREATE TABLE lifecycle_events (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 composition_id text NOT NULL REFERENCES compositions(id),
 occurred_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 kind text NOT NULL,
 body jsonb NOT NULL
);
CREATE INDEX lifecycle_events_composition ON lifecycle_events(composition_id,id);

-- Deliberately exclude updated_at, backoff, and workload polling metadata.
CREATE FUNCTION envy_event_state(body jsonb) RETURNS jsonb
LANGUAGE sql IMMUTABLE AS $$
 SELECT jsonb_build_object(
  'composition',body->'id','project',body->'project',
  'generation',body->'generation','phase',body->'phase',
  'operation',body->'latest_operation','conditions',body->'conditions',
  'error',body->'last_error'
 )
$$;

CREATE FUNCTION envy_record_lifecycle_event() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE event_kind text;
BEGIN
 IF TG_OP = 'INSERT' THEN
  event_kind := 'create_requested';
 ELSIF NEW.generation <> OLD.generation THEN
  IF NEW.deletion_requested AND NEW.runtime->>'DeletionReason' = 'expired' THEN
   event_kind := 'expired';
  ELSE
   event_kind := (NEW.body->'latest_operation'->>'kind') || '_requested';
  END IF;
 ELSIF envy_event_state(NEW.body) IS DISTINCT FROM envy_event_state(OLD.body) THEN
  event_kind := 'observation_changed';
 ELSE
  RETURN NEW;
 END IF;
 INSERT INTO lifecycle_events(composition_id,kind,body)
 VALUES(NEW.id,event_kind,envy_event_state(NEW.body));
 RETURN NEW;
END
$$;

INSERT INTO lifecycle_events(composition_id,kind,body)
 SELECT id,'snapshot',envy_event_state(body) FROM compositions ORDER BY id;
CREATE TRIGGER envy_composition_events AFTER INSERT OR UPDATE ON compositions
 FOR EACH ROW EXECUTE FUNCTION envy_record_lifecycle_event();
