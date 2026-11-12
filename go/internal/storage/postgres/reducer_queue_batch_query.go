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
// reducerClaimBatchUpstreamCTEs renders the batch-claim CTEs from
// reducer_source_inflight through base: the inflight tallies, the
// precomputed gate CTEs, and the gated candidate rows. Split from the
// representative CTEs only to respect the repository function-length cap;
// the chain wrapper below rejoins them verbatim.
func reducerClaimBatchUpstreamCTEs() string {
	return `reducer_source_inflight AS MATERIALIZED (
    SELECT
        COALESCE(NULLIF(BTRIM(payload->>'source_system'), ''), 'unknown') AS reducer_source_system,
        count(*) AS reducer_source_inflight_count
    FROM fact_work_items
    WHERE stage = 'reducer'
      AND status IN ('claimed', 'running')
      AND claim_until > $1
    GROUP BY reducer_source_system
),
-- #8024: per-domain inflight tally feeding the idle-domain ordering key in
-- the locked CTE. Same scan shape as reducer_source_inflight above (claimed/running
-- with a live lease), grouped by domain instead of source_system, so a ready
-- row of a domain with no inflight work sorts ahead of an older same-source
-- backlog that is already being worked.
reducer_domain_inflight AS MATERIALIZED (
    SELECT
        domain AS reducer_domain,
        count(*) AS reducer_domain_inflight_count
    FROM fact_work_items
    WHERE stage = 'reducer'
      AND status IN ('claimed', 'running')
      AND claim_until > $1
    GROUP BY domain
),
` + reducerClaimBatchGatePrecomputeCTEs() + `,
-- base: every shared gate (readiness, projector drain, semantic caps) read
-- through JOINs from the precomputed gate CTEs above (#10565 step 1), which
-- evaluate each gate once per claim. The per-row correlated SubPlans they
-- replace used to re-run once per candidate row.
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
        (readiness_missing.work_item_id IS NULL) AS readiness_ok,
        ($5 = false OR projector_live_scopes.scope_id IS NULL) AS projector_ok,
        CASE WHEN fact_work_items.domain <> 'semantic_entity_materialization' THEN true
        ELSE (
            ($5 = false OR NOT semantic_scalars.proj_any_live)
            AND ($5 = false OR $6 <= 0 OR semantic_scalars.proj_done >= $6)
            AND ($5 = false OR $7 <= 0 OR semantic_scalars.sem_inflight < $7)
            AND ($5 = false OR $7 <= 0 OR COALESCE(semantic_ranked.sem_rank, 0) <= $7 - semantic_scalars.sem_inflight)
        ) END AS semantic_ok
    FROM fact_work_items
    CROSS JOIN semantic_scalars
    LEFT JOIN projector_live_scopes
      ON projector_live_scopes.scope_id = fact_work_items.scope_id
    LEFT JOIN readiness_missing
      ON readiness_missing.work_item_id = fact_work_items.work_item_id
    LEFT JOIN semantic_ranked
      ON semantic_ranked.work_item_id = fact_work_items.work_item_id
    WHERE stage = 'reducer'
      AND status IN ('pending', 'retrying', 'claimed', 'running')
      AND NOT EXISTS (
          SELECT 1
          FROM superseded_stale_reducer_generations AS superseded
          WHERE superseded.work_item_id = fact_work_items.work_item_id
      )
      -- #6828: visibility runs on the clock that stamped the row (same fix
      -- as the single-claim query): the $1 arm covers app-stamped retry
      -- rows and simulated clocks, the pending arm covers trigger-stamped
      -- reopened rows on the database clock.
      AND (visible_at IS NULL OR visible_at <= $1 OR (status = 'pending' AND visible_at <= clock_timestamp()))
      AND (claim_until IS NULL OR claim_until <= $1)
      AND ($2::text[] IS NULL OR domain = ANY($2::text[]))
)`
}

// reducerClaimBatchRepresentativeCTEs renders the batch-claim CTEs from
// reps_ranked through candidate: the representative and fairness windows and
// the fenced representative set. Split from the upstream CTEs only to respect
// the repository function-length cap; the chain wrapper below rejoins them
// verbatim.
func reducerClaimBatchRepresentativeCTEs() string {
	return `-- reps_ranked / reps: representative and fairness windows computed once over
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
        COALESCE(domain_counts.reducer_domain_inflight_count, 0) AS reducer_domain_inflight_count,
        fact_work_items.updated_at AS updated_at
    FROM reps
    JOIN fact_work_items ON fact_work_items.work_item_id = reps.work_item_id
    LEFT JOIN reducer_source_inflight AS source_counts
      ON source_counts.reducer_source_system = reps.reducer_source_system
    LEFT JOIN reducer_domain_inflight AS domain_counts
      ON domain_counts.reducer_domain = reps.domain
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
)`
}

// reducerClaimBatchCandidateChainCTEs renders every batch-claim CTE from
// reducer_source_inflight through candidate as one verbatim chain. The claim
// (claimReducerWorkBatchQuery) and the funnel counts query
// (reducerClaimBatchFunnelQuery) compose it from this single source, so the
// funnel counts the same membership the claim admits: a future gate edit
// lands in both or the verbatim-substring guards fail loudly instead of
// silently diverging funnel attribution from admission.
func reducerClaimBatchCandidateChainCTEs() string {
	return reducerClaimBatchUpstreamCTEs() + ",\n" + reducerClaimBatchRepresentativeCTEs()
}

