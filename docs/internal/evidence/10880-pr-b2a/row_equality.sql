-- row_equality.sql: the supplement result (V branch B picks) three ways, compared with
-- EXCEPT in both directions (all must be 0 rows):
--   probe_ordered  : the scalar-subquery probe over the ordered index (the product form)
--   probe_join     : the join form (eligibility as joins) over the same index
-- The set-based reference (DISTINCT ON over every precise support of the candidates) was dropped:
-- it exhausted the host's memory. Run with the ordered index present; then run
-- `row_equality.sh <dsn> plain`, which swaps in the plain index and compares its probe result
-- with the saved ordered result (row_equality_plain.sql).
\set ON_ERROR_STOP on
\timing on
CREATE TEMP TABLE b2a_cand AS
SELECT w.canonical_key FROM supply_chain_impact_canonical_winners w
WHERE w.impact_status IN ('affected_exact', 'not_affected_known_fixed') AND w.observed_version <> ''
  AND NOT (w.detection_profile = 'precise' OR (w.detection_profile = '' AND w.impact_status IN ('affected_exact', 'not_affected_known_fixed')
    AND w.observed_version <> '' AND w.match_reason IN ('npm_semver_affected_range','npm_semver_known_fixed','nuget_semver_affected_range','nuget_semver_known_fixed',
    'cargo_semver_affected_range','cargo_semver_known_fixed','hex_semver_affected_range','hex_semver_known_fixed','maven_range_match','maven_known_fixed',
    'swift_semver_affected_range','swift_semver_known_fixed')));
SELECT count(*) AS candidates FROM b2a_cand;

CREATE TEMP TABLE probe_ordered AS
SELECT c.canonical_key, p.fact_id FROM b2a_cand c CROSS JOIN LATERAL (
  SELECT s.fact_id FROM supply_chain_impact_finding_supports s
  WHERE s.canonical_key = c.canonical_key AND :PRECISE_S AND s.scope_id <> 'operator:vulnerability_suppressions'
    AND COALESCE((SELECT TRUE FROM ingestion_scopes sc JOIN scope_generations g ON g.scope_id = sc.scope_id AND g.generation_id = sc.active_generation_id
                  WHERE sc.scope_id = s.scope_id AND sc.active_generation_id = s.generation_id AND g.status = 'active' LIMIT 1), FALSE)
    AND NOT EXISTS (SELECT 1 FROM scanner_image_result_fact_associations a WHERE a.finding_fact_id = s.fact_id)
    AND s.scope_id NOT LIKE 'image://derived/%'
    AND COALESCE((SELECT k.source_class = 'static' AND k.lifecycle = 'active' FROM scanner_image_result_fact_classifications k WHERE k.finding_fact_id = s.fact_id LIMIT 1), TRUE)
  ORDER BY s.priority_score DESC, s.has_payload_finding_id DESC, s.fact_id ASC LIMIT 1) p;

CREATE TEMP TABLE probe_join AS
SELECT c.canonical_key, p.fact_id FROM b2a_cand c CROSS JOIN LATERAL (
  SELECT s.fact_id FROM (SELECT s0.* FROM supply_chain_impact_finding_supports s0 WHERE s0.canonical_key = c.canonical_key AND :PRECISE_S0 AND s0.scope_id <> 'operator:vulnerability_suppressions' OFFSET 0) s
  JOIN ingestion_scopes sc ON sc.scope_id = s.scope_id AND sc.active_generation_id = s.generation_id
  JOIN scope_generations g ON g.scope_id = s.scope_id AND g.generation_id = s.generation_id
  LEFT JOIN scanner_image_result_fact_classifications k ON k.finding_fact_id = s.fact_id
  LEFT JOIN scanner_image_result_fact_associations a ON a.finding_fact_id = s.fact_id
  WHERE g.status = 'active' AND a.finding_fact_id IS NULL AND s.scope_id NOT LIKE 'image://derived/%'
    AND (k.finding_fact_id IS NULL OR (k.source_class = 'static' AND k.lifecycle = 'active'))
  ORDER BY s.priority_score DESC, s.has_payload_finding_id DESC, s.fact_id ASC LIMIT 1) p;

SELECT 'rows ordered/join' AS what, (SELECT count(*) FROM probe_ordered), (SELECT count(*) FROM probe_join);
SELECT 'ordered EXCEPT join' AS what, count(*) FROM (SELECT * FROM probe_ordered EXCEPT SELECT * FROM probe_join) x
UNION ALL SELECT 'join EXCEPT ordered', count(*) FROM (SELECT * FROM probe_join EXCEPT SELECT * FROM probe_ordered) x
;
-- The probe result is kept for the plain-index comparison.
CREATE TABLE IF NOT EXISTS b2a_probe_saved (canonical_key text, fact_id text);
TRUNCATE b2a_probe_saved;
INSERT INTO b2a_probe_saved SELECT * FROM probe_ordered;
