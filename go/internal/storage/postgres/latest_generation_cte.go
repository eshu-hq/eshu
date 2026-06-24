// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// latestGenerationCTE is the shared `WITH latest_generations AS (...)` common
// table expression that resolves the single active generation per ingestion
// scope. Every relationship-fact loader and the active-repository-generation
// lookup embed it verbatim so all of them agree on which generation is "latest".
//
// Selection contract (truth-preserving): for each scope the chosen generation_id
// is COALESCE(ingestion_scopes.active_generation_id, newest scope_generations row
// by (ingested_at DESC, generation_id DESC)). The active scope pointer always
// wins; only scopes without an active pointer fall back to the newest generation.
//
// Why DISTINCT ON, not a correlated subquery (issue #3704): the prior form
// computed the fallback with a per-scope correlated subquery
// (`SELECT generation_id ... WHERE candidate.scope_id = generation.scope_id
// ORDER BY ... LIMIT 1`) evaluated once per GROUP BY group. The planner could not
// estimate that correlated subplan's cardinality, so the plan carried a
// misestimated per-scope SubPlan. DISTINCT ON collapses the same selection into
// one ordered pass over scope_generations: ORDER BY
// (scope_id, ingested_at DESC, generation_id DESC) makes the first row per scope
// the newest generation, and COALESCE(active_generation_id, that newest id)
// reproduces the original precedence exactly. active_generation_id is a column of
// ingestion_scopes (one row per scope), so it is constant across a scope's
// generation rows and the COALESCE yields the same value the correlated form did.
//
// The `scope_generations_scope_latest_lookup_idx` index
// (scope_id, ingested_at DESC, generation_id DESC) lets Postgres satisfy the
// per-scope DISTINCT ON ordering without sorting the whole table.
//
// Scope of the win: this rewrite eliminates the planner-misestimated correlated
// subplan and restores a flat, parallelizable plan shape; it is a query-shape
// cleanup, NOT the fix for the multi-minute backfill long pole. Live measurement
// showed both query forms already execute sub-second over the corpus. The actual
// long pole is the serial client-side per-fact processing, addressed by the
// concurrent per-repository batch writes (writeDeferredBackfillInBatches in
// ingestion_backfill.go) and the multi-row evidence INSERT batching
// (UpsertEvidenceFacts in relationship_store.go).
const latestGenerationCTE = `WITH latest_generations AS (
    SELECT DISTINCT ON (generation.scope_id)
        generation.scope_id,
        COALESCE(
            scope.active_generation_id,
            generation.generation_id
        ) AS generation_id
    FROM scope_generations AS generation
    LEFT JOIN ingestion_scopes AS scope
      ON scope.scope_id = generation.scope_id
    ORDER BY generation.scope_id, generation.ingested_at DESC, generation.generation_id DESC
)`
