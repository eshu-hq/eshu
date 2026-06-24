// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

const generationRetentionCandidateQuery = `
WITH locked_scopes AS (
    SELECT scope.scope_id
    FROM ingestion_scopes AS scope
    WHERE EXISTS (
        SELECT 1
        FROM scope_generations AS generation
        WHERE generation.scope_id = scope.scope_id
          AND generation.status = 'superseded'
          AND generation.generation_id <> ALL($4::text[])
          AND generation.superseded_at IS NOT NULL
          AND generation.superseded_at < $1
    )
    ORDER BY scope.scope_id ASC
    LIMIT $3
    FOR UPDATE SKIP LOCKED
),
ranked_superseded_generations AS (
    SELECT
        generation.scope_id,
        generation.generation_id,
        scope.scope_kind,
        generation.superseded_at,
        generation.observed_at,
        ROW_NUMBER() OVER (PARTITION BY generation.scope_id ORDER BY generation.superseded_at DESC, generation.generation_id DESC) AS superseded_rank
    FROM scope_generations AS generation
    JOIN locked_scopes AS locked_scope
      ON locked_scope.scope_id = generation.scope_id
    JOIN ingestion_scopes AS scope
      ON scope.scope_id = generation.scope_id
    WHERE generation.status = 'superseded'
      AND generation.generation_id <> ALL($4::text[])
      AND generation.superseded_at IS NOT NULL
),
eligible_generations AS (
    SELECT ranked.*
    FROM ranked_superseded_generations AS ranked
    JOIN ingestion_scopes AS scope
      ON scope.scope_id = ranked.scope_id
    WHERE ranked.generation_id IS DISTINCT FROM scope.active_generation_id
      AND ranked.superseded_at < $1
      AND ranked.superseded_rank > $2
      AND NOT EXISTS (
          SELECT 1
          FROM fact_work_items AS work
          WHERE work.generation_id = ranked.generation_id
            AND work.status IN ('claimed', 'running', 'retrying')
      )
    ORDER BY ranked.superseded_at ASC, ranked.generation_id ASC
    LIMIT $3
)
SELECT
    candidate.scope_id,
    candidate.generation_id,
    candidate.scope_kind,
    candidate.superseded_at,
    candidate.observed_at
FROM eligible_generations AS candidate
JOIN scope_generations AS generation
  ON generation.generation_id = candidate.generation_id
JOIN ingestion_scopes AS scope
  ON scope.scope_id = candidate.scope_id
WHERE generation.status = 'superseded'
  AND generation.superseded_at = candidate.superseded_at
  AND candidate.generation_id IS DISTINCT FROM scope.active_generation_id
FOR UPDATE OF generation, scope SKIP LOCKED
`

