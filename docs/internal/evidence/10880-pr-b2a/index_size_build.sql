-- index_size_build.sql: size and build time of the two partial indexes at two scales four times
-- apart: the full 8,000,481-supports table and a 2M copy (keys 1..20000, 2,000,000+ supports).
-- Builds are plain CREATE INDEX (not CONCURRENTLY) on an idle table with
-- maintenance_work_mem=512MB; a concurrent build is slower. The precise predicate is the read's.
\set ON_ERROR_STOP on
\timing on
SELECT current_setting('maintenance_work_mem') AS maintenance_work_mem, count(*) AS supports_8m FROM supply_chain_impact_finding_supports;
CREATE INDEX b2a_sz_ordered_8m ON supply_chain_impact_finding_supports (canonical_key, priority_score DESC, has_payload_finding_id DESC, fact_id ASC) WHERE (detection_profile = 'precise' OR (detection_profile = '' AND impact_status IN ('affected_exact', 'not_affected_known_fixed') AND observed_version <> '' AND match_reason IN ('npm_semver_affected_range', 'npm_semver_known_fixed', 'nuget_semver_affected_range', 'nuget_semver_known_fixed', 'cargo_semver_affected_range', 'cargo_semver_known_fixed', 'hex_semver_affected_range', 'hex_semver_known_fixed', 'maven_range_match', 'maven_known_fixed', 'swift_semver_affected_range', 'swift_semver_known_fixed')));
CREATE INDEX b2a_sz_image_8m ON supply_chain_impact_finding_supports (image_ref) WHERE image_ref <> '';
CREATE UNLOGGED TABLE b2a_supports_2m AS
  SELECT * FROM supply_chain_impact_finding_supports
  WHERE canonical_key ~ '^key:[0-9]+$' AND substring(canonical_key from 5)::int <= 20000;
ANALYZE b2a_supports_2m;
CREATE INDEX b2a_sz_ordered_2m ON b2a_supports_2m (canonical_key, priority_score DESC, has_payload_finding_id DESC, fact_id ASC) WHERE (detection_profile = 'precise' OR (detection_profile = '' AND impact_status IN ('affected_exact', 'not_affected_known_fixed') AND observed_version <> '' AND match_reason IN ('npm_semver_affected_range', 'npm_semver_known_fixed', 'nuget_semver_affected_range', 'nuget_semver_known_fixed', 'cargo_semver_affected_range', 'cargo_semver_known_fixed', 'hex_semver_affected_range', 'hex_semver_known_fixed', 'maven_range_match', 'maven_known_fixed', 'swift_semver_affected_range', 'swift_semver_known_fixed')));
CREATE INDEX b2a_sz_image_2m ON b2a_supports_2m (image_ref) WHERE image_ref <> '';
SELECT 'supports' AS what, '8M' AS scale, count(*) AS rows, pg_size_pretty(pg_relation_size('supply_chain_impact_finding_supports')) AS heap FROM supply_chain_impact_finding_supports
UNION ALL SELECT 'supports', '2M', count(*), pg_size_pretty(pg_relation_size('b2a_supports_2m')) FROM b2a_supports_2m;
SELECT c.relname AS index, pg_size_pretty(pg_relation_size(c.oid)) AS size, pg_relation_size(c.oid) AS bytes FROM pg_class c WHERE c.relname LIKE 'b2a_sz_%' ORDER BY 1;
SELECT 'precise-eligible 8M' AS what, count(*) FROM supply_chain_impact_finding_supports WHERE (detection_profile = 'precise' OR (detection_profile = '' AND impact_status IN ('affected_exact', 'not_affected_known_fixed') AND observed_version <> '' AND match_reason IN ('npm_semver_affected_range', 'npm_semver_known_fixed', 'nuget_semver_affected_range', 'nuget_semver_known_fixed', 'cargo_semver_affected_range', 'cargo_semver_known_fixed', 'hex_semver_affected_range', 'hex_semver_known_fixed', 'maven_range_match', 'maven_known_fixed', 'swift_semver_affected_range', 'swift_semver_known_fixed')))
UNION ALL SELECT 'precise-eligible 2M', count(*) FROM b2a_supports_2m WHERE (detection_profile = 'precise' OR (detection_profile = '' AND impact_status IN ('affected_exact', 'not_affected_known_fixed') AND observed_version <> '' AND match_reason IN ('npm_semver_affected_range', 'npm_semver_known_fixed', 'nuget_semver_affected_range', 'nuget_semver_known_fixed', 'cargo_semver_affected_range', 'cargo_semver_known_fixed', 'hex_semver_affected_range', 'hex_semver_known_fixed', 'maven_range_match', 'maven_known_fixed', 'swift_semver_affected_range', 'swift_semver_known_fixed')));
DROP TABLE b2a_supports_2m;
DROP INDEX b2a_sz_ordered_8m;
DROP INDEX b2a_sz_image_8m;
