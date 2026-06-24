// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// listGenerationLifecycleQuery returns one bounded, ordered page of scope
// generation lifecycle rows joined with their owning scope identity, the
// per-generation fact_work_items queue rollup, and the latest per-generation
// failure.
//
// Bounding and ordering contract:
//   - Every optional filter is bypassed when its parameter is the empty string
//     (or, for status, the empty string). A blank scope_id / repository /
//     generation_id / collector_kind / source_system / status matches all rows
//     for that predicate, so the handler enforces scope expectations.
//   - The queue rollup is computed by a correlated LATERAL aggregate over
//     fact_work_items for the exact (scope_id, generation_id) pair, so counts
//     never leak across generations.
//   - The latest failure is the most recently updated fact_work_items row for
//     the generation that carries a failure_class, chosen by a LATERAL
//     ORDER BY updated_at DESC, work_item_id DESC LIMIT 1.
//   - Ordering is deterministic: observed_at DESC, generation_id ASC. The
//     handler fetches limit+1 rows to compute truncation; the storage layer
//     applies the LIMIT verbatim.
//
// Parameter order:
//
//	$1 scope_id         (empty bypasses)
//	$2 repository       (empty bypasses; matches source_key for repository scopes)
//	$3 collector_kind   (empty bypasses)
//	$4 source_system    (empty bypasses)
//	$5 generation_id    (empty bypasses)
//	$6 status           (empty bypasses)
//	$7 limit            (row cap; handler passes page limit + 1)
const listGenerationLifecycleQuery = `
SELECT
    generation.scope_id,
    generation.generation_id,
    scope.scope_kind,
    scope.source_system,
    scope.collector_kind,
    COALESCE(scope.active_generation_id, '') AS current_active_generation_id,
    (scope.active_generation_id IS NOT NULL
        AND scope.active_generation_id = generation.generation_id) AS is_active,
    generation.trigger_kind,
    COALESCE(generation.freshness_hint, '') AS freshness_hint,
    generation.status,
    generation.observed_at,
    generation.ingested_at,
    generation.activated_at,
    generation.superseded_at,
    COALESCE(queue.total_count, 0) AS total_count,
    COALESCE(queue.outstanding_count, 0) AS outstanding_count,
    COALESCE(queue.in_flight_count, 0) AS in_flight_count,
    COALESCE(queue.retrying_count, 0) AS retrying_count,
    COALESCE(queue.succeeded_count, 0) AS succeeded_count,
    COALESCE(queue.failed_count, 0) AS failed_count,
    COALESCE(queue.dead_letter_count, 0) AS dead_letter_count,
    COALESCE(failure.failure_class, '') AS failure_class,
    COALESCE(failure.failure_message, '') AS failure_message,
    COALESCE(failure.status, '') AS failure_work_item_status,
    failure.updated_at AS failure_observed_at
FROM scope_generations AS generation
JOIN ingestion_scopes AS scope
    ON scope.scope_id = generation.scope_id
LEFT JOIN LATERAL (
    SELECT
        COUNT(*) AS total_count,
        COUNT(*) FILTER (WHERE work.status IN ('pending', 'claimed', 'running', 'retrying')) AS outstanding_count,
        COUNT(*) FILTER (WHERE work.status IN ('claimed', 'running')) AS in_flight_count,
        COUNT(*) FILTER (WHERE work.status = 'retrying') AS retrying_count,
        COUNT(*) FILTER (WHERE work.status = 'succeeded') AS succeeded_count,
        COUNT(*) FILTER (WHERE work.status = 'failed') AS failed_count,
        COUNT(*) FILTER (WHERE work.status = 'dead_letter') AS dead_letter_count
    FROM fact_work_items AS work
    WHERE work.scope_id = generation.scope_id
      AND work.generation_id = generation.generation_id
) AS queue ON TRUE
LEFT JOIN LATERAL (
    SELECT
        work.failure_class,
        work.failure_message,
        work.status,
        work.updated_at
    FROM fact_work_items AS work
    WHERE work.scope_id = generation.scope_id
      AND work.generation_id = generation.generation_id
      AND work.failure_class IS NOT NULL
      AND work.failure_class <> ''
    ORDER BY work.updated_at DESC, work.work_item_id DESC
    LIMIT 1
) AS failure ON TRUE
WHERE ($1 = '' OR generation.scope_id = $1)
  AND ($2 = '' OR (scope.scope_kind = 'repository' AND scope.source_key = $2))
  AND ($3 = '' OR scope.collector_kind = $3)
  AND ($4 = '' OR scope.source_system = $4)
  AND ($5 = '' OR generation.generation_id = $5)
  AND ($6 = '' OR generation.status = $6)
ORDER BY generation.observed_at DESC, generation.generation_id ASC
LIMIT $7
`
