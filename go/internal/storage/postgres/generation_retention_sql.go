// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// generationRetentionCandidateQuery selects up to BatchGenerationLimit
// prunable superseded generations across every scope, oldest-first, and locks
// their owning scopes. One eligibility definition feeds both the lock set and
// the candidate list, so they cannot disagree; live_work is read once
// (MATERIALIZED) instead of probed per row (the #6809 class). locked_scopes
// orders oldest-eligible-first, scope_id tie-break, FOR UPDATE OF scope SKIP
// LOCKED only on ingestion_scopes, so a held scope is skipped and replaced
// without waiting. The final SELECT re-checks status, superseded_at and
// active_generation_id at lock time and is byte-identical to the prior
// statement. Rationale/proofs: docs/internal/evidence/7334-generation-retention-selection.md.
//
// $1 soft cutoff, $2 min newer superseded generations, $3 batch/lock-set
// limit, $4 hard-ceiling cutoff (#7585): the count preference cannot retain
// ordinary superseded history older than $4.
const generationRetentionCandidateQuery = `
WITH ranked_superseded_generations AS (
    SELECT
        generation.scope_id,
        generation.generation_id,
        generation.superseded_at,
        generation.observed_at,
        ROW_NUMBER() OVER (PARTITION BY generation.scope_id ORDER BY generation.superseded_at DESC, generation.generation_id DESC) AS superseded_rank
    FROM scope_generations AS generation
    WHERE generation.status = 'superseded'
      AND generation.superseded_at IS NOT NULL
),
live_work AS MATERIALIZED (
    SELECT DISTINCT work.generation_id
    FROM fact_work_items AS work
    WHERE work.status IN ('claimed', 'running', 'retrying')
),
eligible AS MATERIALIZED (
    SELECT ranked.scope_id, ranked.generation_id, scope.scope_kind, ranked.superseded_at, ranked.observed_at
    FROM ranked_superseded_generations AS ranked
    JOIN ingestion_scopes AS scope
      ON scope.scope_id = ranked.scope_id
    WHERE ranked.generation_id IS DISTINCT FROM scope.active_generation_id
      AND ((ranked.superseded_at < $1 AND ranked.superseded_rank > $2) OR ranked.superseded_at < $4)
      AND NOT EXISTS (
          SELECT 1
          FROM live_work
          WHERE live_work.generation_id = ranked.generation_id
      )
),
eligible_scopes AS (
    SELECT eligible.scope_id, min(eligible.superseded_at) AS oldest_superseded_at
    FROM eligible
    GROUP BY eligible.scope_id
),
locked_scopes AS (
    SELECT scope.scope_id
    FROM ingestion_scopes AS scope
    JOIN eligible_scopes ON eligible_scopes.scope_id = scope.scope_id
    ORDER BY eligible_scopes.oldest_superseded_at ASC, scope.scope_id ASC
    LIMIT $3
    FOR UPDATE OF scope SKIP LOCKED
),
eligible_generations AS (
    SELECT eligible.*
    FROM eligible
    JOIN locked_scopes AS locked_scope
      ON locked_scope.scope_id = eligible.scope_id
    ORDER BY eligible.superseded_at ASC, eligible.generation_id ASC
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

// Savepoint around candidate selection (arbiter ruling arb-7127-3d-c). Row
// locks are released by a rollback to a savepoint, so a batch of one
// generation admitted over BatchRowLimit can give back every scope and
// generation row the selection locked and keep only its own. SET LOCAL
// work_mem is issued before the savepoint, so the rollback keeps it.
const (
	generationRetentionSavepointStatement         = "SAVEPOINT retention_selection"
	generationRetentionReleaseSavepointStatement  = "RELEASE SAVEPOINT retention_selection"
	generationRetentionRollbackSavepointStatement = "ROLLBACK TO SAVEPOINT retention_selection"
)

// generationRetentionTargetedCandidateQuery re-locks one candidate after the
// selection's savepoint rollback: the candidate query's predicates for one
// (scope, generation) pair, FOR UPDATE OF generation, scope SKIP LOCKED, so
// the delete of an over-limit batch holds one scope row and one generation
// row. Rank > $2 is written as "at least $2 newer superseded generations",
// which is what ROW_NUMBER over (superseded_at, generation_id) DESC means. It
// is its own statement so the general candidate query's plan is untouched.
// No row means another session took, re-activated or started work on the
// candidate after the rollback; the pass then prunes nothing.
//
// $1 soft cutoff, $2 minimum newer generations, $3 excluded ids, $4 scope
// id, $5 generation id, $6 hard-ceiling cutoff (#7585).
const generationRetentionTargetedCandidateQuery = `-- retention: targeted candidate lock
SELECT generation.scope_id, generation.generation_id, scope.scope_kind, generation.superseded_at, generation.observed_at
FROM scope_generations AS generation
JOIN ingestion_scopes AS scope ON scope.scope_id = generation.scope_id
WHERE scope.scope_id = $4
  AND generation.scope_id = $4
  AND generation.generation_id = $5
  AND generation.status = 'superseded'
  AND generation.generation_id <> ALL($3::text[])
  AND generation.superseded_at IS NOT NULL
  AND generation.generation_id IS DISTINCT FROM scope.active_generation_id
  AND ((generation.superseded_at < $1 AND (
      SELECT count(*)
      FROM scope_generations AS newer
      WHERE newer.scope_id = generation.scope_id
        AND newer.status = 'superseded'
        AND newer.generation_id <> ALL($3::text[])
        AND newer.superseded_at IS NOT NULL
        AND (newer.superseded_at, newer.generation_id) > (generation.superseded_at, generation.generation_id)
  ) >= $2) OR generation.superseded_at < $6)
  AND NOT EXISTS (
      SELECT 1
      FROM fact_work_items AS work
      WHERE work.generation_id = generation.generation_id
        AND work.status IN ('claimed', 'running', 'retrying')
  )
