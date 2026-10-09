// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

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
// The sixteen cascade-child arms (#7396 plus #7751) count the cascade
// children of scope_generations the final DELETE removes implicitly:
// without them BatchRowLimit and the retention events under-count the
// prune. Each arm joins its table through scope_generations on both
// (scope_id, generation_id), the fact_records shape: a generation-only
// probe would skip-scan the two-column index once per candidate over every
// scope, with cost growing with the table instead of the batch. The #7751
// arm counts producer_activation_obligations through its generation-leading
// PRIMARY KEY (generation_id, scope_id), measured with EXPLAIN ANALYZE
// before landing like the #7700 arm.
//
// The sixteenth arm (#7700) counts admission_decision_evidence, a cascade
// grandchild: evidence rows hang off admission_decisions by decision_id, so
// the arm joins through the decision's (scope_id, generation_id) probe and
// then the evidence (decision_id) prefix. Both probes stay index-only and
// the cost follows the batch, measured with EXPLAIN ANALYZE before landing.
//
// The five #7784 arms count the remaining cascade grandchildren. The two
// package keys tables carry no (scope_id, generation_id) prefix index (only
// the ecosystem-first lookup), so their legs count through the generation's
// facts: fact_records_scope_generation_idx, then the keys PRIMARY KEY
// (fact_id, ...) prefix. The probe sits in a correlated scalar subquery,
// not a fourth LEFT JOIN: as a join the planner reorders the leg under a
// generic plan into a scope-wide fact bitmap plus a generation join filter,
// whose cost grows with the scope instead of the batch. The subquery keeps
// the (scope_id, generation_id) probe in every plan the guard checks.
// relationship_reference_candidate_keys has the scope prefix, so its leg
// joins scope_generations directly. content_file_secret_lines hangs off
// content_files, whose doomed keys already live in doomed_files: a secret
// row dies exactly when its key is doomed, so that arm joins doomed_files
// straight onto the secret PRIMARY KEY prefix. All five stay index-backed
// with cost following the batch, measured with EXPLAIN ANALYZE before
// landing like the #7700 arm.
//
// The unroutable leg is generation-only (#7799), not scope-joined: the
// table carries no foreign keys by design (migration 098: scope_id may be
// ” on legacy rows) and the reap DELETE matches on generation_id alone,
// so a scope-joined count misses malformed-scope rows the prune deletes.
// A generation-only probe of the scope-leading index would skip-scan once
// per candidate over every scope, so migration 167 adds a (generation_id)
// btree the leg and the DELETE both probe; measured with EXPLAIN ANALYZE
// before landing like the arms above.
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
UNION ALL
SELECT candidate.generation_id, 'activation_obligations' AS table_name, COUNT(row.generation_id) AS row_count
FROM generation_retention_row_counts AS candidate
LEFT JOIN scope_generations AS generation
  ON generation.generation_id = candidate.generation_id
LEFT JOIN activation_obligations AS row
  ON row.scope_id = generation.scope_id
 AND row.generation_id = candidate.generation_id
GROUP BY candidate.generation_id
UNION ALL
SELECT candidate.generation_id, 'producer_activation_obligations' AS table_name, COUNT(row.generation_id) AS row_count
FROM generation_retention_row_counts AS candidate
LEFT JOIN scope_generations AS generation
  ON generation.generation_id = candidate.generation_id
LEFT JOIN producer_activation_obligations AS row
  ON row.scope_id = generation.scope_id
 AND row.generation_id = candidate.generation_id
GROUP BY candidate.generation_id
UNION ALL
SELECT candidate.generation_id, 'admission_decisions' AS table_name, COUNT(row.generation_id) AS row_count
FROM generation_retention_row_counts AS candidate
LEFT JOIN scope_generations AS generation
  ON generation.generation_id = candidate.generation_id
LEFT JOIN admission_decisions AS row
  ON row.scope_id = generation.scope_id
 AND row.generation_id = candidate.generation_id
GROUP BY candidate.generation_id
UNION ALL
SELECT candidate.generation_id, 'admission_decision_evidence' AS table_name, COUNT(evidence.evidence_id) AS row_count
FROM generation_retention_row_counts AS candidate
LEFT JOIN scope_generations AS generation
  ON generation.generation_id = candidate.generation_id
LEFT JOIN admission_decisions AS decision
  ON decision.scope_id = generation.scope_id
 AND decision.generation_id = candidate.generation_id
