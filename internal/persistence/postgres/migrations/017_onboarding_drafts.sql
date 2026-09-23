-- Preparation is private to its installation, project and authenticated author.
-- Drafts are never consulted by creation or reconciliation.
CREATE TABLE onboarding_drafts (
 installation_id text NOT NULL,
 project text NOT NULL,
 author text NOT NULL,
 revision bigint NOT NULL CHECK (revision > 0),
 body jsonb NOT NULL,
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (installation_id, project, author)
);