FOR UPDATE OF generation, scope SKIP LOCKED
`

// generationRetentionRowCountsQuery reports, per candidate generation and
// table, the rows a retention batch over $1 deletes. Generation-owned tables
// count the generation's own rows. A content row (content_entities,
// content_files, content_file_references, and the infra_resource_entities
// mirror) can be named by facts in several candidates but is deleted once, so
// it is attributed to exactly one: the newest candidate naming its key, where
// newest is the last position in $1 (the store passes candidates oldest first).
// Per table the per-generation counts therefore sum to the rows the content
// prunes delete, and BatchRowLimit compares against that sum (#6809).
//
// Its cost follows the batch, not fact_records (#7279). candidate_* reads only
// the candidates' own facts, through scope_generations and the (scope_id,
// generation_id) prefix of fact_records_scope_generation_idx, and doomed_*
// keeps a key only when a NOT EXISTS probe of the key index
// (fact_records_content_entity_key_idx, fact_records_file_key_idx) finds no
// live fact outside $1. The fact_records arm is scope-joined the same way, so
// no plan skip-scans the index once per candidate over every scope. The earlier
// grouped pass read every live fact of a kind on every call while the scope
// locks were held. The store runs this only after it has checked both key
// indexes are valid; without them each probe would scan the table (#6809).
const generationRetentionRowCountsQuery = `
WITH generation_retention_row_counts AS (
    SELECT candidate.generation_id, candidate.rank
    FROM unnest($1::text[]) WITH ORDINALITY AS candidate(generation_id, rank)
),
candidate_files AS (
    SELECT fact.payload->>'repo_id' AS repo_id,
           fact.payload->>'relative_path' AS relative_path,
           max(candidate.rank) AS attributed_rank
    FROM generation_retention_row_counts AS candidate
    JOIN scope_generations AS generation
      ON generation.generation_id = candidate.generation_id
    JOIN fact_records AS fact
      ON fact.scope_id = generation.scope_id
     AND fact.generation_id = generation.generation_id
    WHERE fact.fact_kind = 'file'
      AND fact.is_tombstone = FALSE
      AND fact.payload->>'repo_id' <> ''
      AND fact.payload->>'relative_path' <> ''
    GROUP BY 1, 2
),
doomed_files AS (
    SELECT candidate_key.repo_id, candidate_key.relative_path, candidate_key.attributed_rank
    FROM candidate_files AS candidate_key
    WHERE NOT EXISTS (
        SELECT 1
        FROM fact_records AS retained
        WHERE retained.fact_kind = 'file'
          AND retained.is_tombstone = FALSE
          AND retained.payload->>'repo_id' = candidate_key.repo_id
          AND retained.payload->>'relative_path' = candidate_key.relative_path
          AND retained.generation_id <> ALL($1::text[])
    )
),
candidate_entities AS (
    SELECT fact.payload->>'repo_id' AS repo_id,
           fact.payload->>'entity_id' AS entity_id,
           max(candidate.rank) AS attributed_rank
    FROM generation_retention_row_counts AS candidate
    JOIN scope_generations AS generation
      ON generation.generation_id = candidate.generation_id
    JOIN fact_records AS fact
      ON fact.scope_id = generation.scope_id
     AND fact.generation_id = generation.generation_id
    WHERE fact.fact_kind = 'content_entity'
      AND fact.is_tombstone = FALSE
      AND fact.payload->>'repo_id' <> ''
      AND fact.payload->>'entity_id' <> ''
    GROUP BY 1, 2
),
doomed_entities AS (
    SELECT candidate_key.repo_id, candidate_key.entity_id, candidate_key.attributed_rank
    FROM candidate_entities AS candidate_key
    WHERE NOT EXISTS (
        SELECT 1
        FROM fact_records AS retained
        WHERE retained.fact_kind = 'content_entity'
          AND retained.is_tombstone = FALSE
          AND retained.payload->>'repo_id' = candidate_key.repo_id
          AND retained.payload->>'entity_id' = candidate_key.entity_id
          AND retained.generation_id <> ALL($1::text[])
    )
)
SELECT candidate.generation_id, 'fact_records' AS table_name, COUNT(row.generation_id) AS row_count
FROM generation_retention_row_counts AS candidate
LEFT JOIN scope_generations AS generation
  ON generation.generation_id = candidate.generation_id