var claimReducerWorkBatchQuery = `
WITH ` + reducerClaimReadinessRequirementsCTE() + `,
` + supersedeInactiveReducerGenerationsCTE + `,
` + reducerClaimBatchCandidateChainCTEs() + `,
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
    -- This arm locks via the same lock_target join with identical EvalPlanQual
    -- row-self lease/visibility/status recheck predicates as locked_rest, so a
    -- row another worker claimed between our snapshot and this lock is dropped
    -- in either arm. SKIP LOCKED never blocks, so the second locking arm adds
    -- no deadlock edge: workers still never wait on each other.
    -- MATERIALIZED: the rest arm anti-joins this output, so it must be
    -- evaluated once with its locks taken once, not inlined per reference.
    SELECT candidate.*
    FROM fact_work_items AS lock_target
    JOIN candidate ON candidate.work_item_id = lock_target.work_item_id
    WHERE lock_target.stage = 'reducer'
      AND lock_target.status IN ('pending', 'retrying', 'claimed', 'running')
      AND (lock_target.claim_until IS NULL OR lock_target.claim_until <= $1)
      -- #6828: stamp-clock visibility, matching the base candidate filter
      -- above; a Go-only recheck here would reintroduce the skew flake by
      -- dropping trigger-reopened rows the base filter admitted.
      AND (lock_target.visible_at IS NULL OR lock_target.visible_at <= $1 OR (lock_target.status = 'pending' AND lock_target.visible_at <= clock_timestamp()))
      AND candidate.reducer_domain_priority = 1
    ORDER BY candidate.updated_at ASC, candidate.work_item_id ASC
    LIMIT 1
    FOR UPDATE OF lock_target SKIP LOCKED
),
locked_rest AS (
    SELECT candidate.*
    FROM fact_work_items AS lock_target
    JOIN candidate ON candidate.work_item_id = lock_target.work_item_id
    -- Anti-join the reserved row so it is never claimed twice in one batch.
    -- A LEFT JOIN ... IS NULL keeps the lock/claim tail free of per-row
    -- SubPlan gates (NOT EXISTS would add one); the join is on this
    -- statement's own reserved output, so no concurrent-write hazard.
    LEFT JOIN locked_search_doc_reserved AS search_doc_taken
      ON search_doc_taken.work_item_id = candidate.work_item_id
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
      -- #6828: stamp-clock visibility, matching the base candidate filter
      -- above; a Go-only recheck here would reintroduce the skew flake by
      -- dropping trigger-reopened rows the base filter admitted.
      AND (lock_target.visible_at IS NULL OR lock_target.visible_at <= $1 OR (lock_target.status = 'pending' AND lock_target.visible_at <= clock_timestamp()))
      AND search_doc_taken.work_item_id IS NULL
    -- #8024: reducer_domain_inflight_count ASC sits after the fairness ranks
    -- and before the updated_at tie-break, so a ready row of a domain with no
    -- inflight work takes a slot ahead of an older same-source backlog without
    -- disturbing the #3385 cross-domain interleave (which ties at 0 = 0 when
    -- neither domain holds leases).
    ORDER BY reducer_domain_priority ASC, reducer_source_inflight_count ASC, reducer_source_fair_rank ASC, reducer_domain_fair_rank ASC, reducer_domain_inflight_count ASC, updated_at ASC, work_item_id ASC
    -- The reserved arm took COUNT(*) rows (0 or 1); the rest arm fills what
    -- remains of the batch. The scalar subquery plans as an InitPlan, not a
    -- per-row SubPlan, and falls back to the full $8 when nothing reserved.
    LIMIT ($8 - (SELECT COUNT(*) FROM locked_search_doc_reserved))
    FOR UPDATE OF lock_target SKIP LOCKED
),
locked AS (
    SELECT * FROM locked_search_doc_reserved
    UNION ALL
    SELECT * FROM locked_rest
),
claimed AS (
    UPDATE fact_work_items AS work
    SET status = 'claimed',
        attempt_count = ` + reducerClaimAttemptCountCaseSQL() + `,
        container_image_identity_claim_epoch =
            CASE
                WHEN work.domain = 'container_image_identity'
                    THEN work.container_image_identity_claim_epoch + 1
                ELSE work.container_image_identity_claim_epoch
            END,
        container_image_identity_v2_required =
            CASE
                WHEN work.domain = 'container_image_identity'
                    THEN TRUE
                ELSE work.container_image_identity_v2_required
            END,
        container_image_identity_v2_authorized_status =
            CASE
                WHEN work.domain = 'container_image_identity'
                    THEN 'claimed'
                ELSE work.container_image_identity_v2_authorized_status
            END,
        container_image_identity_v3_required =
            CASE
                WHEN work.domain = 'container_image_identity'
                    THEN TRUE
                ELSE work.container_image_identity_v3_required
            END,
        container_image_identity_v3_authorized_status =
            CASE
                WHEN work.domain = 'container_image_identity'
                    THEN 'claimed'
                ELSE work.container_image_identity_v3_authorized_status
            END,
        lease_owner = $3,
        claim_until = $4,
        last_attempt_at = CASE
            WHEN work.last_attempt_at IS NULL OR work.last_attempt_at < $1 THEN $1
            ELSE work.last_attempt_at + interval '1 microsecond'
        END,
        updated_at = $1
    FROM locked
    WHERE work.work_item_id = locked.work_item_id
    RETURNING
        work.work_item_id,
        work.scope_id,
        work.generation_id,
        work.domain,
        work.attempt_count,
        work.container_image_identity_claim_epoch,
        work.created_at,
        COALESCE(work.visible_at, work.created_at) AS available_at,
        COALESCE(work.reopened_at, work.created_at) AS cycle_started_at,
        work.last_attempt_at AS claimed_at,
        work.payload
)
SELECT
    work_item_id,
    scope_id,
    generation_id,
    domain,
    attempt_count,
    container_image_identity_claim_epoch,
    created_at,
    available_at,
    cycle_started_at,
    claimed_at,
    payload
FROM claimed
`
