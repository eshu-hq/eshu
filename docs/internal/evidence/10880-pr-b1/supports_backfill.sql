-- generated from CanonicalFindingKeySQL / severityBucketFactSQL: backfill of the supports table from active-or-stale finding facts.
INSERT INTO supply_chain_impact_finding_supports (
  fact_id, canonical_key, scope_id, generation_id, finding_id, has_payload_finding_id, priority_score,
  source_confidence, suppression_state, impact_status, ecosystem, severity_bucket, repository_id, cve_id,
  advisory_id, package_id, subject_digest, image_ref, priority_bucket, detection_profile, observed_version,
  match_reason, payload)
SELECT fact.fact_id,
       CONCAT_WS('|',
         COALESCE(NULLIF(fact.payload->>'cve_id', ''), NULLIF(fact.payload->>'advisory_id', ''), ''),
         COALESCE(fact.payload->>'advisory_id', ''),
         COALESCE(fact.payload->>'package_id', ''),
         COALESCE(fact.payload->>'purl', ''),
         COALESCE(fact.payload->>'product_criteria', ''),
         COALESCE(fact.payload->>'match_criteria_id', ''),
         COALESCE(fact.payload->>'observed_version', ''),
         COALESCE(fact.payload->>'requested_range', ''),
         COALESCE(fact.payload->>'impact_status', ''),
         COALESCE(fact.payload->>'repository_id', ''),
         COALESCE(fact.payload->>'subject_digest', '')
       ),
       fact.scope_id, fact.generation_id,
       COALESCE(
         NULLIF(fact.payload->>'finding_id', ''),
         CONCAT_WS('|',
         COALESCE(NULLIF(fact.payload->>'cve_id', ''), NULLIF(fact.payload->>'advisory_id', ''), ''),
         COALESCE(fact.payload->>'advisory_id', ''),
         COALESCE(fact.payload->>'package_id', ''),
         COALESCE(fact.payload->>'purl', ''),
         COALESCE(fact.payload->>'product_criteria', ''),
         COALESCE(fact.payload->>'match_criteria_id', ''),
         COALESCE(fact.payload->>'observed_version', ''),
         COALESCE(fact.payload->>'requested_range', ''),
         COALESCE(fact.payload->>'impact_status', ''),
         COALESCE(fact.payload->>'repository_id', ''),
         COALESCE(fact.payload->>'subject_digest', '')
       )
       ),
       CASE
         WHEN NULLIF(fact.payload->>'finding_id', '') IS NULL THEN 0
         ELSE 1
       END,
       COALESCE(NULLIF(fact.payload->>'priority_score', '')::int, 0),
       fact.source_confidence,
       COALESCE(NULLIF(fact.payload->>'suppression_state', ''), 'active'),
       COALESCE(fact.payload->>'impact_status', ''),
       COALESCE(LOWER((fact.payload->>'ecosystem') COLLATE "C") COLLATE "default", ''),
       CASE
    WHEN COALESCE(NULLIF(fact.payload->>'cvss_score', '')::numeric, 0) >= 9.0 THEN 'critical'
    WHEN COALESCE(NULLIF(fact.payload->>'cvss_score', '')::numeric, 0) >= 7.0 THEN 'high'
    WHEN COALESCE(NULLIF(fact.payload->>'cvss_score', '')::numeric, 0) >= 4.0 THEN 'medium'
    WHEN COALESCE(NULLIF(fact.payload->>'cvss_score', '')::numeric, 0) > 0.0 THEN 'low'
    ELSE CASE lower(btrim(fact.payload #>> '{provenance,selected_severity_label}'))
        WHEN 'critical' THEN 'critical'
        WHEN 'high' THEN 'high'
        WHEN 'important' THEN 'high'
        WHEN 'medium' THEN 'medium'
        WHEN 'moderate' THEN 'medium'
        WHEN 'low' THEN 'low'
        ELSE 'none'
    END
  END,
       COALESCE(fact.payload->>'repository_id', ''), COALESCE(fact.payload->>'cve_id', ''),
       COALESCE(fact.payload->>'advisory_id', ''), COALESCE(fact.payload->>'package_id', ''),
       COALESCE(fact.payload->>'subject_digest', ''), COALESCE(fact.payload->>'image_ref', ''),
       COALESCE(fact.payload->>'priority_bucket', ''), COALESCE(fact.payload->>'detection_profile', ''),
       COALESCE(fact.payload->>'observed_version', ''), COALESCE(fact.payload->>'match_reason', ''),
       fact.payload
FROM fact_records AS fact
WHERE fact.fact_kind = 'reducer_supply_chain_impact_finding' AND fact.is_tombstone = FALSE
ON CONFLICT (fact_id) DO NOTHING;
