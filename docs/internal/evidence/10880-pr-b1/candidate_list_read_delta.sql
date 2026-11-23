-- #10880 PR-B1 theory proof: candidate scoped list read (final shape: grant legs + lateral override probe).
-- Generated from listFindingsQueryTemplate by zz_b1_candidate_scratch_test.go; $1..$30 keep the legacy bind order.
-- 1. source_candidates + operator_candidates replace the legacy CTEs of the same names:
source_candidates AS (
  SELECT fact.fact_id, fact.scope_id, fact.finding_id, fact.source_confidence,
         fact.suppression_state, fact.priority_score,
         fact.has_payload_finding_id, fact.canonical_key
  FROM (SELECT * FROM supply_chain_impact_finding_supports WHERE scope_id = ANY($23::text[])
   UNION ALL
   SELECT * FROM supply_chain_impact_finding_supports WHERE repository_id = ANY($22::text[])
     AND NOT COALESCE(scope_id = ANY($23::text[]), FALSE)) AS fact
  JOIN ingestion_scopes AS scope
    ON scope.scope_id = fact.scope_id
   AND scope.active_generation_id = fact.generation_id
  JOIN scope_generations AS generation
    ON generation.scope_id = fact.scope_id
   AND generation.generation_id = fact.generation_id

  LEFT JOIN scanner_image_result_fact_classifications AS finding_class
    ON finding_class.finding_fact_id = fact.fact_id
  LEFT JOIN scanner_image_result_fact_associations AS finding_association
    ON finding_association.finding_fact_id = fact.fact_id
  WHERE $1::text IS NOT NULL
    AND generation.status = 'active'
    AND fact.scope_id <> 'operator:vulnerability_suppressions'
 AND $25::boolean IS NOT NULL AND $26::text IS NOT NULL AND $27::text IS NOT NULL AND $28::text IS NOT NULL AND $29::text IS NOT NULL AND $30::text[] IS NOT NULL

    AND finding_association.finding_fact_id IS NULL
    AND fact.scope_id NOT LIKE 'image://derived/%'
    AND (finding_class.finding_fact_id IS NULL
         OR (finding_class.source_class = 'static' AND finding_class.lifecycle = 'active'))
    AND ($2 = '' OR fact.cve_id = $2)
    AND ($3 = '' OR fact.package_id = $3)
    AND ($4 = '' OR fact.repository_id = $4)
    AND ($5 = '' OR fact.subject_digest = $5)
    AND ($6 = '' OR fact.impact_status = $6)
    AND ($7 = '' OR fact.advisory_id = $7)
    AND ($8 = '' OR fact.ecosystem = LOWER(($8) COLLATE "C") COLLATE "default")

  AND (
        $9 = ''
        OR EXISTS (
             SELECT 1
             FROM runtime_filter_repositories AS runtime_filter
             WHERE runtime_filter.filter_kind = 'service'
               AND runtime_filter.repository_id = fact.repository_id
           )
      )
  AND (
        $10 = ''
        OR EXISTS (
             SELECT 1
             FROM runtime_filter_repositories AS runtime_filter
             WHERE runtime_filter.filter_kind = 'workload'
               AND runtime_filter.repository_id = fact.repository_id
           )
      )
  AND (
        $11 = ''
        OR EXISTS (
             SELECT 1
             FROM runtime_filter_repositories AS runtime_filter
             WHERE runtime_filter.filter_kind = 'environment'
               AND runtime_filter.repository_id = fact.repository_id
           )
	)
    AND ($12 = '' OR fact.severity_bucket = $12)
    AND (
          $13 = ''
          OR fact.detection_profile = $13
          OR ($13 = 'comprehensive' AND fact.detection_profile = '')
          OR (
                $13 = 'precise'
                AND fact.detection_profile = ''
                AND fact.impact_status IN ('affected_exact', 'not_affected_known_fixed')
                AND fact.observed_version <> ''
                AND fact.match_reason IN (
                      'npm_semver_affected_range', 'npm_semver_known_fixed',
                      'nuget_semver_affected_range', 'nuget_semver_known_fixed',
                      'cargo_semver_affected_range', 'cargo_semver_known_fixed',
                      'hex_semver_affected_range', 'hex_semver_known_fixed',
                      'maven_range_match', 'maven_known_fixed',
                      'swift_semver_affected_range', 'swift_semver_known_fixed'
                    )
             )
        )
    AND ($14 = '' OR fact.priority_bucket = $14)
    AND ($15 = 0 OR fact.priority_score >= $15)
    AND ($16 = '' OR fact.image_ref = $16)
    AND (
          (COALESCE(cardinality($22::text[]), 0) = 0 AND COALESCE(cardinality($23::text[]), 0) = 0)
          OR fact.repository_id = ANY($22::text[])
          OR fact.scope_id = ANY($23::text[])
        )
),
operator_candidates AS (
  SELECT fact.fact_id, fact.suppression_state,
         fact.payload #>> '{suppression,expires_at}' AS expires_at,
         fact.priority_score, fact.has_payload_finding_id, fact.canonical_key
  FROM supply_chain_impact_finding_supports AS fact
  JOIN ingestion_scopes AS scope
    ON scope.scope_id = fact.scope_id
   AND scope.active_generation_id = fact.generation_id
  JOIN scope_generations AS generation
    ON generation.scope_id = fact.scope_id
   AND generation.generation_id = fact.generation_id
  WHERE generation.status = 'active'
    AND fact.scope_id = 'operator:vulnerability_suppressions'
    AND fact.suppression_state <> 'active'
),

-- 2. joined_winners: the operator override joins by an indexed lateral probe instead of the operator_overrides CTE:
LEFT JOIN LATERAL (
    SELECT fact.fact_id, fact.suppression_state,
           fact.payload #>> '{suppression,expires_at}' AS expires_at
    FROM supply_chain_impact_finding_supports AS fact
    WHERE fact.scope_id = 'operator:vulnerability_suppressions'
      AND fact.canonical_key = source.canonical_key
      AND fact.suppression_state <> 'active'
      AND fact.generation_id = (
        SELECT g.generation_id FROM ingestion_scopes AS s
        JOIN scope_generations AS g ON g.scope_id = s.scope_id AND g.generation_id = s.active_generation_id
        WHERE s.scope_id = 'operator:vulnerability_suppressions' AND g.status = 'active')
    ORDER BY fact.priority_score DESC, fact.has_payload_finding_id DESC, fact.fact_id ASC
    LIMIT 1
  ) AS override ON TRUE

-- 3. the page payload and the override payload come from the supports table, not fact_records:
JOIN supply_chain_impact_finding_supports AS source ON source.fact_id = page.fact_id
LEFT JOIN supply_chain_impact_finding_supports AS override ON override.fact_id = page.override_fact_id
-- Everything else (runtime filter CTE, source_winners DISTINCT ON, canonical_facts, paged_facts) is the legacy text unchanged.
