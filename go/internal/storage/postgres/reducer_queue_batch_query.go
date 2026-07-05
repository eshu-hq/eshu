// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// claimReducerWorkBatchQuery selects and claims up to $8 reducer work items in
// one round trip.
//
// # Rank-once window rewrite (#3624 Track 2)
//
// Earlier revisions computed reducer_domain_fair_rank and the same-conflict-key
// representative with correlated scalar subqueries re-run once PER CANDIDATE
// ROW: each invocation re-scanned fact_work_items, re-applied the readiness
// gate, and (for reducer_domain_fair_rank) re-walked every same-domain peer.
// On a live ~9.6k-item reducer backlog that cost 24s/claim and ~20.6M shared
// Postgres buffers per call (#3624 diagnostic run), because the per-row work is
// O(D) and the whole candidate set is O(D) rows, making the combined cost
// O(Sigma_d D_d^2).
//
// The fix computes every shared gate (readiness, projector drain, semantic
// caps) exactly ONCE per row in `base`, then ranks representatives and the
// domain fair-rank with window functions evaluated once over the whole
// candidate set instead of once per row:
//
//   - base: one pass over fact_work_items with readiness_ok / projector_ok /
//     semantic_ok computed once each.
//   - reps_ranked: row_number() OVER (PARTITION BY conflict_domain, ckey ...)
//     for both the #4137 claimed/running-first representative order
//     (w_same/same_rn) and the fairness peer order (w_fairsame/fair_same_rn).
//     Window functions are legal here because this is an inner CTE, not the
//     FOR UPDATE SKIP LOCKED level itself — the "window functions forbidden
//     with FOR UPDATE SKIP LOCKED" Postgres restriction only applies AT THAT
//     SAME QUERY LEVEL. The `locked` CTE below applies FOR UPDATE OF at an
//     outer level that joins back to `candidate` by primary key, so the
//     restriction is satisfied without giving up window functions inside.
//   - reps: reducer_domain_fair_rank folded in as a conditional running-sum
//     window (sum(peer_flag) OVER (PARTITION BY domain ORDER BY updated_at,
//     work_item_id ROWS UNBOUNDED PRECEDING) - peer_flag), replacing the
//     correlated "count(*) ... fair_peer" subquery. Because (updated_at,
//     work_item_id) is unique (work_item_id is the primary key), the running
//     sum minus this row's own flag equals the count of strictly-earlier
//     fair_peer members for every row, member and non-member alike.
//   - representative selection: the #4137 conflict-key representative is the
//     reps.same_rn = 1 row (claimed/running holder first, then is_search_doc,
//     updated_at, work_item_id), ranked once per key by the w_same window
//     instead of re-derived by a correlated ORDER BY ... LIMIT 1 subquery for
//     every fair_peer candidate. The candidate CTE below fences directly on
//     reps.same_rn = 1; there is no separate `same` CTE and no correlated
//     same-representative subquery — that per-candidate re-scan was the #3624
//     O(N^2) source and has been removed.
//
// A live pure-SQL shim of this shape measured 56ms vs 23,598ms (421x) on the
// same ~9.6k-item backlog, with a bidirectional EXCEPT diff against the
// pre-rewrite candidate SELECT returning 0/0 rows on both the full backlog and
// a seeded expired-lease non-member fixture. The in-repo equivalent of that
// shim is TestClaimBatchRankOnceRewriteMatchesPreRewriteCandidateSetAndOrder
// (reducer_queue_batch_rank_once_diff_test.go).
// Performance Evidence and Observability Evidence for this change are
// recorded in README.md under "Reducer claim rank-once window rewrite
// (#3624)".
var claimReducerWorkBatchQuery = `
WITH ` + reducerClaimReadinessRequirementsCTE() + `,
` + supersedeInactiveReducerGenerationsCTE + `,
reducer_source_inflight AS MATERIALIZED (
    SELECT
        COALESCE(NULLIF(BTRIM(payload->>'source_system'), ''), 'unknown') AS reducer_source_system,
        count(*) AS reducer_source_inflight_count
    FROM fact_work_items
    WHERE stage = 'reducer'
      AND status IN ('claimed', 'running')
      AND claim_until > $1
    GROUP BY reducer_source_system
),
-- base: every shared gate (readiness, projector drain, semantic caps)
-- evaluated exactly once per candidate row. Every predicate mirrors the
-- outer WHERE clauses these gates used to duplicate at each correlated call
-- site (fair_peer, fair_same, source_head).
base AS MATERIALIZED (
    SELECT
        fact_work_items.work_item_id,
        fact_work_items.domain,
        fact_work_items.scope_id,
        fact_work_items.generation_id,
        fact_work_items.conflict_domain,
        COALESCE(fact_work_items.conflict_key, fact_work_items.scope_id) AS ckey,
        fact_work_items.updated_at,
        fact_work_items.status,
        COALESCE(NULLIF(BTRIM(fact_work_items.payload->>'source_system'), ''), 'unknown') AS reducer_source_system,
        CASE WHEN fact_work_items.domain = 'eshu_search_document' THEN 1 ELSE 0 END AS is_search_doc,
        CASE WHEN fact_work_items.status IN ('claimed', 'running') THEN 0 ELSE 1 END AS claimed_running_first,
        ` + reducerClaimReadinessGateSQL("fact_work_items", "readiness_req", "readiness_phase") + ` AS readiness_ok,
        ($5 = false OR NOT EXISTS (
            SELECT 1
            FROM fact_work_items AS projector_work
            WHERE projector_work.stage = 'projector'
              AND projector_work.scope_id = fact_work_items.scope_id
              AND projector_work.status IN ('pending', 'retrying', 'claimed', 'running')
        )) AS projector_ok,
        (
            ($5 = false OR fact_work_items.domain <> 'semantic_entity_materialization' OR NOT EXISTS (
                SELECT 1
                FROM fact_work_items AS projector_any
                WHERE projector_any.stage = 'projector'
                  AND projector_any.domain = 'source_local'
                  AND projector_any.status IN ('pending', 'retrying', 'claimed', 'running')
            ))
            AND ($5 = false OR fact_work_items.domain <> 'semantic_entity_materialization' OR $6 <= 0 OR (
                SELECT count(*)
                FROM fact_work_items AS projector_done
                WHERE projector_done.stage = 'projector'
                  AND projector_done.domain = 'source_local'
                  AND projector_done.status = 'succeeded'
            ) >= $6)
            AND ($5 = false OR fact_work_items.domain <> 'semantic_entity_materialization' OR $7 <= 0 OR (
                SELECT count(*)
                FROM fact_work_items AS semantic_inflight
                WHERE semantic_inflight.stage = 'reducer'
                  AND semantic_inflight.domain = 'semantic_entity_materialization'
                  AND semantic_inflight.work_item_id <> fact_work_items.work_item_id
                  AND semantic_inflight.status IN ('claimed', 'running')
                  AND semantic_inflight.claim_until > $1
            ) < $7)
            AND ($5 = false OR fact_work_items.domain <> 'semantic_entity_materialization' OR $7 <= 0 OR (
                SELECT count(*)
                FROM fact_work_items AS semantic_next
                WHERE semantic_next.stage = 'reducer'
                  AND semantic_next.domain = 'semantic_entity_materialization'
                  AND semantic_next.status IN ('pending', 'retrying', 'claimed', 'running')
                  AND (semantic_next.visible_at IS NULL OR semantic_next.visible_at <= $1)
                  AND (semantic_next.claim_until IS NULL OR semantic_next.claim_until <= $1)
                  AND (
                      semantic_next.updated_at < fact_work_items.updated_at
                      OR (
                          semantic_next.updated_at = fact_work_items.updated_at
                          AND semantic_next.work_item_id <= fact_work_items.work_item_id
                      )
                  )
            ) <= $7 - (
                SELECT count(*)
                FROM fact_work_items AS semantic_inflight2
                WHERE semantic_inflight2.stage = 'reducer'
                  AND semantic_inflight2.domain = 'semantic_entity_materialization'
                  AND semantic_inflight2.status IN ('claimed', 'running')
                  AND semantic_inflight2.claim_until > $1
            ))
        ) AS semantic_ok
    FROM fact_work_items
    WHERE stage = 'reducer'
      AND status IN ('pending', 'retrying', 'claimed', 'running')
      AND NOT EXISTS (
          SELECT 1
          FROM superseded_stale_reducer_generations AS superseded
          WHERE superseded.work_item_id = fact_work_items.work_item_id
      )
      AND (visible_at IS NULL OR visible_at <= $1)
      AND (claim_until IS NULL OR claim_until <= $1)
      AND ($2::text[] IS NULL OR domain = ANY($2::text[]))
),
-- reps_ranked / reps: representative and fairness windows computed once over
-- the whole readiness-gated candidate set, replacing the correlated
-- "same"/"fair_same"/"fair_peer" subqueries that used to re-derive this per
-- row. Two CTE levels are required: Postgres forbids nesting a window
-- function call (row_number()) inside another window aggregate in the same
-- SELECT list, so fair_same_rn must be materialized in reps_ranked before
-- reps can build the conditional running-sum peer_flag/fair-rank on top of
-- it.
reps_ranked AS MATERIALIZED (
    SELECT
        base.*,
        row_number() OVER w_same     AS same_rn,
        row_number() OVER w_fairsame AS fair_same_rn
    FROM base
    WHERE readiness_ok
    WINDOW
        w_same     AS (PARTITION BY conflict_domain, ckey
                       ORDER BY claimed_running_first ASC, is_search_doc ASC, updated_at ASC, work_item_id ASC),
        w_fairsame AS (PARTITION BY conflict_domain, ckey
                       ORDER BY is_search_doc ASC, updated_at ASC, work_item_id ASC)
),
reps AS MATERIALIZED (
    SELECT
        reps_ranked.*,
        CASE WHEN fair_same_rn = 1 AND projector_ok AND semantic_ok THEN 1 ELSE 0 END AS peer_flag,
        (
            sum(CASE WHEN fair_same_rn = 1 AND projector_ok AND semantic_ok THEN 1 ELSE 0 END) OVER (
                PARTITION BY domain
                ORDER BY updated_at ASC, work_item_id ASC
                ROWS UNBOUNDED PRECEDING
            )
            - CASE WHEN fair_same_rn = 1 AND projector_ok AND semantic_ok THEN 1 ELSE 0 END
        ) AS reducer_domain_fair_rank
    FROM reps_ranked
),
-- representative fence: the #4137 conflict-key representative is the
-- reps.same_rn = 1 row (one per (conflict_domain, ckey), ranked once by the
-- w_same window). The candidate CTE below expresses the "this row IS its
-- conflict key's representative" fence directly as reps.same_rn = 1 — no
-- separate same-representative CTE and no correlated ORDER BY ... LIMIT 1
-- subquery, which was the #3624 O(N^2) per-candidate re-scan and is removed.
-- source_heads: unchanged in shape from the pre-rewrite query (cheap; one
-- row_number() pass over reps), kept as its own CTE so the planner does not
-- re-inline it into candidate.
source_heads AS MATERIALIZED (
    SELECT work_item_id
    FROM (
        SELECT
            work_item_id,
            row_number() OVER (
                PARTITION BY domain, reducer_source_system
                ORDER BY is_search_doc ASC, updated_at ASC, work_item_id ASC
            ) AS src_rn
        FROM reps
    ) sh
    WHERE sh.src_rn = 1
),
candidate AS (
    SELECT
        fact_work_items.work_item_id,
        CASE WHEN fact_work_items.domain = 'eshu_search_document' THEN 1 ELSE 0 END AS reducer_domain_priority,
        COALESCE(source_counts.reducer_source_inflight_count, 0) AS reducer_source_inflight_count,
        CASE WHEN fact_work_items.work_item_id = source_heads.work_item_id THEN 0 ELSE 1 END AS reducer_source_fair_rank,
        reps.reducer_domain_fair_rank AS reducer_domain_fair_rank,
        fact_work_items.updated_at AS updated_at
    FROM reps
    JOIN fact_work_items ON fact_work_items.work_item_id = reps.work_item_id
    LEFT JOIN reducer_source_inflight AS source_counts
      ON source_counts.reducer_source_system = reps.reducer_source_system
    LEFT JOIN source_heads
      ON source_heads.work_item_id = reps.work_item_id
    WHERE reps.same_rn = 1
      AND reps.projector_ok
      AND reps.semantic_ok
      AND NOT EXISTS (
          SELECT 1
          FROM fact_work_items AS inflight
          WHERE inflight.stage = 'reducer'
            AND inflight.conflict_domain = fact_work_items.conflict_domain
            AND COALESCE(inflight.conflict_key, inflight.scope_id) = COALESCE(fact_work_items.conflict_key, fact_work_items.scope_id)
            AND inflight.work_item_id <> fact_work_items.work_item_id
            AND inflight.status IN ('claimed', 'running')
            AND inflight.claim_until > $1
      )
),
locked AS (
    SELECT candidate.*
    FROM fact_work_items AS lock_target
    JOIN candidate ON candidate.work_item_id = lock_target.work_item_id
    -- Re-apply the row-self lease/visibility/status predicates on the ACTUAL
    -- locked row. candidate is evaluated from the statement snapshot, but under
    -- Read Committed the FOR UPDATE row lock re-fetches the latest committed
    -- version of lock_target and re-runs THIS CTE's quals against it (EvalPlanQual
    -- recheck). Without these predicates the only qual is the id join to the
    -- snapshot candidate set, so a row another worker claimed and committed
    -- between our snapshot and this lock still matches and its fresh lease would
    -- be overwritten (lease theft / duplicate work). Re-checking status and
    -- claim_until/visible_at here drops such a row exactly as the pre-rewrite
    -- query did when FOR UPDATE SKIP LOCKED sat directly on the predicate-bearing
    -- candidate SELECT. These are a strict subset of base's WHERE, so in the
    -- uncontended case (lock_target == the snapshot row) they are a no-op and the
    -- claimed candidate set is unchanged.
    WHERE lock_target.stage = 'reducer'
      AND lock_target.status IN ('pending', 'retrying', 'claimed', 'running')
      AND (lock_target.claim_until IS NULL OR lock_target.claim_until <= $1)
      AND (lock_target.visible_at IS NULL OR lock_target.visible_at <= $1)
    ORDER BY reducer_domain_priority ASC, reducer_source_inflight_count ASC, reducer_source_fair_rank ASC, reducer_domain_fair_rank ASC, updated_at ASC, work_item_id ASC
    LIMIT $8
    FOR UPDATE OF lock_target SKIP LOCKED
),
claimed AS (
    UPDATE fact_work_items AS work
    SET status = 'claimed',
        attempt_count = ` + reducerClaimAttemptCountCaseSQL() + `,
        lease_owner = $3,
        claim_until = $4,
        last_attempt_at = $1,
        updated_at = $1
    FROM locked
    WHERE work.work_item_id = locked.work_item_id
    RETURNING
        work.work_item_id,
        work.scope_id,
        work.generation_id,
        work.domain,
        work.attempt_count,
        work.created_at,
        COALESCE(work.visible_at, work.created_at) AS available_at,
        work.payload
)
SELECT
    work_item_id,
    scope_id,
    generation_id,
    domain,
    attempt_count,
    created_at,
    available_at,
    payload
FROM claimed
`
