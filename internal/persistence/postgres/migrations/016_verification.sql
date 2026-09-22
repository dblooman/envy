CREATE TABLE verification_evidence (
 id bigserial PRIMARY KEY,
 composition_id text NOT NULL REFERENCES compositions(id) ON DELETE CASCADE,
 body jsonb NOT NULL
);
CREATE INDEX verification_composition ON verification_evidence(composition_id, id);
