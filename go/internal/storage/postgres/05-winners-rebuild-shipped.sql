
WITH winners_now AS (
WITH active_facts AS NOT MATERIALIZED (
    SELECT fact.*
    FROM fact_records AS fact
    JOIN ingestion_scopes AS scope
      ON scope.scope_id = fact.scope_id
     AND scope.active_generation_id = fact.generation_id
    JOIN scope_generations AS generation
      ON generation.scope_id = fact.scope_id
     AND generation.generation_id = fact.generation_id
    WHERE fact.fact_kind = 'reducer_supply_chain_impact_finding'
      AND fact.is_tombstone = FALSE
      AND generation.status = 'active'
),
source_candidates AS (
    SELECT
        fact.fact_id,
        fact.scope_id,
        fact.payload,
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
        ) AS canonical_key,
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
        ) AS finding_id,
        COALESCE(NULLIF(fact.payload->>'priority_score', '')::int, 0) AS priority_score,
        CASE WHEN NULLIF(fact.payload->>'finding_id', '') IS NULL THEN 0 ELSE 1 END AS has_payload_finding_id,
        COALESCE(NULLIF(fact.payload->>'suppression_state', ''), 'active') AS suppression_state
    FROM active_facts AS fact
    LEFT JOIN scanner_image_result_fact_classifications AS finding_class
      ON finding_class.finding_fact_id = fact.fact_id
    LEFT JOIN scanner_image_result_fact_associations AS finding_association
      ON finding_association.finding_fact_id = fact.fact_id
    WHERE fact.scope_id <> 'operator:vulnerability_suppressions'
      AND finding_association.finding_fact_id IS NULL
      AND fact.scope_id NOT LIKE 'image://derived/%'
      AND (finding_class.finding_fact_id IS NULL
           OR (finding_class.source_class = 'static' AND finding_class.lifecycle = 'active'))
),
source_ranked AS (
    SELECT source.*,
           COUNT(*) OVER (PARTITION BY canonical_key) AS source_count
    FROM source_candidates AS source
),
source_winners AS (
    SELECT DISTINCT ON (canonical_key) *
    FROM source_ranked
    ORDER BY canonical_key,
             priority_score DESC,
             has_payload_finding_id DESC,
             fact_id ASC
),
operator_candidates AS (
    SELECT
        fact.fact_id,
        fact.payload,
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
        ) AS canonical_key,
        COALESCE(NULLIF(fact.payload->>'suppression_state', ''), 'active') AS suppression_state,
        COALESCE(NULLIF(fact.payload->>'priority_score', '')::int, 0) AS priority_score,
        CASE WHEN NULLIF(fact.payload->>'finding_id', '') IS NULL THEN 0 ELSE 1 END AS has_payload_finding_id
    FROM active_facts AS fact
    WHERE fact.scope_id = 'operator:vulnerability_suppressions'
      AND COALESCE(NULLIF(fact.payload->>'suppression_state', ''), 'active') <> 'active'
),
operator_overrides AS (
    SELECT DISTINCT ON (canonical_key) *
    FROM operator_candidates
    ORDER BY canonical_key,
             priority_score DESC,
             has_payload_finding_id DESC,
             fact_id ASC
)
SELECT
    source.canonical_key,
    source.fact_id AS winner_fact_id,
    source.scope_id AS winner_scope_id,
    source.finding_id,
    source.priority_score,
    source.source_count,
    COALESCE(source.payload->>'impact_status', '') AS impact_status,
    COALESCE(source.payload->>'ecosystem', '') AS ecosystem,
    CASE
    WHEN COALESCE(NULLIF(source.payload->>'cvss_score', '')::numeric, 0) >= 9.0 THEN 'critical'
    WHEN COALESCE(NULLIF(source.payload->>'cvss_score', '')::numeric, 0) >= 7.0 THEN 'high'
    WHEN COALESCE(NULLIF(source.payload->>'cvss_score', '')::numeric, 0) >= 4.0 THEN 'medium'
    WHEN COALESCE(NULLIF(source.payload->>'cvss_score', '')::numeric, 0) > 0.0 THEN 'low'
    ELSE CASE lower(btrim(source.payload #>> '{provenance,selected_severity_label}'))
        WHEN 'critical' THEN 'critical'
        WHEN 'high' THEN 'high'
        WHEN 'important' THEN 'high'
        WHEN 'medium' THEN 'medium'
        WHEN 'moderate' THEN 'medium'
        WHEN 'low' THEN 'low'
        ELSE 'none'
    END
  END AS severity_bucket,
    COALESCE(source.payload->>'repository_id', '') AS repository_id,
    COALESCE(source.payload->>'cve_id', '') AS cve_id,
    COALESCE(source.payload->>'advisory_id', '') AS advisory_id,
    COALESCE(source.payload->>'package_id', '') AS package_id,
    COALESCE(source.payload->>'subject_digest', '') AS subject_digest,
    COALESCE(source.payload->>'image_ref', '') AS image_ref,
    COALESCE(source.payload->>'priority_bucket', '') AS priority_bucket,
    COALESCE(source.payload->>'detection_profile', '') AS detection_profile,
    COALESCE(source.payload->>'observed_version', '') AS observed_version,
    COALESCE(source.payload->>'match_reason', '') AS match_reason,
    COALESCE(override.suppression_state, source.suppression_state) AS suppression_state,
    CASE
        WHEN override.fact_id IS NULL
          OR NULLIF(override.payload #>> '{suppression,expires_at}', '') IS NULL
        THEN NULL
        WHEN pg_input_is_valid(
            override.payload #>> '{suppression,expires_at}',
            'timestamp with time zone'
        )
        THEN (override.payload #>> '{suppression,expires_at}')::timestamptz
        ELSE '-infinity'::timestamptz
    END AS suppression_expires_at,
    COALESCE(source.payload->'service_ids', '[]'::jsonb) AS service_ids,
    COALESCE(source.payload->'workload_ids', '[]'::jsonb) AS workload_ids,
    COALESCE(source.payload->'environments', '[]'::jsonb) AS environments
FROM source_winners AS source
LEFT JOIN operator_overrides AS override
  ON override.canonical_key = source.canonical_key
),
changed AS MATERIALIZED (
    SELECT n.*, (w.canonical_key IS NULL) AS is_new
    FROM winners_now AS n
    LEFT JOIN supply_chain_impact_canonical_winners AS w
      ON w.canonical_key = n.canonical_key
    WHERE w.canonical_key IS NULL
       OR ROW(n.winner_fact_id, n.winner_scope_id, n.finding_id, n.priority_score, n.source_count, n.impact_status, n.ecosystem, n.severity_bucket, n.repository_id, n.cve_id, n.advisory_id, n.package_id, n.subject_digest, n.image_ref, n.priority_bucket, n.detection_profile, n.observed_version, n.match_reason, n.suppression_state, n.suppression_expires_at, n.service_ids, n.workload_ids, n.environments) IS DISTINCT FROM ROW(w.winner_fact_id, w.winner_scope_id, w.finding_id, w.priority_score, w.source_count, w.impact_status, w.ecosystem, w.severity_bucket, w.repository_id, w.cve_id, w.advisory_id, w.package_id, w.subject_digest, w.image_ref, w.priority_bucket, w.detection_profile, w.observed_version, w.match_reason, w.suppression_state, w.suppression_expires_at, w.service_ids, w.workload_ids, w.environments)
),
upserted AS (
    INSERT INTO supply_chain_impact_canonical_winners (
        canonical_key, winner_fact_id, winner_scope_id, finding_id, priority_score, source_count, impact_status, ecosystem, severity_bucket, repository_id, cve_id, advisory_id, package_id, subject_digest, image_ref, priority_bucket, detection_profile, observed_version, match_reason, suppression_state, suppression_expires_at, service_ids, workload_ids, environments, materialized_at
    )
    SELECT
        canonical_key, winner_fact_id, winner_scope_id, finding_id, priority_score, source_count, impact_status, ecosystem, severity_bucket, repository_id, cve_id, advisory_id, package_id, subject_digest, image_ref, priority_bucket, detection_profile, observed_version, match_reason, suppression_state, suppression_expires_at, service_ids, workload_ids, environments, $1
    FROM changed
    ON CONFLICT (canonical_key) DO UPDATE SET
        winner_fact_id = EXCLUDED.winner_fact_id,
        winner_scope_id = EXCLUDED.winner_scope_id,
        finding_id = EXCLUDED.finding_id,
        priority_score = EXCLUDED.priority_score,
        source_count = EXCLUDED.source_count,
        impact_status = EXCLUDED.impact_status,
        ecosystem = EXCLUDED.ecosystem,
        severity_bucket = EXCLUDED.severity_bucket,
        repository_id = EXCLUDED.repository_id,
        cve_id = EXCLUDED.cve_id,
        advisory_id = EXCLUDED.advisory_id,
        package_id = EXCLUDED.package_id,
        subject_digest = EXCLUDED.subject_digest,
        image_ref = EXCLUDED.image_ref,
        priority_bucket = EXCLUDED.priority_bucket,
        detection_profile = EXCLUDED.detection_profile,
        observed_version = EXCLUDED.observed_version,
        match_reason = EXCLUDED.match_reason,
        suppression_state = EXCLUDED.suppression_state,
        suppression_expires_at = EXCLUDED.suppression_expires_at,
        service_ids = EXCLUDED.service_ids,
        workload_ids = EXCLUDED.workload_ids,
        environments = EXCLUDED.environments,
        materialized_at = EXCLUDED.materialized_at
),
deleted AS (
    DELETE FROM supply_chain_impact_canonical_winners w
    WHERE NOT EXISTS (SELECT 1 FROM winners_now n WHERE n.canonical_key = w.canonical_key)
    RETURNING 1
)
INSERT INTO supply_chain_impact_winners_materialization (singleton, materialized_at)
VALUES (1, $1)
ON CONFLICT (singleton) DO UPDATE SET materialized_at = EXCLUDED.materialized_at
RETURNING
    (SELECT count(*) FROM changed WHERE is_new),
    (SELECT count(*) FROM changed WHERE NOT is_new),
    (SELECT count(*) FROM deleted),
    (SELECT count(*) FROM winners_now) - (SELECT count(*) FROM changed),
    (SELECT materialized_at FROM supply_chain_impact_winners_materialization WHERE singleton = 1)
