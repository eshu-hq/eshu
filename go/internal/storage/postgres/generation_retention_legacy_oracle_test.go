// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// The legacy statements below are the #6809 grouped-pass retention SQL exactly
// as origin/main 98394122fa shipped it, before #7279 replaced it with the
// per-candidate key probe. They are frozen on purpose: they are the exactness
// oracle the probe statements are compared against row for row
// (TestGenerationRetentionProbeMatchesGroupedPassLive), and the performance
// baseline the scale and contention proofs measure. Never edit them to follow
// production; production is what they check.

// legacyGenerationRetentionRowCountsQuery is the frozen #6809 form of generationRetentionRowCountsQuery.
const legacyGenerationRetentionRowCountsQuery = `
WITH generation_retention_row_counts AS (
    SELECT candidate.generation_id, candidate.rank
    FROM unnest($1::text[]) WITH ORDINALITY AS candidate(generation_id, rank)
),
doomed_files AS (
    SELECT row.payload->>'repo_id' AS repo_id,
           row.payload->>'relative_path' AS relative_path,
           max(candidate.rank) AS attributed_rank
    FROM fact_records AS row
    LEFT JOIN generation_retention_row_counts AS candidate
      ON candidate.generation_id = row.generation_id
    WHERE row.fact_kind = 'file'
      AND row.is_tombstone = FALSE
      AND row.payload->>'repo_id' <> ''
      AND row.payload->>'relative_path' <> ''
    GROUP BY 1, 2
    HAVING bool_or(candidate.rank IS NOT NULL)
       AND NOT bool_or(candidate.rank IS NULL)
),
doomed_entities AS (
    SELECT row.payload->>'repo_id' AS repo_id,
           row.payload->>'entity_id' AS entity_id,
           max(candidate.rank) AS attributed_rank
    FROM fact_records AS row
    LEFT JOIN generation_retention_row_counts AS candidate
      ON candidate.generation_id = row.generation_id
    WHERE row.fact_kind = 'content_entity'
      AND row.is_tombstone = FALSE
      AND row.payload->>'repo_id' <> ''
      AND row.payload->>'entity_id' <> ''
    GROUP BY 1, 2
    HAVING bool_or(candidate.rank IS NOT NULL)
       AND NOT bool_or(candidate.rank IS NULL)
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
SELECT candidate.generation_id, 'iac_reachability_rows' AS table_name, COUNT(row.generation_id) AS row_count
FROM generation_retention_row_counts AS candidate
LEFT JOIN iac_reachability_rows AS row
  ON candidate.generation_id = row.generation_id
GROUP BY candidate.generation_id
UNION ALL
SELECT candidate.generation_id, 'shared_projection_intents' AS table_name, COUNT(row.generation_id) AS row_count
FROM generation_retention_row_counts AS candidate
LEFT JOIN shared_projection_intents AS row
  ON candidate.generation_id = row.generation_id
GROUP BY candidate.generation_id
UNION ALL
SELECT candidate.generation_id, 'content_file_references' AS table_name, COUNT(ref.repo_id) AS row_count
FROM generation_retention_row_counts AS candidate
LEFT JOIN doomed_files AS file
  ON file.attributed_rank = candidate.rank
LEFT JOIN content_file_references AS ref
  ON ref.repo_id = file.repo_id
 AND ref.relative_path = file.relative_path
GROUP BY candidate.generation_id
UNION ALL
SELECT candidate.generation_id, 'content_entities' AS table_name, COUNT(entity.entity_id) AS row_count
FROM generation_retention_row_counts AS candidate
LEFT JOIN doomed_entities AS candidate_entity
  ON candidate_entity.attributed_rank = candidate.rank
LEFT JOIN content_entities AS entity
  ON entity.repo_id = candidate_entity.repo_id
 AND entity.entity_id = candidate_entity.entity_id
GROUP BY candidate.generation_id
UNION ALL
SELECT candidate.generation_id, 'infra_resource_entities' AS table_name, COUNT(mirror.entity_id) AS row_count
FROM generation_retention_row_counts AS candidate
LEFT JOIN doomed_entities AS candidate_entity
  ON candidate_entity.attributed_rank = candidate.rank
LEFT JOIN infra_resource_entities AS mirror
  ON mirror.entity_id = candidate_entity.entity_id
 AND mirror.repo_id = candidate_entity.repo_id
GROUP BY candidate.generation_id
UNION ALL
SELECT candidate.generation_id, 'content_files' AS table_name, COUNT(content_file.relative_path) AS row_count
FROM generation_retention_row_counts AS candidate
LEFT JOIN doomed_files AS file
  ON file.attributed_rank = candidate.rank
LEFT JOIN content_files AS content_file
  ON content_file.repo_id = file.repo_id
 AND content_file.relative_path = file.relative_path
GROUP BY candidate.generation_id
`

