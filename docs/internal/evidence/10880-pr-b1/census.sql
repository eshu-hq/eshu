-- #10880 PR-B1 census over the supports table (active generations, static, source scopes).
\echo '== supports per canonical key (active)'
WITH s AS (
  SELECT sp.* FROM supply_chain_impact_finding_supports sp
  JOIN ingestion_scopes sc ON sc.scope_id = sp.scope_id AND sc.active_generation_id = sp.generation_id
  WHERE sp.scope_id <> 'operator:vulnerability_suppressions'
    AND NOT EXISTS (SELECT 1 FROM scanner_image_result_fact_classifications c WHERE c.finding_fact_id = sp.fact_id AND c.source_class = 'derived')
), per_key AS (SELECT canonical_key, count(*) n, count(DISTINCT scope_id) scopes FROM s GROUP BY 1)
SELECT n AS supports_per_key, count(*) AS keys FROM per_key GROUP BY 1 ORDER BY 1;
\echo '== (canonical_key, scope_id) pairs with more than one active support'
WITH s AS (
  SELECT sp.* FROM supply_chain_impact_finding_supports sp
  JOIN ingestion_scopes sc ON sc.scope_id = sp.scope_id AND sc.active_generation_id = sp.generation_id
  WHERE sp.scope_id <> 'operator:vulnerability_suppressions'
)
SELECT count(*) AS duplicate_key_scope_pairs FROM (SELECT canonical_key, scope_id FROM s GROUP BY 1,2 HAVING count(*) > 1) d;
\echo '== multi-support keys whose supports differ, by field'
WITH s AS (
  SELECT sp.* FROM supply_chain_impact_finding_supports sp
  JOIN ingestion_scopes sc ON sc.scope_id = sp.scope_id AND sc.active_generation_id = sp.generation_id
  WHERE sp.scope_id <> 'operator:vulnerability_suppressions'
    AND NOT EXISTS (SELECT 1 FROM scanner_image_result_fact_classifications c WHERE c.finding_fact_id = sp.fact_id AND c.source_class = 'derived')
), m AS (SELECT canonical_key FROM s GROUP BY 1 HAVING count(*) > 1)
SELECT count(*) AS multi_support_keys,
  count(*) FILTER (WHERE d_prio > 1) AS differ_priority,
  count(*) FILTER (WHERE d_img > 1) AS differ_image_ref,
  count(*) FILTER (WHERE d_match > 1) AS differ_match_reason,
  count(*) FILTER (WHERE d_prec > 1) AS differ_precise_eligibility,
  count(*) FILTER (WHERE d_fid > 1) AS differ_finding_id,
  count(*) FILTER (WHERE d_bucket > 1) AS differ_priority_bucket,
  count(*) FILTER (WHERE d_sev > 1) AS differ_severity_bucket
FROM (
  SELECT s.canonical_key, count(DISTINCT priority_score) d_prio, count(DISTINCT image_ref) d_img,
         count(DISTINCT match_reason) d_match, count(DISTINCT finding_id) d_fid,
         count(DISTINCT priority_bucket) d_bucket, count(DISTINCT severity_bucket) d_sev,
         count(DISTINCT (detection_profile = 'precise' OR detection_profile = '' AND impact_status IN ('affected_exact','not_affected_known_fixed')
           AND observed_version <> '' AND match_reason IN ('npm_semver_affected_range','npm_semver_known_fixed',
             'nuget_semver_affected_range','nuget_semver_known_fixed','cargo_semver_affected_range','cargo_semver_known_fixed',
             'hex_semver_affected_range','hex_semver_known_fixed','maven_range_match','maven_known_fixed',
             'swift_semver_affected_range','swift_semver_known_fixed'))) d_prec
  FROM s JOIN m USING (canonical_key) GROUP BY 1) q;
\echo '== stale-generation and derived-class supports present in the table'
SELECT count(*) FILTER (WHERE sc.active_generation_id IS DISTINCT FROM sp.generation_id) AS stale_generation_rows,
       count(*) FILTER (WHERE sp.scope_id = 'operator:vulnerability_suppressions') AS operator_rows,
       count(*) FILTER (WHERE EXISTS (SELECT 1 FROM scanner_image_result_fact_classifications c WHERE c.finding_fact_id = sp.fact_id AND c.source_class = 'derived')) AS derived_class_rows
FROM supply_chain_impact_finding_supports sp JOIN ingestion_scopes sc ON sc.scope_id = sp.scope_id;
