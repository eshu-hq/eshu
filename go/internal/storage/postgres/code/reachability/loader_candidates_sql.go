// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reachabilitystore

// CompleteRunGateSQL is the predicate that admits an active acceptance run to
// reachability projection only when its code-edge set is provably complete.
// It is the same check the dead-code query's run_gate applies before binding
// incoming edges to the acceptance run (deadCodeIncomingBoundQuery in
// internal/query), written against the loader's aliases: `acceptance` is the
// shared_projection_acceptance row and `generation` its scope_generations row.
// A run passes when:
//
//   - the generation is full (a delta generation carries only changed files);
//   - both reducer materialization work items, code_call_materialization and
//     inheritance_materialization, succeeded and none of them is in any other
//     status (a domain that never ran has emitted no intents);
//   - no code_calls or inheritance_edges intent of the generation for the
//     repository is still pending (migration 108's partial index).
//
// A drift test in internal/query derives the dead-code run_gate from its
// statement and requires it to equal this text after alias mapping, so the
// two gates cannot disagree about which runs are complete.
const CompleteRunGateSQL = `NOT generation.is_delta
      AND EXISTS (
          SELECT 1 FROM fact_work_items AS work
          WHERE work.scope_id = acceptance.scope_id
            AND work.generation_id = acceptance.generation_id
            AND work.stage = 'reducer'
            AND work.domain = 'code_call_materialization'
            AND work.status = 'succeeded')
      AND EXISTS (
          SELECT 1 FROM fact_work_items AS work
          WHERE work.scope_id = acceptance.scope_id
            AND work.generation_id = acceptance.generation_id
            AND work.stage = 'reducer'
            AND work.domain = 'inheritance_materialization'
            AND work.status = 'succeeded')
      AND NOT EXISTS (
          SELECT 1 FROM fact_work_items AS work
          WHERE work.scope_id = acceptance.scope_id
            AND work.generation_id = acceptance.generation_id
            AND work.stage = 'reducer'
            AND work.domain IN ('code_call_materialization', 'inheritance_materialization')
            AND work.status <> 'succeeded')
      AND NOT EXISTS (
          SELECT 1 FROM shared_projection_intents AS pending
          WHERE pending.generation_id = acceptance.generation_id
            AND pending.completed_at IS NULL
            AND pending.repository_id = acceptance.acceptance_unit_id
            AND pending.projection_domain IN ('code_calls', 'inheritance_edges'))`

// listPendingCodeReachabilityInputsSQL selects the bounded batch of active
// acceptance runs whose reachability is missing, older than their newest
// completed code-edge intent, or stamped under an older verdict-schema epoch
// ($2), ordered completed_at ASC, repository_id ASC, limited to $1.
//
// Shape (#7547): the `ready` CTE first narrows acceptance rows to active,
// complete runs (CompleteRunGateSQL); then one LATERAL max(completed_at) per
// ready run reads its completed intents, and the watermark is joined once per
// run. The pre-#7547 statement joined every completed intent row to the
// watermark and grouped afterwards, which on the QA replica meant ~497k intent
// rows, a per-row watermark probe, and a 112 MB external sort every poll. The
// row set for complete runs is unchanged: shared_projection_acceptance is
// unique per (scope_id, acceptance_unit_id, source_run_id) and the watermark
// per (scope_id, generation_id, repository_id), so the old GROUP BY and max()
// over the watermark collapsed nothing the per-run form does not.
const listPendingCodeReachabilityInputsSQL = `
WITH ready AS MATERIALIZED (
    SELECT acceptance.scope_id,
           acceptance.acceptance_unit_id,
           acceptance.source_run_id,
           acceptance.generation_id
    FROM shared_projection_acceptance AS acceptance
    JOIN ingestion_scopes AS scope
      ON scope.scope_id = acceptance.scope_id
     AND scope.active_generation_id = acceptance.generation_id
    JOIN scope_generations AS generation
      ON generation.generation_id = acceptance.generation_id
     AND generation.status = 'active'
    WHERE ` + CompleteRunGateSQL + `
), candidate AS (
    SELECT ready.scope_id,
           ready.acceptance_unit_id AS repository_id,
           ready.source_run_id,
           ready.generation_id,
           latest.completed_at,
           watermark.updated_at AS reach_updated_at,
           watermark.verdict_schema_epoch AS reach_verdict_epoch
    FROM ready
    CROSS JOIN LATERAL (
        SELECT max(intent.completed_at) AS completed_at
        FROM shared_projection_intents AS intent
        WHERE intent.scope_id = ready.scope_id
          AND intent.acceptance_unit_id = ready.acceptance_unit_id
          AND intent.source_run_id = ready.source_run_id
          AND intent.generation_id = ready.generation_id
          AND intent.projection_domain IN ('code_calls', 'inheritance_edges')
          AND intent.completed_at IS NOT NULL
    ) AS latest
    LEFT JOIN code_reachability_repository_watermarks AS watermark
      ON watermark.scope_id = ready.scope_id
     AND watermark.generation_id = ready.generation_id
     AND watermark.repository_id = ready.acceptance_unit_id
    WHERE latest.completed_at IS NOT NULL
)
SELECT scope_id, repository_id, source_run_id, generation_id, completed_at
FROM candidate
WHERE reach_updated_at IS NULL
   OR completed_at > reach_updated_at
   OR coalesce(reach_verdict_epoch, 0) < $2
ORDER BY completed_at ASC, repository_id ASC
LIMIT $1
`
