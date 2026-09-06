-- Keep legacy fields for inspection; reconciliation uses the new maps. Neither
-- Kubernetes identities nor generation/operation state changes during upgrade.
UPDATE compositions SET runtime = jsonb_set(runtime, '{Plan,Components}',
 jsonb_build_object(runtime->'Plan'->'Component'->>'id',runtime->'Plan'->'Component'))
 WHERE runtime->'Plan'->'Component'->>'id' IS NOT NULL
 AND NOT (runtime->'Plan' ? 'Components');
UPDATE compositions SET runtime = runtime || jsonb_build_object('Workloads',
 CASE WHEN COALESCE(runtime->'Workload'->>'Namespace','') <> '' THEN
 jsonb_build_object(runtime->'Plan'->'Component'->>'id',runtime->'Workload')
 ELSE '{}'::jsonb END)
 WHERE runtime->'Plan'->'Component'->>'id' IS NOT NULL
 AND NOT runtime ? 'Workloads';
-- Expand the installation-seeded demo approval to exercise entry/middle/leaf
-- routing. Borrowed baseline Deployments are never changed by this migration.
UPDATE components c SET body = c.body || jsonb_build_object('overridable',true,'env',
 jsonb_build_object('DOWNSTREAM_URL',CASE c.id WHEN 'gateway'
 THEN 'http://service-a.envy-baseline.svc.cluster.local:8080'
 ELSE 'http://service-b.envy-baseline.svc.cluster.local:8080' END))
 WHERE c.project='demo' AND c.id IN ('gateway','service-a')
 AND c.body->>'profile'='http-small' AND c.body->>'overridable'='false'
 AND EXISTS (SELECT 1 FROM baselines b WHERE b.project='demo' AND b.id='staging'
 AND b.body->>'revision'='demo-v1'
 AND b.body->'components'->c.id->>'service_host'=c.id||'.envy-baseline.svc.cluster.local');
