-- Measurement shim for #11208, not a migration. The candidate index the arbiter
-- rejected in #11208 (retired evidence record); kept
-- only to reproduce the measurement. Do not add it to the schema from this file.
CREATE INDEX perf11208_ssm_idx ON fact_records (scope_id, generation_id)
    WHERE fact_kind IN ('aws_ssm_host_application', 'aws_ssm_host_platform');
VACUUM (ANALYZE) fact_records;
SELECT pg_size_pretty(pg_relation_size('perf11208_ssm_idx')) AS index_size;