const generationRetentionRowCountsQuery = `
WITH generation_retention_row_counts AS (
    SELECT unnest($1::text[]) AS generation_id
),
candidate_files AS (
    SELECT DISTINCT
        candidate.generation_id,
        row.payload->>'repo_id' AS repo_id,
        row.payload->>'relative_path' AS relative_path
    FROM generation_retention_row_counts AS candidate
    JOIN fact_records AS row
      ON row.generation_id = candidate.generation_id
    WHERE row.fact_kind = 'file'
      AND row.is_tombstone = FALSE
      AND row.payload->>'repo_id' <> ''
      AND row.payload->>'relative_path' <> ''
),
prunable_candidate_files AS (
    SELECT candidate.*
    FROM candidate_files AS candidate
    WHERE NOT EXISTS (
        SELECT 1
        FROM fact_records AS retained
        WHERE retained.generation_id <> ALL($1::text[])
          AND retained.fact_kind = 'file'
          AND retained.is_tombstone = FALSE
          AND retained.payload->>'repo_id' = candidate.repo_id
          AND retained.payload->>'relative_path' = candidate.relative_path
    )
),
candidate_entities AS (
    SELECT DISTINCT
        candidate.generation_id,
        row.payload->>'repo_id' AS repo_id,
        row.payload->>'entity_id' AS entity_id
    FROM generation_retention_row_counts AS candidate
    JOIN fact_records AS row
      ON row.generation_id = candidate.generation_id
    WHERE row.fact_kind = 'content_entity'
      AND row.is_tombstone = FALSE
      AND row.payload->>'repo_id' <> ''
      AND row.payload->>'entity_id' <> ''
),
prunable_candidate_entities AS (
    SELECT candidate.*
    FROM candidate_entities AS candidate
    WHERE NOT EXISTS (
        SELECT 1
        FROM fact_records AS retained
        WHERE retained.generation_id <> ALL($1::text[])
          AND retained.fact_kind = 'content_entity'
          AND retained.is_tombstone = FALSE
          AND retained.payload->>'repo_id' = candidate.repo_id
          AND retained.payload->>'entity_id' = candidate.entity_id
    )
)
SELECT candidate.generation_id, 'fact_records' AS table_name, COUNT(row.generation_id) AS row_count
FROM generation_retention_row_counts AS candidate
LEFT JOIN fact_records AS row
  ON candidate.generation_id = row.generation_id
GROUP BY candidate.generation_id
UNION ALL
SELECT candidate.generation_id, 'fact_work_items' AS table_name, COUNT(row.generation_id) AS row_count
FROM generation_retention_row_counts AS candidate
LEFT JOIN fact_work_items AS row
  ON candidate.generation_id = row.generation_id
GROUP BY candidate.generation_id
UNION ALL
SELECT candidate.generation_id, 'fact_replay_events' AS table_name, COUNT(row.generation_id) AS row_count
FROM generation_retention_row_counts AS candidate
LEFT JOIN fact_replay_events AS row
  ON candidate.generation_id = row.generation_id
GROUP BY candidate.generation_id
UNION ALL
SELECT candidate.generation_id, 'semantic_extraction_jobs' AS table_name, COUNT(row.generation_id) AS row_count
FROM generation_retention_row_counts AS candidate
LEFT JOIN semantic_extraction_jobs AS row
  ON candidate.generation_id = row.generation_id
GROUP BY candidate.generation_id
UNION ALL
SELECT candidate.generation_id, 'shared_projection_acceptance' AS table_name, COUNT(row.generation_id) AS row_count
FROM generation_retention_row_counts AS candidate
LEFT JOIN shared_projection_acceptance AS row
  ON candidate.generation_id = row.generation_id
GROUP BY candidate.generation_id
UNION ALL
SELECT candidate.generation_id, 'graph_projection_phase_state' AS table_name, COUNT(row.generation_id) AS row_count
FROM generation_retention_row_counts AS candidate
LEFT JOIN graph_projection_phase_state AS row
  ON candidate.generation_id = row.generation_id
GROUP BY candidate.generation_id
UNION ALL
SELECT candidate.generation_id, 'graph_projection_phase_repair_queue' AS table_name, COUNT(row.generation_id) AS row_count
FROM generation_retention_row_counts AS candidate
LEFT JOIN graph_projection_phase_repair_queue AS row
  ON candidate.generation_id = row.generation_id
GROUP BY candidate.generation_id
UNION ALL
SELECT candidate.generation_id, 'iac_reachability' AS table_name, COUNT(row.generation_id) AS row_count
FROM generation_retention_row_counts AS candidate
LEFT JOIN iac_reachability AS row
  ON candidate.generation_id = row.generation_id
GROUP BY candidate.generation_id
UNION ALL
SELECT candidate.generation_id, 'shared_projection_intents' AS table_name, COUNT(row.generation_id) AS row_count
FROM generation_retention_row_counts AS candidate
LEFT JOIN shared_projection_intents AS row
  ON candidate.generation_id = row.generation_id
GROUP BY candidate.generation_id
UNION ALL
SELECT candidate.generation_id, 'content_file_references' AS table_name, COUNT(ref.reference_id) AS row_count
FROM generation_retention_row_counts AS candidate
LEFT JOIN prunable_candidate_files AS file
  ON file.generation_id = candidate.generation_id
LEFT JOIN content_file_references AS ref
  ON ref.repo_id = file.repo_id
 AND ref.relative_path = file.relative_path
GROUP BY candidate.generation_id
UNION ALL
SELECT candidate.generation_id, 'content_entities' AS table_name, COUNT(entity.entity_id) AS row_count
FROM generation_retention_row_counts AS candidate
LEFT JOIN prunable_candidate_entities AS candidate_entity
  ON candidate_entity.generation_id = candidate.generation_id
LEFT JOIN content_entities AS entity
  ON entity.repo_id = candidate_entity.repo_id
 AND entity.entity_id = candidate_entity.entity_id
GROUP BY candidate.generation_id
UNION ALL
SELECT candidate.generation_id, 'content_files' AS table_name, COUNT(content_file.relative_path) AS row_count
FROM generation_retention_row_counts AS candidate
LEFT JOIN prunable_candidate_files AS file
  ON file.generation_id = candidate.generation_id
LEFT JOIN content_files AS content_file
  ON content_file.repo_id = file.repo_id
 AND content_file.relative_path = file.relative_path
GROUP BY candidate.generation_id
`

