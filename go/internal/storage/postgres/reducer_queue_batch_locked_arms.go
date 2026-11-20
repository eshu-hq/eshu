// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// Lock-arm CTEs of the batch claim, split out of the main batch-claim
// query so that file stays under the 500-line cap (and no single
// function trips the funlen gate). The assembled SQL is byte-identical
// to the inline form: the reserved arm opens with the newline the
// gate CTEs closing `),` line used to carry, the rest arm opens bare
// after the interpolated `,` line, and each arm closes with its own
// `)`. Verified by the pinned ORDER BY, LIMIT, and rejoin-shape tests
// plus the live claim tests.
// reducerClaimBatchReservedArmCTE renders the reserved single
// search-doc row arm (at most one row, LIMIT 1).
func reducerClaimBatchReservedArmCTE() string {
	return `
locked_search_doc_reserved AS MATERIALIZED (
    -- #10568 search_doc_reserved: one reserved claim slot per batch for the
    -- oldest ready eshu_search_document candidate. The claim orders by
    -- reducer_domain_priority ASC first and eshu_search_document is the only
    -- priority-1 domain, so any priority-0 backlog deeper than the batch LIMIT
    -- starves it outright. The reserved arm takes at most one row (k = 1); the
    -- rest arm below fills the remaining LIMIT - 1 slots with the unchanged
    -- ordering, and takes the full LIMIT when no search-doc candidate is ready
    -- (zero idle capacity loss). Advisory impact is bounded structurally: at
    -- most 1 batch slot per claim whenever a search-doc row is ready, 0
    -- otherwise, regardless of advisory backlog depth or worker count.
    -- This arm locks via the same lock_target predicates with identical
    -- EvalPlanQual row-self lease/visibility/status recheck as locked_rest,
    -- so a row another worker claimed between our snapshot and this lock is
    -- dropped in either arm. SKIP LOCKED never blocks, so the second locking
    -- arm adds no deadlock edge: workers still never wait on each other.
    -- MATERIALIZED: the rest arm anti-joins this output, so it must be
    -- evaluated once with its locks taken once, not inlined per reference.
    -- The lock is a per-candidate LATERAL primary-key probe, not a join the
    -- planner can reorder: referencing candidate from two arms materializes
    -- it, and with stale statistics a plain join flips to an outer scan over
    -- every lock_target row with the candidate re-scanned per row (measured
    -- +37 ms of lock tail on the stale-heavy fixture, unbounded at
    -- production table size). LATERAL fixes the candidate side as the outer
    -- loop whatever the statistics say. The ordered subquery (OFFSET 0
    -- keeps its order as the outer loop's path) lets LIMIT stop pulling
    -- candidates once the arm fills, so rows are lock-probed in claim order
    -- and SKIP LOCKED passes over a concurrently locked candidate to the
    -- next one exactly as the plain join did.
    SELECT ordered_candidate.*
    FROM (
        -- #10972: the gate-boosted slot lives in the rest arm only; this
        -- arm takes at most one priority-1 (eshu_search_document) row, and
        -- gating generations are scope-namespaced baseline generations that
        -- do not match a search-doc row's generation in practice, so the constant keeps
        -- the UNION shape without changing what this arm admits.
        SELECT candidate.*, 1 AS gate_boost
        FROM candidate
        WHERE candidate.reducer_domain_priority = 1
        ORDER BY candidate.reclaim_rank ASC, candidate.updated_at ASC, candidate.work_item_id ASC
        OFFSET 0
    ) AS ordered_candidate
    JOIN LATERAL (
        SELECT lock_target.work_item_id
        FROM fact_work_items AS lock_target
        WHERE lock_target.stage = 'reducer'
          AND lock_target.work_item_id = ordered_candidate.work_item_id
          -- #10863: recheck the stale-generation gate on the locked row,
          -- beside the lease rechecks below, so hand-out refusal does not
          -- rest on the snapshot candidate set alone.
          AND NOT EXISTS (
              SELECT 1
              FROM stale_reducer_candidates AS gate_stale
              WHERE gate_stale.work_item_id = lock_target.work_item_id
          )
          AND lock_target.status IN ('pending', 'retrying', 'claimed', 'running')
          AND (lock_target.claim_until IS NULL OR lock_target.claim_until <= $1)
          -- #6828: stamp-clock visibility, matching the base candidate filter
          -- above; a Go-only recheck here would reintroduce the skew flake by
          -- dropping trigger-reopened rows the base filter admitted.
          AND (lock_target.visible_at IS NULL OR lock_target.visible_at <= $1 OR (lock_target.status = 'pending' AND lock_target.visible_at <= clock_timestamp()))
        FOR UPDATE OF lock_target SKIP LOCKED
    ) AS lock_target ON true
    ORDER BY ordered_candidate.reclaim_rank ASC, ordered_candidate.updated_at ASC, ordered_candidate.work_item_id ASC
    LIMIT 1
)`
}