LEFT JOIN fact_records AS row
  ON row.scope_id = generation.scope_id
 AND row.generation_id = candidate.generation_id
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

const deleteSharedProjectionIntentsForGenerationsQuery = `
DELETE FROM shared_projection_intents
WHERE generation_id = ANY($1::text[])
`

// deleteSharedProjectionUnroutableIntentsForGenerationsQuery reaps the durable
// unroutable-intent records alongside the intents they describe (#5984). The
// table deliberately carries no foreign keys -- shared_projection_intents.scope_id
// can be ” on legacy rows, so an FK would reject the insert exactly when a
// malformed row is being recorded -- which means it does not cascade and needs
// this explicit delete to avoid outliving its generation.
const deleteSharedProjectionUnroutableIntentsForGenerationsQuery = `
DELETE FROM shared_projection_unroutable_intents
WHERE generation_id = ANY($1::text[])
`

// pruneContentFileReferencesForGenerationsQuery deletes the content_file_references
// rows whose only live facts sit in the pruned generations (#7279). It reads
// the candidates' own file facts through the (scope_id, generation_id) index
// prefix and keeps a key when a NOT EXISTS probe of fact_records_file_key_idx
// finds a live fact in any generation outside $1, so its cost follows the batch
// and not fact_records. Without that index the probe would scan fact_records
// once per candidate key, the #6809 cliff, so the store refuses the cycle
// unless the index is valid.
const pruneContentFileReferencesForGenerationsQuery = `
WITH candidate_keys AS (
    SELECT DISTINCT fact.payload->>'repo_id' AS repo_id, fact.payload->>'relative_path' AS relative_path
    FROM scope_generations AS generation
    JOIN fact_records AS fact
      ON fact.scope_id = generation.scope_id
     AND fact.generation_id = generation.generation_id
    WHERE generation.generation_id = ANY($1::text[])
      AND fact.fact_kind = 'file' AND fact.is_tombstone = FALSE
      AND fact.payload->>'repo_id' <> '' AND fact.payload->>'relative_path' <> ''
),
doomed AS (
    SELECT candidate_key.repo_id, candidate_key.relative_path
    FROM candidate_keys AS candidate_key
    WHERE NOT EXISTS (
        SELECT 1
        FROM fact_records AS retained
        WHERE retained.fact_kind = 'file' AND retained.is_tombstone = FALSE
          AND retained.payload->>'repo_id' = candidate_key.repo_id
          AND retained.payload->>'relative_path' = candidate_key.relative_path
          AND retained.generation_id <> ALL($1::text[])
    )
)
DELETE FROM content_file_references AS t
USING doomed AS d
WHERE t.repo_id = d.repo_id AND t.relative_path = d.relative_path
`

// pruneContentEntitiesForGenerationsQuery deletes the content_entities
// rows whose only live facts sit in the pruned generations (#7279). It reads
// the candidates' own content_entity facts through the (scope_id, generation_id) index
// prefix and keeps a key when a NOT EXISTS probe of fact_records_content_entity_key_idx
// finds a live fact in any generation outside $1, so its cost follows the batch
// and not fact_records. Without that index the probe would scan fact_records
// once per candidate key, the #6809 cliff, so the store refuses the cycle
// unless the index is valid.
const pruneContentEntitiesForGenerationsQuery = `
WITH candidate_keys AS (
    SELECT DISTINCT fact.payload->>'repo_id' AS repo_id, fact.payload->>'entity_id' AS entity_id
    FROM scope_generations AS generation
    JOIN fact_records AS fact
      ON fact.scope_id = generation.scope_id
     AND fact.generation_id = generation.generation_id
    WHERE generation.generation_id = ANY($1::text[])
      AND fact.fact_kind = 'content_entity' AND fact.is_tombstone = FALSE
      AND fact.payload->>'repo_id' <> '' AND fact.payload->>'entity_id' <> ''
),
doomed AS (
    SELECT candidate_key.repo_id, candidate_key.entity_id
    FROM candidate_keys AS candidate_key
    WHERE NOT EXISTS (
        SELECT 1
        FROM fact_records AS retained
        WHERE retained.fact_kind = 'content_entity' AND retained.is_tombstone = FALSE
          AND retained.payload->>'repo_id' = candidate_key.repo_id
          AND retained.payload->>'entity_id' = candidate_key.entity_id
          AND retained.generation_id <> ALL($1::text[])
    )
)
DELETE FROM content_entities AS t
USING doomed AS d
WHERE t.repo_id = d.repo_id AND t.entity_id = d.entity_id
`

