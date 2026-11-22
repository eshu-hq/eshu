-- Measurement shim for #11208, not a gate. Adds SSM facts to the seed.sql
-- fixture: 20 AWS scopes x 10 hosts x (1 platform + 300 application) rows in the
-- active generation, plus the same in the superseded generation of scopes
-- where scope_number % 5 < 2. Needs :naws >= 20.
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload)
SELECT 'ssm-' || s || '-' || h || '-' || r || '-' || g, 'scope-' || s, 'gen-' || s || '-' || g,
  CASE WHEN r = 0 THEN 'aws_ssm_host_platform' ELSE 'aws_ssm_host_application' END,
  'sk-' || s || '-' || h || '-' || r || '-' || g, 'aws', 'sf', now(), now(),
  jsonb_build_object('partition', 'aws', 'account_id', 'acct' || s, 'region', 'us-east-1',
                     'instance_id', 'i-' || s || '-' || h, 'name', 'app' || r)
FROM generate_series(1, 20) s, generate_series(1, 10) h, generate_series(0, 300) r, (VALUES ('a'), ('s')) v(g)
WHERE (g = 'a' OR s % 5 < 2);
VACUUM (ANALYZE) fact_records;
SELECT count(*) AS ssm_fact_rows FROM fact_records WHERE fact_kind LIKE 'aws_ssm_host%';
