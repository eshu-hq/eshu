-- gen_extras.sql: objects the reads need that the corpus generator does not make.
-- Run after gen_v_corpus.sql and before the partial indexes are created.
\set ON_ERROR_STOP on
\timing on

-- Repository -> service map for the grouped inventory read (3,000 repositories,
-- 300 services, every tenth repository also in one of 50 shared services).
CREATE TABLE IF NOT EXISTS b2a_service_map (repository_id text NOT NULL, dim_id text NOT NULL);
TRUNCATE b2a_service_map;
INSERT INTO b2a_service_map
SELECT 'repo:' || r, 'service:' || (r % 300) FROM generate_series(0, 2999) AS r
UNION ALL
SELECT 'repo:' || r, 'service:shared' || (r % 50) FROM generate_series(0, 2999, 10) AS r;
CREATE INDEX IF NOT EXISTS b2a_service_map_repo_idx ON b2a_service_map (repository_id);
ANALYZE b2a_service_map;

-- One hot image reference on about 2% of the supports (about 160,000), to size the
-- image read at its worst. QA's real maximum per image is a census item.
UPDATE supply_chain_impact_finding_supports
SET image_ref = 'registry.example/hot:1'
WHERE (hashtext(fact_id) & 2147483647) % 50 = 0 AND fact_id LIKE 'fact:%:%' AND scope_id <> 'operator:vulnerability_suppressions';
ANALYZE supply_chain_impact_finding_supports;