// pruneContentFilesForGenerationsQuery deletes the content_files
// rows whose only live facts sit in the pruned generations (#7279). It reads
// the candidates' own file facts through the (scope_id, generation_id) index
// prefix and keeps a key when a NOT EXISTS probe of fact_records_file_key_idx
// finds a live fact in any generation outside $1, so its cost follows the batch
// and not fact_records. Without that index the probe would scan fact_records
// once per candidate key, the #6809 cliff, so the store refuses the cycle
// unless the index is valid.
const pruneContentFilesForGenerationsQuery = `
WITH candidate_keys AS (
    SELECT DISTINCT fact.payload->>'repo_id' AS repo_id, fact.payload->>'relative_path' AS relative_path
    FROM scope_generations AS generation
    JOIN fact_records AS fact
      ON fact.scope_id = generation.scope_id
     AND fact.generation_id = generation.generation_id
    WHERE generation.generation_id = ANY($1::text[])
      AND fact.fact_kind = 'file' AND fact.is_tombstone = FALSE
      AND fact.payload->>'repo_id' <> '' AND fact.payload->>'relative_path' <> ''
),
doomed AS (
    SELECT candidate_key.repo_id, candidate_key.relative_path
    FROM candidate_keys AS candidate_key
    WHERE NOT EXISTS (
        SELECT 1
        FROM fact_records AS retained
        WHERE retained.fact_kind = 'file' AND retained.is_tombstone = FALSE
          AND retained.payload->>'repo_id' = candidate_key.repo_id
          AND retained.payload->>'relative_path' = candidate_key.relative_path
          AND retained.generation_id <> ALL($1::text[])
    )
)
DELETE FROM content_files AS t
USING doomed AS d
WHERE t.repo_id = d.repo_id AND t.relative_path = d.relative_path
`

const deleteScopeGenerationsForRetentionQuery = `
DELETE FROM scope_generations
WHERE generation_id = ANY($1::text[])
`

// generationRetentionKeyIndexesQuery returns the name of every retention key
// index that is not a valid, ready, non-unique two-column btree over the
// expected key expressions with the retention statements' partial predicate.
// It reads only pg_catalog, so it takes no lock on fact_records. to_regclass
// resolves both names through the session's search_path, as the retention
// statements do.
const generationRetentionKeyIndexesQuery = `
WITH generation_retention_key_indexes(index_name, key_column, fact_kind) AS (
    VALUES
        ('fact_records_content_entity_key_idx', 'entity_id', 'content_entity'),
        ('fact_records_file_key_idx', 'relative_path', 'file')
)
SELECT expected.index_name
FROM generation_retention_key_indexes AS expected
WHERE NOT EXISTS (
    SELECT 1
    FROM pg_index AS index_state
    JOIN pg_class AS index_relation ON index_relation.oid = index_state.indexrelid
    JOIN pg_am AS access_method ON access_method.oid = index_relation.relam
    WHERE index_state.indexrelid = to_regclass(expected.index_name)
      AND index_state.indrelid = to_regclass('fact_records')
      AND access_method.amname = 'btree'
      AND index_state.indisvalid
      AND index_state.indisready
      AND NOT index_state.indisunique
      AND index_state.indnkeyatts = 2
      AND index_state.indnatts = 2
      AND pg_get_expr(index_state.indexprs, index_state.indrelid)
          = format('(payload ->> ''repo_id''::text), (payload ->> %L::text)', expected.key_column)
      AND pg_get_expr(index_state.indpred, index_state.indrelid)
          = format('((fact_kind = %L::text) AND (is_tombstone = false))', expected.fact_kind)
)
ORDER BY expected.index_name
`

// execRowsAffected runs one retention statement and returns its affected row
// count.
func execRowsAffected(ctx context.Context, exec db.Executor, query string, args ...any) (int64, error) {
	result, err := exec.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	return affected, nil
}
