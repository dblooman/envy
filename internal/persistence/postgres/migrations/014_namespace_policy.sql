ALTER TABLE installation_profile ADD COLUMN namespace_policy_mode text NOT NULL DEFAULT 'legacy'
 CHECK (namespace_policy_mode IN ('legacy', 'isolated'));
ALTER TABLE installation_profile ALTER COLUMN namespace_policy_mode SET DEFAULT 'isolated';
ALTER TABLE installation_profile ADD COLUMN namespace_policy_fingerprint text NOT NULL DEFAULT '';
