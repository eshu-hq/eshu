-- v_indexes.sql: the partial indexes measured in RESULTS.md (migrations 343 and 344 in the
-- product). The precise predicate is the shipped read's two-arm filter, written once by
-- build_v_reads.py; the product derives it from the same Go constant as the read.
--
-- ORDERED form: passes the 1,000 ms bar (V list/count 754-846 ms warm at 8M supports).
CREATE INDEX IF NOT EXISTS supply_chain_impact_finding_supports_precise_key_ordered_idx
    ON supply_chain_impact_finding_supports (canonical_key, priority_score DESC, has_payload_finding_id DESC, fact_id ASC)
    WHERE (detection_profile = 'precise' OR (detection_profile = '' AND impact_status IN ('affected_exact', 'not_affected_known_fixed') AND observed_version <> '' AND match_reason IN ('npm_semver_affected_range', 'npm_semver_known_fixed', 'nuget_semver_affected_range', 'nuget_semver_known_fixed', 'cargo_semver_affected_range', 'cargo_semver_known_fixed', 'hex_semver_affected_range', 'hex_semver_known_fixed', 'maven_range_match', 'maven_known_fixed', 'swift_semver_affected_range', 'swift_semver_known_fixed')));
-- PLAIN form (the ruled shape): 5.5-5.9 s for the same reads. Measured, not recommended.
-- CREATE INDEX supply_chain_impact_finding_supports_precise_key_idx
--     ON supply_chain_impact_finding_supports (canonical_key) WHERE <same predicate>;
CREATE INDEX IF NOT EXISTS supply_chain_impact_finding_supports_image_ref_idx
    ON supply_chain_impact_finding_supports (image_ref)
    WHERE image_ref <> '';
