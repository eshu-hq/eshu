BEGIN;
SET TRANSACTION ISOLATION LEVEL REPEATABLE READ, READ ONLY;
SET LOCAL statement_timeout = '30s';
\timing on
SELECT 'cve_mcv' AS src, unnest(most_common_vals::text::text[]) AS v, unnest(most_common_freqs::text::float[]) AS f
FROM pg_stats WHERE tablename = 'supply_chain_impact_finding_supports' AND attname = 'cve_id' LIMIT 30;
SELECT 'adv_mcv' AS src, unnest(most_common_vals::text::text[]) AS v, unnest(most_common_freqs::text::float[]) AS f
FROM pg_stats WHERE tablename = 'supply_chain_impact_finding_supports' AND attname = 'advisory_id' LIMIT 30;
COMMIT;