const insertGenerationRetentionEventQuery = `
INSERT INTO generation_retention_events (
    event_id,
    scope_id_hash,
    generation_id_hash,
    scope_class,
    policy_scope,
    policy_revision,
    generation_observed_at,
    generation_superseded_at,
    reason,
    row_counts,
    pruned_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10::jsonb, $11
)
ON CONFLICT (event_id) DO NOTHING
`

const deleteSharedProjectionIntentsForGenerationsQuery = `
DELETE FROM shared_projection_intents
WHERE generation_id = ANY($1::text[])
`

const pruneContentFileReferencesForGenerationsQuery = `
WITH candidate_files AS (
    SELECT DISTINCT
        payload->>'repo_id' AS repo_id,
        payload->>'relative_path' AS relative_path
    FROM fact_records
    WHERE generation_id = ANY($1::text[])
      AND fact_kind = 'file'
      AND is_tombstone = FALSE
      AND payload->>'repo_id' <> ''
      AND payload->>'relative_path' <> ''
)
DELETE FROM content_file_references AS ref
USING candidate_files AS candidate
WHERE ref.repo_id = candidate.repo_id
  AND ref.relative_path = candidate.relative_path
  AND NOT EXISTS (
      SELECT 1
      FROM fact_records AS retained
      WHERE retained.generation_id <> ALL($1::text[])
        AND retained.fact_kind = 'file'
        AND retained.is_tombstone = FALSE
        AND retained.payload->>'repo_id' = ref.repo_id
        AND retained.payload->>'relative_path' = ref.relative_path
  )
`

const pruneContentEntitiesForGenerationsQuery = `
WITH candidate_entities AS (
    SELECT DISTINCT
        payload->>'repo_id' AS repo_id,
        payload->>'entity_id' AS entity_id
    FROM fact_records
    WHERE generation_id = ANY($1::text[])
      AND fact_kind = 'content_entity'
      AND is_tombstone = FALSE
      AND payload->>'repo_id' <> ''
      AND payload->>'entity_id' <> ''
)
DELETE FROM content_entities AS entity
USING candidate_entities AS candidate
WHERE entity.repo_id = candidate.repo_id
  AND entity.entity_id = candidate.entity_id
  AND NOT EXISTS (
      SELECT 1
      FROM fact_records AS retained
      WHERE retained.generation_id <> ALL($1::text[])
        AND retained.fact_kind = 'content_entity'
        AND retained.is_tombstone = FALSE
        AND retained.payload->>'repo_id' = entity.repo_id
        AND retained.payload->>'entity_id' = entity.entity_id
  )
`

const pruneContentFilesForGenerationsQuery = `
WITH candidate_files AS (
    SELECT DISTINCT
        payload->>'repo_id' AS repo_id,
        payload->>'relative_path' AS relative_path
    FROM fact_records
    WHERE generation_id = ANY($1::text[])
      AND fact_kind = 'file'
      AND is_tombstone = FALSE
      AND payload->>'repo_id' <> ''
      AND payload->>'relative_path' <> ''
)
DELETE FROM content_files AS file
USING candidate_files AS candidate
WHERE file.repo_id = candidate.repo_id
  AND file.relative_path = candidate.relative_path
  AND NOT EXISTS (
      SELECT 1
      FROM fact_records AS retained
      WHERE retained.generation_id <> ALL($1::text[])
        AND retained.fact_kind = 'file'
        AND retained.is_tombstone = FALSE
        AND retained.payload->>'repo_id' = file.repo_id
        AND retained.payload->>'relative_path' = file.relative_path
  )
`

const deleteScopeGenerationsForRetentionQuery = `
DELETE FROM scope_generations
WHERE generation_id = ANY($1::text[])
`