// legacyPruneContentFileReferencesForGenerationsQuery is the frozen #6809 form of pruneContentFileReferencesForGenerationsQuery.
const legacyPruneContentFileReferencesForGenerationsQuery = `
WITH doomed AS (
    SELECT payload->>'repo_id' AS repo_id, payload->>'relative_path' AS relative_path
    FROM fact_records
    WHERE fact_kind = 'file' AND is_tombstone = FALSE
      AND payload->>'repo_id' <> '' AND payload->>'relative_path' <> ''
    GROUP BY 1, 2
    HAVING bool_or(generation_id = ANY($1::text[]))
       AND NOT bool_or(generation_id <> ALL($1::text[]))
)
DELETE FROM content_file_references AS t
USING doomed AS d
WHERE t.repo_id = d.repo_id AND t.relative_path = d.relative_path
`

// legacyPruneContentEntitiesForGenerationsQuery is the frozen #6809 form of pruneContentEntitiesForGenerationsQuery.
const legacyPruneContentEntitiesForGenerationsQuery = `
WITH doomed AS (
    SELECT payload->>'repo_id' AS repo_id, payload->>'entity_id' AS entity_id
    FROM fact_records
    WHERE fact_kind = 'content_entity' AND is_tombstone = FALSE
      AND payload->>'repo_id' <> '' AND payload->>'entity_id' <> ''
    GROUP BY 1, 2
    HAVING bool_or(generation_id = ANY($1::text[]))
       AND NOT bool_or(generation_id <> ALL($1::text[]))
)
DELETE FROM content_entities AS t
USING doomed AS d
WHERE t.repo_id = d.repo_id AND t.entity_id = d.entity_id
`

// legacyPruneContentFilesForGenerationsQuery is the frozen #6809 form of pruneContentFilesForGenerationsQuery.
const legacyPruneContentFilesForGenerationsQuery = `
WITH doomed AS (
    SELECT payload->>'repo_id' AS repo_id, payload->>'relative_path' AS relative_path
    FROM fact_records
    WHERE fact_kind = 'file' AND is_tombstone = FALSE
      AND payload->>'repo_id' <> '' AND payload->>'relative_path' <> ''
    GROUP BY 1, 2
    HAVING bool_or(generation_id = ANY($1::text[]))
       AND NOT bool_or(generation_id <> ALL($1::text[]))
)
DELETE FROM content_files AS t
USING doomed AS d
WHERE t.repo_id = d.repo_id AND t.relative_path = d.relative_path
`

// legacyRetentionStatements are the frozen #6809 lock-window statements, in
// the order and under the names of retentionPlanStatements, so the scale proof
// can measure the before and after shapes on one fixture.
var legacyRetentionStatements = []retentionStatement{
	{name: "row_counts", sql: legacyGenerationRetentionRowCountsQuery},
	{name: "prune_refs", sql: legacyPruneContentFileReferencesForGenerationsQuery},
	{name: "prune_entities", sql: legacyPruneContentEntitiesForGenerationsQuery},
	{name: "prune_files", sql: legacyPruneContentFilesForGenerationsQuery},
}

// legacyGenerationRetentionCandidateQuery is the frozen pre-#7334 form of
// generationRetentionCandidateQuery exactly as origin/main shipped it before
// arbiter ruling arb-7334.md's fix. locked_scopes here checks only that a
// scope owns SOME old superseded generation, not that the generation is
// actually rank/live-work eligible, so it can starve the whole scope lock
// budget on scopes that sort first by scope_id but own no eligible
// generation at all (arb-7334.md E1/E3-old). It is the RED oracle
// TestGenerationRetentionSelectsEligibleGenerationsAcrossScopesLive runs
// against the fix's own fixture. Frozen on purpose: never edit it to follow
// production.
const legacyGenerationRetentionCandidateQuery = `
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
