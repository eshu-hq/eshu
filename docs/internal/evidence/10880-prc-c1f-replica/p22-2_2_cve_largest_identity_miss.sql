-- Shape: ungranted selector by CVE id. Variant: plain (the session's plan cache mode).
-- Rendered from the C1 head by explain_shapes_render_test.go; read-only. Override a value with psql -v name=value.
\set ON_ERROR_STOP on
\if :{?cve}
\else
\set cve 'CVE-2026-33349'
\endif
\if :{?advisory}
\else
\set advisory 'GHSA-xxxx-xxxx-xxxx'
\endif
\if :{?package}
\else
\set package 'pkg:npm/axios'
\endif
\if :{?repository}
\else
\set repository 'repository:example'
\endif
\if :{?digest}
\else
\set digest 'sha256:0000000000000000000000000000000000000000000000000000000000000000'
\endif
\if :{?fact_id}
\else
\set fact_id 'fact:example'
\endif
\if :{?finding_id}
\else
\set finding_id 'finding:example'
\endif
\if :{?canonical_key}
\else
\set canonical_key 'example-canonical-key'
\endif
BEGIN TRANSACTION READ ONLY;
SET statement_timeout = '25s';
PREPARE q AS
WITH
runtime_workload_identity_matches AS MATERIALIZED (
  SELECT scope_id, generation_id, payload
  FROM fact_records
  WHERE $8 <> ''
    AND fact_kind = 'reducer_workload_identity'
    AND is_tombstone = FALSE
    AND (
          (
            jsonb_typeof(payload->'workload_id') = 'string'
            AND payload->>'workload_id' = $8
          )
          OR (
               $8 LIKE 'workload:%'
               AND jsonb_typeof(payload->'entity_keys') IN ('array', 'string')
               AND payload->'entity_keys' ? $8
             )
        )

  UNION ALL

  SELECT scope_id, generation_id, payload
  FROM fact_records
  WHERE $8 <> ''
    AND fact_kind = 'reducer_workload_identity'
    AND is_tombstone = FALSE
    AND NOT COALESCE(
          (
            jsonb_typeof(payload->'workload_id') = 'string'
            AND payload->>'workload_id' = $8
          )
          OR (
               $8 LIKE 'workload:%'
               AND jsonb_typeof(payload->'entity_keys') IN ('array', 'string')
               AND payload->'entity_keys' ? $8
             ),
          FALSE
        )
    AND jsonb_typeof(payload->'workload_id') = 'string'
    AND payload->>'workload_id' IS DISTINCT FROM BTRIM(payload->>'workload_id')
    AND BTRIM(payload->>'workload_id') = $8

  UNION ALL

  SELECT scope_id, generation_id, payload
  FROM fact_records
  WHERE $8 <> ''
    AND fact_kind = 'reducer_workload_identity'
    AND is_tombstone = FALSE
    AND NOT COALESCE(
          (
            jsonb_typeof(payload->'workload_id') = 'string'
            AND payload->>'workload_id' = $8
          )
          OR (
               $8 LIKE 'workload:%'
               AND jsonb_typeof(payload->'entity_keys') IN ('array', 'string')
               AND payload->'entity_keys' ? $8
             ),
          FALSE
        )
    AND NOT COALESCE(
          jsonb_typeof(payload->'workload_id') = 'string'
          AND BTRIM(payload->>'workload_id') = $8,
          FALSE
        )
    AND jsonb_path_exists(payload->'entity_keys',
          'lax $[*] ? (@.type() == "string" && @ like_regex "(^ | $)")')
    AND $8 LIKE 'workload:%'
    AND EXISTS (
          SELECT 1
          FROM jsonb_array_elements_text(
            CASE
              WHEN jsonb_typeof(payload->'entity_keys') = 'array'
                THEN payload->'entity_keys'
              WHEN jsonb_typeof(payload->'entity_keys') = 'string'
                THEN jsonb_build_array(payload->'entity_keys')
              ELSE '[]'::jsonb
            END
          ) AS entity_key(value)
          WHERE BTRIM(entity_key.value) = $8
            AND BTRIM(entity_key.value) LIKE 'workload:%'
        )
),
runtime_workload_catalog_matches AS MATERIALIZED (
  SELECT scope_id, generation_id, payload
  FROM fact_records
  WHERE $8 <> ''
    AND fact_kind = 'reducer_service_catalog_correlation'
    AND is_tombstone = FALSE
    AND BTRIM(COALESCE(payload->>'outcome', '')) IN ('', 'exact', 'derived')
    AND LOWER(BTRIM(COALESCE(payload->>'provenance_only', ''))) <> 'true'
    AND jsonb_typeof(payload->'workload_id') = 'string'
    AND payload->>'workload_id' = $8

  UNION ALL

  SELECT scope_id, generation_id, payload
  FROM fact_records
  WHERE $8 <> ''
    AND fact_kind = 'reducer_service_catalog_correlation'
    AND is_tombstone = FALSE
    AND BTRIM(COALESCE(payload->>'outcome', '')) IN ('', 'exact', 'derived')
    AND LOWER(BTRIM(COALESCE(payload->>'provenance_only', ''))) <> 'true'
    AND jsonb_typeof(payload->'workload_id') = 'string'
    AND payload->>'workload_id' IS DISTINCT FROM $8
    AND payload->>'workload_id' IS DISTINCT FROM BTRIM(payload->>'workload_id')
    AND BTRIM(payload->>'workload_id') = $8
),
runtime_service_matches AS MATERIALIZED (
  SELECT scope_id, generation_id, payload
  FROM fact_records
  WHERE $9 <> ''
    AND fact_kind = 'reducer_service_catalog_correlation'
    AND is_tombstone = FALSE
    AND BTRIM(COALESCE(payload->>'outcome', '')) IN ('', 'exact', 'derived')
    AND LOWER(BTRIM(COALESCE(payload->>'provenance_only', ''))) <> 'true'
    AND jsonb_typeof(payload->'service_id') = 'string'
    AND payload->>'service_id' = $9

  UNION ALL

  SELECT scope_id, generation_id, payload
  FROM fact_records
  WHERE $9 <> ''
    AND fact_kind = 'reducer_service_catalog_correlation'
    AND is_tombstone = FALSE
    AND BTRIM(COALESCE(payload->>'outcome', '')) IN ('', 'exact', 'derived')
    AND LOWER(BTRIM(COALESCE(payload->>'provenance_only', ''))) <> 'true'
    AND jsonb_typeof(payload->'service_id') = 'string'
    AND payload->>'service_id' IS DISTINCT FROM $9
    AND payload->>'service_id' IS DISTINCT FROM BTRIM(payload->>'service_id')
    AND BTRIM(payload->>'service_id') = $9
),
runtime_environment_matches AS MATERIALIZED (
  SELECT scope_id, generation_id, payload
  FROM fact_records
  WHERE '' <> ''
    AND fact_kind = 'reducer_ci_cd_run_correlation'
    AND is_tombstone = FALSE
    AND BTRIM(COALESCE(payload->>'outcome', '')) IN ('', 'exact', 'derived')
    AND LOWER(BTRIM(COALESCE(payload->>'provenance_only', ''))) <> 'true'
    AND jsonb_typeof(payload->'environment') = 'string'
    AND payload->>'environment' = ''

  UNION ALL

  SELECT scope_id, generation_id, payload
  FROM fact_records
  WHERE '' <> ''
    AND fact_kind = 'reducer_ci_cd_run_correlation'
    AND is_tombstone = FALSE
    AND BTRIM(COALESCE(payload->>'outcome', '')) IN ('', 'exact', 'derived')
    AND LOWER(BTRIM(COALESCE(payload->>'provenance_only', ''))) <> 'true'
    AND jsonb_typeof(payload->'environment') = 'string'
    AND payload->>'environment' IS DISTINCT FROM ''
    AND payload->>'environment' IS DISTINCT FROM BTRIM(payload->>'environment')
    AND BTRIM(payload->>'environment') = ''
),
runtime_filter_candidates AS NOT MATERIALIZED (
  SELECT 'workload'::text AS filter_kind, runtime_fact.scope_id, runtime_fact.payload
  FROM runtime_workload_identity_matches AS runtime_fact
  JOIN ingestion_scopes AS runtime_scope
    ON runtime_scope.scope_id = runtime_fact.scope_id
   AND runtime_scope.active_generation_id = runtime_fact.generation_id
  JOIN scope_generations AS runtime_generation
    ON runtime_generation.scope_id = runtime_fact.scope_id
   AND runtime_generation.generation_id = runtime_fact.generation_id
  WHERE runtime_generation.status = 'active'

  UNION ALL

  SELECT 'workload'::text AS filter_kind, runtime_fact.scope_id, runtime_fact.payload
  FROM runtime_workload_catalog_matches AS runtime_fact
  JOIN ingestion_scopes AS runtime_scope
    ON runtime_scope.scope_id = runtime_fact.scope_id
   AND runtime_scope.active_generation_id = runtime_fact.generation_id
  JOIN scope_generations AS runtime_generation
    ON runtime_generation.scope_id = runtime_fact.scope_id
   AND runtime_generation.generation_id = runtime_fact.generation_id
  WHERE runtime_generation.status = 'active'

  UNION ALL

  SELECT 'service'::text AS filter_kind, runtime_fact.scope_id, runtime_fact.payload
  FROM runtime_service_matches AS runtime_fact
  JOIN ingestion_scopes AS runtime_scope
    ON runtime_scope.scope_id = runtime_fact.scope_id
   AND runtime_scope.active_generation_id = runtime_fact.generation_id
  JOIN scope_generations AS runtime_generation
    ON runtime_generation.scope_id = runtime_fact.scope_id
   AND runtime_generation.generation_id = runtime_fact.generation_id
  WHERE runtime_generation.status = 'active'

  UNION ALL

  SELECT 'environment'::text AS filter_kind, runtime_fact.scope_id, runtime_fact.payload
  FROM runtime_environment_matches AS runtime_fact
  JOIN ingestion_scopes AS runtime_scope
    ON runtime_scope.scope_id = runtime_fact.scope_id
   AND runtime_scope.active_generation_id = runtime_fact.generation_id
  JOIN scope_generations AS runtime_generation
    ON runtime_generation.scope_id = runtime_fact.scope_id
   AND runtime_generation.generation_id = runtime_fact.generation_id
  WHERE runtime_generation.status = 'active'
),
runtime_filter_repository_candidates AS (
  SELECT runtime_fact.filter_kind,
         runtime_fact.scope_id,
         runtime_repository.repository_id
  FROM runtime_filter_candidates AS runtime_fact

LEFT JOIN LATERAL (
  SELECT COALESCE(
           NULLIF(BTRIM(
             CASE
               WHEN jsonb_typeof(runtime_fact.payload->'repository_id') = 'string'
                 THEN runtime_fact.payload->>'repository_id'
             END
           ), ''),
           NULLIF(BTRIM(
             CASE
               WHEN jsonb_typeof(runtime_fact.payload->'repo_id') = 'string'
                 THEN runtime_fact.payload->>'repo_id'
             END
           ), ''),
           CASE
             WHEN selected_scope.value LIKE 'repository:%'
               THEN selected_scope.value
             WHEN selected_scope.value LIKE 'git-repository-scope:%'
               THEN NULLIF(BTRIM(SUBSTRING(selected_scope.value FROM 22)), '')
           END,
           (
             SELECT decoded_related.repository_id
             FROM (
               SELECT related.ordinal,
                      CASE
                        WHEN BTRIM(related.value) LIKE 'repository:%'
                          THEN BTRIM(related.value)
                        WHEN BTRIM(related.value) LIKE 'git-repository-scope:%'
                          THEN NULLIF(BTRIM(SUBSTRING(related.value FROM 22)), '')
                      END AS repository_id
               FROM jsonb_array_elements_text(
                 CASE
                   WHEN jsonb_typeof(runtime_fact.payload->'related_scope_ids') = 'array'
                     THEN runtime_fact.payload->'related_scope_ids'
                   WHEN jsonb_typeof(runtime_fact.payload->'related_scope_ids') = 'string'
                     THEN jsonb_build_array(runtime_fact.payload->'related_scope_ids')
                   ELSE '[]'::jsonb
                 END
               ) WITH ORDINALITY AS related(value, ordinal)
             ) AS decoded_related
             WHERE COALESCE(decoded_related.repository_id, '') <> ''
             ORDER BY decoded_related.ordinal
             LIMIT 1
           ),
           selected_scope.value
         ) AS repository_id
  FROM (
    SELECT COALESCE(
             NULLIF(BTRIM(
               CASE
                 WHEN jsonb_typeof(runtime_fact.payload->'scope_id') = 'string'
                   THEN runtime_fact.payload->>'scope_id'
               END
             ), ''),
             NULLIF(BTRIM(runtime_fact.scope_id), '')
           ) AS value
  ) AS selected_scope
) AS runtime_repository ON TRUE
),
runtime_filter_repositories AS MATERIALIZED (
  SELECT DISTINCT filter_kind, repository_id
  FROM runtime_filter_repository_candidates
  WHERE COALESCE(repository_id, '') <> ''
    AND (
          (
            COALESCE(cardinality($11::text[]), 0) = 0
            AND COALESCE(cardinality($12::text[]), 0) = 0
          )
          OR runtime_filter_repository_candidates.repository_id = ANY($11::text[])
          OR runtime_filter_repository_candidates.scope_id = ANY($12::text[])
        )
),
key_supports AS MATERIALIZED (
  SELECT fact.fact_id, fact.canonical_key, fact.scope_id, fact.generation_id, fact.finding_id,
         fact.priority_score, fact.has_payload_finding_id
  FROM supply_chain_impact_finding_supports AS fact
  WHERE $1::text IS NOT NULL
    AND fact.cve_id = $4 AND fact.cve_id <> ''
    AND fact.scope_id <> 'operator:vulnerability_suppressions'
    AND fact.scope_id NOT LIKE 'image://derived/%'
    AND $14::boolean IS NOT NULL AND $15::text IS NOT NULL AND $16::text IS NOT NULL
    AND $17::text IS NOT NULL AND $18::text IS NOT NULL AND $19::text[] IS NOT NULL
    AND ($3 = '' OR fact.advisory_id = $3 OR fact.cve_id = $3)
    AND ($4 = '' OR fact.cve_id = $4)
    AND ($5 = '' OR fact.package_id = $5)
    AND ($6 = '' OR fact.repository_id = $6)
    AND ($7 = '' OR fact.subject_digest = $7)

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
        $8 = ''
        OR EXISTS (
             SELECT 1
             FROM runtime_filter_repositories AS runtime_filter
             WHERE runtime_filter.filter_kind = 'workload'
               AND runtime_filter.repository_id = fact.repository_id
           )
      )
  AND (
        '' = ''
        OR EXISTS (
             SELECT 1
             FROM runtime_filter_repositories AS runtime_filter
             WHERE runtime_filter.filter_kind = 'environment'
               AND runtime_filter.repository_id = fact.repository_id
           )
	)
    AND ($10 = '' OR fact.image_ref = $10)
    AND (
      (COALESCE(cardinality($11::text[]), 0) = 0 AND COALESCE(cardinality($12::text[]), 0) = 0)
      OR fact.repository_id = ANY($11::text[])
      OR fact.scope_id = ANY($12::text[])
    )
),
identity_keys AS (
  SELECT fact.canonical_key
  FROM supply_chain_impact_finding_supports AS fact
  CROSS JOIN LATERAL (
    SELECT 1 AS authorized
    FROM ingestion_scopes AS scope
    JOIN scope_generations AS generation
      ON generation.scope_id = scope.scope_id
     AND generation.generation_id = scope.active_generation_id
    LEFT JOIN scanner_image_result_fact_classifications AS finding_class
      ON finding_class.finding_fact_id = fact.fact_id
    LEFT JOIN scanner_image_result_fact_associations AS finding_association
      ON finding_association.finding_fact_id = fact.fact_id
    WHERE scope.scope_id = fact.scope_id
      AND scope.active_generation_id = fact.generation_id
      AND generation.status = 'active'
      AND finding_association.finding_fact_id IS NULL
      AND (finding_class.finding_fact_id IS NULL
           OR (finding_class.source_class = 'static' AND finding_class.lifecycle = 'active'))
    LIMIT 1
  ) AS authority
  WHERE $2 <> ''
    AND fact.fact_id = $2
    AND fact.scope_id <> 'operator:vulnerability_suppressions'
    AND fact.scope_id NOT LIKE 'image://derived/%'
    AND $14::boolean IS NOT NULL AND $15::text IS NOT NULL AND $16::text IS NOT NULL
    AND $17::text IS NOT NULL AND $18::text IS NOT NULL AND $19::text[] IS NOT NULL
    AND ($3 = '' OR fact.advisory_id = $3 OR fact.cve_id = $3)
    AND ($4 = '' OR fact.cve_id = $4)
    AND ($5 = '' OR fact.package_id = $5)
    AND ($6 = '' OR fact.repository_id = $6)
    AND ($7 = '' OR fact.subject_digest = $7)

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
        $8 = ''
        OR EXISTS (
             SELECT 1
             FROM runtime_filter_repositories AS runtime_filter
             WHERE runtime_filter.filter_kind = 'workload'
               AND runtime_filter.repository_id = fact.repository_id
           )
      )
  AND (
        '' = ''
        OR EXISTS (
             SELECT 1
             FROM runtime_filter_repositories AS runtime_filter
             WHERE runtime_filter.filter_kind = 'environment'
               AND runtime_filter.repository_id = fact.repository_id
           )
	)
    AND ($10 = '' OR fact.image_ref = $10)
    AND (
      (COALESCE(cardinality($11::text[]), 0) = 0 AND COALESCE(cardinality($12::text[]), 0) = 0)
      OR fact.repository_id = ANY($11::text[])
      OR fact.scope_id = ANY($12::text[])
    )
),
first_hit AS MATERIALIZED (
  SELECT fact.fact_id, fact.canonical_key
  FROM key_supports AS fact
  CROSS JOIN LATERAL (
    SELECT 1 AS authorized
    FROM ingestion_scopes AS scope
    JOIN scope_generations AS generation
      ON generation.scope_id = scope.scope_id
     AND generation.generation_id = scope.active_generation_id
    LEFT JOIN scanner_image_result_fact_classifications AS finding_class
      ON finding_class.finding_fact_id = fact.fact_id
    LEFT JOIN scanner_image_result_fact_associations AS finding_association
      ON finding_association.finding_fact_id = fact.fact_id
    WHERE scope.scope_id = fact.scope_id
      AND scope.active_generation_id = fact.generation_id
      AND generation.status = 'active'
      AND finding_association.finding_fact_id IS NULL
      AND (finding_class.finding_fact_id IS NULL
           OR (finding_class.source_class = 'static' AND finding_class.lifecycle = 'active'))
    LIMIT 1
  ) AS authority
  WHERE ($2 = ''
        OR fact.fact_id = $2
        OR fact.finding_id = $2
        OR fact.canonical_key = $2
        OR fact.canonical_key IN (SELECT identity_key.canonical_key FROM identity_keys AS identity_key))
  LIMIT 1
),
second_hit AS MATERIALIZED (
  SELECT fact.fact_id, fact.canonical_key
  FROM key_supports AS fact
  CROSS JOIN LATERAL (
    SELECT 1 AS authorized
    FROM ingestion_scopes AS scope
    JOIN scope_generations AS generation
      ON generation.scope_id = scope.scope_id
     AND generation.generation_id = scope.active_generation_id
    LEFT JOIN scanner_image_result_fact_classifications AS finding_class
      ON finding_class.finding_fact_id = fact.fact_id
    LEFT JOIN scanner_image_result_fact_associations AS finding_association
      ON finding_association.finding_fact_id = fact.fact_id
    WHERE scope.scope_id = fact.scope_id
      AND scope.active_generation_id = fact.generation_id
      AND generation.status = 'active'
      AND finding_association.finding_fact_id IS NULL
      AND (finding_class.finding_fact_id IS NULL
           OR (finding_class.source_class = 'static' AND finding_class.lifecycle = 'active'))
    LIMIT 1
  ) AS authority
  WHERE fact.canonical_key <> (SELECT first_key.canonical_key FROM first_hit AS first_key)
    AND ($2 = ''
        OR fact.fact_id = $2
        OR fact.finding_id = $2
        OR fact.canonical_key = $2
        OR fact.canonical_key IN (SELECT identity_key.canonical_key FROM identity_keys AS identity_key))
  LIMIT 1
),
ambiguous_rows AS (
  SELECT support.finding_id, support.source_confidence, support.payload
  FROM (
    SELECT hit.fact_id FROM first_hit AS hit WHERE EXISTS (SELECT 1 FROM second_hit)
    UNION ALL
    SELECT hit.fact_id FROM second_hit AS hit
  ) AS decided
  JOIN supply_chain_impact_finding_supports AS support ON support.fact_id = decided.fact_id
),
ranked_copies AS MATERIALIZED (
  SELECT fact.fact_id, fact.scope_id, fact.generation_id
  FROM key_supports AS fact
  WHERE NOT EXISTS (SELECT 1 FROM second_hit)
    AND fact.canonical_key = (SELECT first_key.canonical_key FROM first_hit AS first_key)
    AND ($2 = ''
        OR fact.fact_id = $2
        OR fact.finding_id = $2
        OR fact.canonical_key = $2
        OR fact.canonical_key IN (SELECT identity_key.canonical_key FROM identity_keys AS identity_key))
  ORDER BY fact.priority_score DESC, fact.has_payload_finding_id DESC, fact.fact_id ASC
),
best_copy AS MATERIALIZED (
  SELECT fact.fact_id
  FROM ranked_copies AS fact
  CROSS JOIN LATERAL (
    SELECT 1 AS authorized
    FROM ingestion_scopes AS scope
    JOIN scope_generations AS generation
      ON generation.scope_id = scope.scope_id
     AND generation.generation_id = scope.active_generation_id
    LEFT JOIN scanner_image_result_fact_classifications AS finding_class
      ON finding_class.finding_fact_id = fact.fact_id
    LEFT JOIN scanner_image_result_fact_associations AS finding_association
      ON finding_association.finding_fact_id = fact.fact_id
    WHERE scope.scope_id = fact.scope_id
      AND scope.active_generation_id = fact.generation_id
      AND generation.status = 'active'
      AND finding_association.finding_fact_id IS NULL
      AND (finding_class.finding_fact_id IS NULL
           OR (finding_class.source_class = 'static' AND finding_class.lifecycle = 'active'))
    LIMIT 1
  ) AS authority
  LIMIT 1
),
source_candidates AS MATERIALIZED (
  SELECT support.fact_id, support.scope_id, support.finding_id, support.source_confidence, support.payload,
         support.suppression_state, support.priority_score, support.has_payload_finding_id, support.canonical_key
  FROM best_copy
  JOIN supply_chain_impact_finding_supports AS support ON support.fact_id = best_copy.fact_id
),
source_winners AS (
  SELECT DISTINCT ON (canonical_key)
         fact_id, scope_id, finding_id, source_confidence, payload,
         suppression_state, priority_score, has_payload_finding_id,
         canonical_key
  FROM source_candidates
  ORDER BY canonical_key,
           priority_score DESC,
           has_payload_finding_id DESC,
           fact_id ASC
),
joined_winners AS (
  SELECT source.fact_id,
         source.scope_id,
         source.finding_id,
         source.source_confidence,
         source.payload AS source_payload,
         override.payload AS override_payload,
         CASE
           WHEN override.fact_id IS NULL THEN source.suppression_state
           ELSE CASE
    WHEN override.scope_id <> 'operator:vulnerability_suppressions'
    THEN override.suppression_state
    ELSE CASE
    WHEN override.suppression_state NOT IN ('not_affected','accepted_risk','false_positive','ignored','advisory_retracted')
      OR NULLIF(override.payload #>> '{suppression,expires_at}', '') IS NULL
    THEN override.suppression_state
    WHEN NOT pg_input_is_valid(override.payload #>> '{suppression,expires_at}', 'timestamp with time zone')
    THEN 'expired'
    WHEN (override.payload #>> '{suppression,expires_at}')::timestamptz <= $13::timestamptz
    THEN 'expired'
    ELSE override.suppression_state
  END
  END
         END AS effective_suppression_state,
         source.priority_score,
         source.has_payload_finding_id,
         source.canonical_key
  FROM source_winners AS source
  LEFT JOIN LATERAL (
    SELECT operator.fact_id, operator.scope_id, operator.payload, operator.suppression_state
    FROM supply_chain_impact_finding_supports AS operator
    WHERE operator.scope_id = 'operator:vulnerability_suppressions'
      AND operator.canonical_key = source.canonical_key
      AND operator.suppression_state <> 'active'
      AND operator.generation_id = (
        SELECT g.generation_id FROM ingestion_scopes AS s
        JOIN scope_generations AS g ON g.scope_id = s.scope_id AND g.generation_id = s.active_generation_id
        WHERE s.scope_id = 'operator:vulnerability_suppressions' AND g.status = 'active')
    ORDER BY operator.priority_score DESC, operator.has_payload_finding_id DESC, operator.fact_id ASC
    LIMIT 1
  ) AS override ON TRUE
),
canonical_winners AS (
  SELECT fact_id,
         scope_id,
         finding_id,
         source_confidence,
         CASE
    WHEN override_payload IS NULL
    THEN (source_payload || jsonb_build_object(
    'suppression_state', effective_suppression_state,
    'suppression',
      COALESCE(
        CASE
          WHEN jsonb_typeof(source_payload->'suppression') = 'object'
          THEN source_payload->'suppression'
        END,
        '{}'::jsonb
      ) || jsonb_build_object('state', effective_suppression_state)
  ))
    ELSE source_payload || jsonb_build_object(
      'suppression_state', effective_suppression_state,
      'suppression',
        COALESCE(
          CASE
            WHEN jsonb_typeof(override_payload->'suppression') = 'object'
            THEN override_payload->'suppression'
          END,
          '{}'::jsonb
        ) || jsonb_build_object('state', effective_suppression_state)
    )
  END AS payload,
         effective_suppression_state AS suppression_state,
         priority_score,
         has_payload_finding_id,
         canonical_key
  FROM joined_winners
),
canonical_facts AS (
  SELECT finding_id, source_confidence, payload
  FROM canonical_winners
  UNION ALL
  SELECT finding_id, source_confidence, payload
  FROM ambiguous_rows
)
SELECT finding_id, source_confidence, payload
FROM canonical_facts
ORDER BY finding_id ASC
LIMIT 2;
-- for the plan without running the statement, drop ANALYZE from the line below
EXPLAIN (ANALYZE, BUFFERS) EXECUTE q('reducer_supply_chain_impact_finding', 'finding:p22-absent-identity', '', :'cve', '', '', '', '', '', '', '{}'::text[], '{}'::text[], now(), false, '', '', '', '', '{}'::text[]);
DEALLOCATE q;
ROLLBACK;