// reducerClaimBatchRestArmCTE renders the rest-arm fair-rank sweep,
// including the #10972 gate-boosted first slot.
func reducerClaimBatchRestArmCTE() string {
	return `locked_rest AS (
    -- Same per-candidate LATERAL lock as the reserved arm (see its comment):
    -- the candidate side stays the outer loop and lock_target is probed by
    -- primary key, so stale statistics cannot flip this arm into an outer
    -- scan over fact_work_items.
    SELECT ordered_candidate.*
    FROM (
        SELECT candidate.*,
            -- #10972: gate boost. At most one boosted row per claim: among
            -- live candidates of gate-waited generations, only the
            -- oldest-waiting generation's oldest row sorts first
            -- (gate_boost 0); every other row, gating or not, keeps its
            -- ordinary key (gate_boost 1), so non-gating relative order is
            -- unchanged. A drained or completed year contributes no live
            -- candidate rows, so the tier auto-expires. This only reorders
            -- for the lock loop below; admission still runs every
            -- claim-time fence. The named window keeps the PARTITION BY /
            -- ORDER BY beside the CASE that consumes them.
            CASE WHEN gate.waited_generation_id IS NOT NULL AND ROW_NUMBER() OVER w_gate_boost = 1
                THEN 0 ELSE 1 END AS gate_boost
        FROM candidate
        LEFT JOIN gate_waited_generations AS gate
          ON gate.waited_generation_id = candidate.generation_id
        -- Anti-join the reserved row so it is never claimed twice in one
        -- batch. A LEFT JOIN ... IS NULL keeps the lock/claim tail free of
        -- per-row SubPlan gates (NOT EXISTS would add one); the join is on
        -- this statement's own reserved output, so no concurrent-write
        -- hazard.
        LEFT JOIN locked_search_doc_reserved AS search_doc_taken
          ON search_doc_taken.work_item_id = candidate.work_item_id
        WHERE search_doc_taken.work_item_id IS NULL
        WINDOW w_gate_boost AS (
            PARTITION BY (gate.waited_generation_id IS NOT NULL)
            ORDER BY gate.gate_wait_since ASC NULLS LAST, candidate.updated_at ASC, candidate.work_item_id ASC
        )
        -- #10865: reclaim_rank ASC leads every key below, so an expired
        -- claimed/running hold takes a slot ahead of ordinary waiting rows;
        -- the keys below keep their relative order, so pending-only batches
        -- claim exactly as before.
        -- #8024: reducer_domain_inflight_count ASC sits after the fairness
        -- ranks and before the updated_at tie-break, so a ready row of a
        -- domain with no inflight work takes a slot ahead of an older
        -- same-source backlog without disturbing the #3385 cross-domain
        -- interleave (which ties at 0 = 0 when neither domain holds leases).
        -- gate_boost stays unqualified on purpose: it is this SELECT's own
        -- output alias, not a candidate column, so a candidate. prefix would
        -- fail to resolve (same reason the outer ORDER BY below stays
        -- unqualified per #10568).
        ORDER BY candidate.reclaim_rank ASC, gate_boost ASC, candidate.reducer_domain_priority ASC, candidate.reducer_source_inflight_count ASC, candidate.reducer_source_fair_rank ASC, candidate.reducer_domain_fair_rank ASC, candidate.reducer_domain_inflight_count ASC, candidate.updated_at ASC, candidate.work_item_id ASC
        OFFSET 0
    ) AS ordered_candidate
    JOIN LATERAL (
        SELECT lock_target.work_item_id
        FROM fact_work_items AS lock_target
        -- Re-apply the row-self lease/visibility/status predicates on the
        -- ACTUAL locked row. candidate is evaluated from the statement
        -- snapshot, but under Read Committed the FOR UPDATE row lock
        -- re-fetches the latest committed version of lock_target and re-runs
        -- THIS locking level's quals against it (EvalPlanQual recheck).
        -- Without these predicates the only qual is the id join to the
        -- snapshot candidate set, so a row another worker claimed and
        -- committed between our snapshot and this lock still matches and its
        -- fresh lease would be overwritten (lease theft / duplicate work).
        -- Re-checking status and claim_until/visible_at here drops such a
        -- row exactly as the pre-rewrite query did when FOR UPDATE SKIP
        -- LOCKED sat directly on the predicate-bearing candidate SELECT.
        -- These are a strict subset of base's WHERE, so in the uncontended
        -- case (lock_target == the snapshot row) they are a no-op and the
        -- claimed candidate set is unchanged.
        WHERE lock_target.stage = 'reducer'
          AND lock_target.work_item_id = ordered_candidate.work_item_id
          -- #10863: recheck the stale-generation gate on the locked row,
          -- beside the lease rechecks below, so hand-out refusal does not
          -- rest on the snapshot candidate set alone.
          AND NOT EXISTS (
              SELECT 1
              FROM stale_reducer_candidates AS gate_stale
              WHERE gate_stale.work_item_id = lock_target.work_item_id
          )
          AND lock_target.status IN ('pending', 'retrying', 'claimed', 'running')
          AND (lock_target.claim_until IS NULL OR lock_target.claim_until <= $1)
          -- #6828: stamp-clock visibility, matching the base candidate filter
          -- above; a Go-only recheck here would reintroduce the skew flake by
          -- dropping trigger-reopened rows the base filter admitted.
          AND (lock_target.visible_at IS NULL OR lock_target.visible_at <= $1 OR (lock_target.status = 'pending' AND lock_target.visible_at <= clock_timestamp()))
        FOR UPDATE OF lock_target SKIP LOCKED
    ) AS lock_target ON true
    -- #10568: the fairness keys are spelled unqualified here: ORDER BY binds
    -- simple names to this SELECT's output columns (ordered_candidate.*), so
    -- the sort is identical to the qualified form, and the literal stays the
    -- pinned source-fairness contract. The inner copy above stays
    -- candidate-qualified for clarity beside the reserved anti-join, whose
    -- output carries the same column names.
    ORDER BY reclaim_rank ASC, gate_boost ASC, reducer_domain_priority ASC, reducer_source_inflight_count ASC, reducer_source_fair_rank ASC, reducer_domain_fair_rank ASC, reducer_domain_inflight_count ASC, updated_at ASC, work_item_id ASC
    -- The reserved arm took COUNT(*) rows (0 or 1); the rest arm fills what
    -- remains of the batch. The scalar subquery plans as an InitPlan, not a
    -- per-row SubPlan, and falls back to the full $8 when nothing reserved.
    LIMIT ($8 - (SELECT COUNT(*) FROM locked_search_doc_reserved))
)`
}
