CREATE FUNCTION envy_notify_desired_change() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 PERFORM pg_notify('envy_desired_changed', NEW.id);
 RETURN NEW;
END;
$$;
CREATE TRIGGER envy_desired_change AFTER INSERT OR UPDATE OF generation, deletion_requested
 ON compositions FOR EACH ROW EXECUTE FUNCTION envy_notify_desired_change();
CREATE INDEX compositions_routing_domain ON compositions(project, baseline) WHERE phase <> 'destroyed';