LEFT JOIN admission_decision_evidence AS evidence
  ON evidence.decision_id = decision.decision_id
GROUP BY candidate.generation_id
UNION ALL
SELECT candidate.generation_id, 'code_reachability_rows' AS table_name, COUNT(row.generation_id) AS row_count
FROM generation_retention_row_counts AS candidate
LEFT JOIN scope_generations AS generation
  ON generation.generation_id = candidate.generation_id
LEFT JOIN code_reachability_rows AS row
  ON row.scope_id = generation.scope_id
 AND row.generation_id = candidate.generation_id
GROUP BY candidate.generation_id
UNION ALL
SELECT candidate.generation_id, 'code_reachability_repository_watermarks' AS table_name, COUNT(row.generation_id) AS row_count
FROM generation_retention_row_counts AS candidate
LEFT JOIN scope_generations AS generation
  ON generation.generation_id = candidate.generation_id
LEFT JOIN code_reachability_repository_watermarks AS row
  ON row.scope_id = generation.scope_id
 AND row.generation_id = candidate.generation_id
GROUP BY candidate.generation_id
UNION ALL
SELECT candidate.generation_id, 'code_root_verdicts' AS table_name, COUNT(row.generation_id) AS row_count
FROM generation_retention_row_counts AS candidate
LEFT JOIN scope_generations AS generation
  ON generation.generation_id = candidate.generation_id
LEFT JOIN code_root_verdicts AS row
  ON row.scope_id = generation.scope_id
 AND row.generation_id = candidate.generation_id
GROUP BY candidate.generation_id
UNION ALL
SELECT candidate.generation_id, 'container_image_identity_cutovers' AS table_name, COUNT(row.generation_id) AS row_count
FROM generation_retention_row_counts AS candidate
LEFT JOIN scope_generations AS generation
  ON generation.generation_id = candidate.generation_id
LEFT JOIN container_image_identity_cutovers AS row
  ON row.scope_id = generation.scope_id
 AND row.generation_id = candidate.generation_id
GROUP BY candidate.generation_id
UNION ALL
SELECT candidate.generation_id, 'deferred_backfill_partition_memo' AS table_name, COUNT(row.generation_id) AS row_count
FROM generation_retention_row_counts AS candidate
LEFT JOIN scope_generations AS generation
  ON generation.generation_id = candidate.generation_id
LEFT JOIN deferred_backfill_partition_memo AS row
  ON row.scope_id = generation.scope_id
 AND row.generation_id = candidate.generation_id
GROUP BY candidate.generation_id
UNION ALL
SELECT candidate.generation_id, 'eshu_search_document_projection_state' AS table_name, COUNT(row.generation_id) AS row_count
FROM generation_retention_row_counts AS candidate
LEFT JOIN scope_generations AS generation
  ON generation.generation_id = candidate.generation_id
LEFT JOIN eshu_search_document_projection_state AS row
  ON row.scope_id = generation.scope_id
 AND row.generation_id = candidate.generation_id
GROUP BY candidate.generation_id
UNION ALL
SELECT candidate.generation_id, 'eshu_search_index_documents' AS table_name, COUNT(row.generation_id) AS row_count
FROM generation_retention_row_counts AS candidate
LEFT JOIN scope_generations AS generation
  ON generation.generation_id = candidate.generation_id
LEFT JOIN eshu_search_index_documents AS row
  ON row.scope_id = generation.scope_id
 AND row.generation_id = candidate.generation_id
GROUP BY candidate.generation_id
UNION ALL
SELECT candidate.generation_id, 'eshu_search_index_stats' AS table_name, COUNT(row.generation_id) AS row_count
FROM generation_retention_row_counts AS candidate
LEFT JOIN scope_generations AS generation
  ON generation.generation_id = candidate.generation_id
LEFT JOIN eshu_search_index_stats AS row
  ON row.scope_id = generation.scope_id
 AND row.generation_id = candidate.generation_id
GROUP BY candidate.generation_id
UNION ALL
SELECT candidate.generation_id, 'eshu_search_index_terms' AS table_name, COUNT(row.generation_id) AS row_count
FROM generation_retention_row_counts AS candidate
LEFT JOIN scope_generations AS generation
  ON generation.generation_id = candidate.generation_id
LEFT JOIN eshu_search_index_terms AS row
  ON row.scope_id = generation.scope_id
 AND row.generation_id = candidate.generation_id
