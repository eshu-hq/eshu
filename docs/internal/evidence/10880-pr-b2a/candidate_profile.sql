-- candidate_profile.sql: how many keys reach branch B (winner fails the precise filter) and how
-- many supports a probe can touch, by selector. Run against the corpus.
WITH w AS (
  SELECT w.*, (w.detection_profile = 'precise' OR (w.detection_profile = '' AND w.impact_status IN ('affected_exact','not_affected_known_fixed')
    AND w.observed_version <> '' AND w.match_reason IN ('npm_semver_affected_range','npm_semver_known_fixed','nuget_semver_affected_range','nuget_semver_known_fixed',
    'cargo_semver_affected_range','cargo_semver_known_fixed','hex_semver_affected_range','hex_semver_known_fixed','maven_range_match','maven_known_fixed',
    'swift_semver_affected_range','swift_semver_known_fixed'))) AS passes
  FROM supply_chain_impact_canonical_winners AS w
)
SELECT 'winners' AS what, count(*) AS n FROM w
UNION ALL SELECT 'winners passing precise (branch A)', count(*) FROM w WHERE passes
UNION ALL SELECT 'winners failing precise (branch B candidates, no selector)', count(*) FROM w WHERE NOT passes
UNION ALL SELECT 'candidates with status=affected_exact', count(*) FROM w WHERE NOT passes AND impact_status = 'affected_exact'
UNION ALL SELECT 'candidates status-prune (exact/known_fixed + observed version)', count(*) FROM w WHERE NOT passes AND impact_status IN ('affected_exact','not_affected_known_fixed') AND observed_version <> ''
UNION ALL SELECT 'candidates in the axios package', count(*) FROM w WHERE NOT passes AND package_id = 'pkg:npm/axios'
UNION ALL SELECT 'candidates for one CVE', count(*) FROM w WHERE NOT passes AND cve_id = 'CVE-2026-17'
UNION ALL SELECT 'candidates that CAN yield a precise pick (some precise-eligible support)',
  count(*) FROM w WHERE NOT passes AND EXISTS (SELECT 1 FROM supply_chain_impact_finding_supports s WHERE s.canonical_key = w.canonical_key AND
    (s.detection_profile = 'precise' OR (s.detection_profile = '' AND s.impact_status IN ('affected_exact','not_affected_known_fixed') AND s.observed_version <> '' AND s.match_reason IN ('npm_semver_affected_range','npm_semver_known_fixed','nuget_semver_affected_range','nuget_semver_known_fixed','cargo_semver_affected_range','cargo_semver_known_fixed','hex_semver_affected_range','hex_semver_known_fixed','maven_range_match','maven_known_fixed','swift_semver_affected_range','swift_semver_known_fixed'))));
SELECT 'max supports per key' AS what, max(source_count) AS n FROM supply_chain_impact_canonical_winners
UNION ALL SELECT 'mean supports per key', round(avg(source_count)) FROM supply_chain_impact_canonical_winners;
