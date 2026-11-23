-- gen_profile_patch.sql: make the corpus's detection_profile stamping realistic.
-- The writer stamps detection_profile ('precise' or 'comprehensive') on every
-- finding it classifies (core/findings_build.go:108); only findings written before
-- profile tagging carry ''. The read's precise filter therefore has two arms:
--   detection_profile = 'precise'                                  (stamped)
--   detection_profile = '' AND status/observed/match_reason test   (legacy)
-- This patch stamps 90% of supports and leaves 10% legacy, then rebuilds the
-- winners table. Run after gen_v_corpus.sql and gen_extras.sql, before the indexes.
\set ON_ERROR_STOP on
\timing on
DROP INDEX IF EXISTS supply_chain_impact_finding_supports_precise_key_idx;
DROP INDEX IF EXISTS supply_chain_impact_finding_supports_precise_key_ordered_idx;
UPDATE supply_chain_impact_finding_supports
SET detection_profile = CASE
  WHEN (hashtext(fact_id) & 2147483647) % 10 = 0 THEN ''
  WHEN impact_status IN ('affected_exact', 'not_affected_known_fixed') AND observed_version <> ''
       AND match_reason IN ('npm_semver_affected_range', 'npm_semver_known_fixed',
         'nuget_semver_affected_range', 'nuget_semver_known_fixed', 'cargo_semver_affected_range',
         'cargo_semver_known_fixed', 'hex_semver_affected_range', 'hex_semver_known_fixed',
         'maven_range_match', 'maven_known_fixed', 'swift_semver_affected_range', 'swift_semver_known_fixed')
    THEN 'precise'
  ELSE 'comprehensive' END
WHERE scope_id <> 'operator:vulnerability_suppressions';
TRUNCATE supply_chain_impact_canonical_winners;
INSERT INTO supply_chain_impact_canonical_winners (
  canonical_key, winner_fact_id, winner_scope_id, finding_id, priority_score, source_count,
  impact_status, ecosystem, severity_bucket, repository_id, cve_id, advisory_id, package_id,
  subject_digest, image_ref, priority_bucket, detection_profile, observed_version, match_reason,
  suppression_state, materialized_at, payload, source_confidence)
SELECT DISTINCT ON (s.canonical_key)
  s.canonical_key, s.fact_id, s.scope_id, s.finding_id, s.priority_score,
  count(*) OVER (PARTITION BY s.canonical_key)::int,
  s.impact_status, s.ecosystem, s.severity_bucket, s.repository_id, s.cve_id, s.advisory_id,
  s.package_id, s.subject_digest, s.image_ref, s.priority_bucket, s.detection_profile,
  s.observed_version, s.match_reason, s.suppression_state, now(), s.payload, s.source_confidence
FROM supply_chain_impact_finding_supports AS s
WHERE s.scope_id <> 'operator:vulnerability_suppressions'
ORDER BY s.canonical_key, s.priority_score DESC, s.has_payload_finding_id DESC, s.fact_id ASC;
VACUUM FULL supply_chain_impact_finding_supports;
VACUUM (ANALYZE) supply_chain_impact_canonical_winners;
ANALYZE supply_chain_impact_finding_supports;
