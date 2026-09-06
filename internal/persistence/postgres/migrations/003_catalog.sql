-- Existing demo records acquire explicit profiles and routing bindings. New
-- registrations always supply these fields; catalog entries remain immutable.
UPDATE components SET body = body || '{"profile":"http-small","readiness_path":"/readyz"}'::jsonb
 WHERE NOT body ? 'profile';
UPDATE baselines SET body = body || '{"routing":{"namespace":"envy-baseline","gateway":"envy-preview","entry_component":"gateway"},"verification":{"kind":"envy-chain","chain":["gateway","service-a","service-b"]}}'::jsonb
 WHERE project='demo' AND id='staging' AND NOT body ? 'routing';
UPDATE compositions c SET runtime = c.runtime || jsonb_build_object('Plan',jsonb_build_object('Baseline',b.body,'Component',p.body))
 FROM baselines b, components p
 WHERE b.project=c.project AND b.id=c.baseline AND p.project=c.project
 AND c.body->'overrides' ? p.id AND NOT c.runtime ? 'Plan';
CREATE TABLE baseline_host_claims (
 host text PRIMARY KEY,
 project text NOT NULL,
 baseline text NOT NULL,
 FOREIGN KEY(project,baseline) REFERENCES baselines(project,id)
);
INSERT INTO baseline_host_claims(host,project,baseline)
 SELECT binding.value->>'service_host', b.project,b.id
 FROM baselines b, jsonb_each(b.body->'components') binding;
CREATE UNIQUE INDEX baseline_endpoint_unique ON baselines ((body->>'endpoint'));