GROUP BY candidate.generation_id
UNION ALL
SELECT candidate.generation_id, 'eshu_search_vector_metadata' AS table_name, COUNT(row.generation_id) AS row_count
FROM generation_retention_row_counts AS candidate
LEFT JOIN scope_generations AS generation
  ON generation.generation_id = candidate.generation_id
LEFT JOIN eshu_search_vector_metadata AS row
  ON row.scope_id = generation.scope_id
 AND row.generation_id = candidate.generation_id
GROUP BY candidate.generation_id
UNION ALL
SELECT candidate.generation_id, 'eshu_search_vector_scope_state' AS table_name, COUNT(row.generation_id) AS row_count
FROM generation_retention_row_counts AS candidate
LEFT JOIN scope_generations AS generation
  ON generation.generation_id = candidate.generation_id
LEFT JOIN eshu_search_vector_scope_state AS row
  ON row.scope_id = generation.scope_id
 AND row.generation_id = candidate.generation_id
GROUP BY candidate.generation_id
UNION ALL
SELECT candidate.generation_id, 'eshu_search_vector_values' AS table_name, COUNT(row.generation_id) AS row_count
FROM generation_retention_row_counts AS candidate
LEFT JOIN scope_generations AS generation
  ON generation.generation_id = candidate.generation_id
LEFT JOIN eshu_search_vector_values AS row
  ON row.scope_id = generation.scope_id
 AND row.generation_id = candidate.generation_id
GROUP BY candidate.generation_id
UNION ALL
SELECT candidate.generation_id, 'reducer_input_invalid_facts' AS table_name, COUNT(row.generation_id) AS row_count
FROM generation_retention_row_counts AS candidate
LEFT JOIN scope_generations AS generation
  ON generation.generation_id = candidate.generation_id
LEFT JOIN reducer_input_invalid_facts AS row
  ON row.scope_id = generation.scope_id
 AND row.generation_id = candidate.generation_id
GROUP BY candidate.generation_id
UNION ALL
SELECT candidate.generation_id, 'package_manifest_consumption_keys' AS table_name,
  (SELECT COUNT(keys.fact_id)
   FROM fact_records AS fact
   JOIN package_manifest_consumption_keys AS keys
     ON keys.fact_id = fact.fact_id
   WHERE fact.scope_id = generation.scope_id
     AND fact.generation_id = candidate.generation_id) AS row_count
FROM generation_retention_row_counts AS candidate
LEFT JOIN scope_generations AS generation
  ON generation.generation_id = candidate.generation_id
UNION ALL
SELECT candidate.generation_id, 'package_registry_identity_keys' AS table_name,
  (SELECT COUNT(keys.fact_id)
   FROM fact_records AS fact
   JOIN package_registry_identity_keys AS keys
     ON keys.fact_id = fact.fact_id
   WHERE fact.scope_id = generation.scope_id
     AND fact.generation_id = candidate.generation_id) AS row_count
FROM generation_retention_row_counts AS candidate
LEFT JOIN scope_generations AS generation
  ON generation.generation_id = candidate.generation_id
UNION ALL
SELECT candidate.generation_id, 'relationship_reference_candidate_keys' AS table_name, COUNT(keys.fact_id) AS row_count
FROM generation_retention_row_counts AS candidate
LEFT JOIN scope_generations AS generation
  ON generation.generation_id = candidate.generation_id
LEFT JOIN relationship_reference_candidate_keys AS keys
  ON keys.scope_id = generation.scope_id
 AND keys.generation_id = candidate.generation_id
GROUP BY candidate.generation_id
UNION ALL
SELECT candidate.generation_id, 'content_file_secret_lines' AS table_name, COUNT(lines.line_number) AS row_count
FROM generation_retention_row_counts AS candidate
LEFT JOIN doomed_files AS file
  ON file.attributed_rank = candidate.rank
LEFT JOIN content_file_secret_lines AS lines
  ON lines.repo_id = file.repo_id
 AND lines.relative_path = file.relative_path
GROUP BY candidate.generation_id
UNION ALL
SELECT candidate.generation_id, 'shared_projection_unroutable_intents' AS table_name, COUNT(row.generation_id) AS row_count
FROM generation_retention_row_counts AS candidate
LEFT JOIN shared_projection_unroutable_intents AS row
  ON row.generation_id = candidate.generation_id
GROUP BY candidate.generation_id
`
