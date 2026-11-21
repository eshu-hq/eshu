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
      -- #10863: stale-generation rows are fenced read-only here; the
      -- periodic sweep terminalizes them out of band, so a fenced row
      -- stays pending (diagnosable, replayable) until it runs.
      AND NOT EXISTS (
          SELECT 1
          FROM stale_reducer_candidates AS superseded
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
	return `-- live_fence_keys: the once-per-claim live-lease conflict-key set
-- (#10630 step 2). Declared here, ahead of its only reader ` + "`candidate`" + `,
-- because the gate precomputes above feed ` + "`base`" + ` while this set feeds
-- the fence below; WITH order only requires it before ` + "`candidate`" + `.
` + reducerClaimLiveFenceKeysCTE() + `,
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
-- The live-lease fence below it reads the once-per-claim live_fence_keys
-- set (#10630 step 2) instead of re-probing fact_work_items per row.
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
    -- #10916: every column this CTE reads (work_item_id, generation_id,
    -- domain, conflict_domain, ckey, updated_at) already rides on reps from
    -- base, snapshot-identical by construction, so candidate reads reps
    -- directly instead of joining back to fact_work_items by primary key.
    -- The lock arms below keep their fact_work_items lock_target probes:
    -- those re-read the live row for the EvalPlanQual recheck, which a
    -- snapshot column cannot replace.
    SELECT
        reps.work_item_id,
        reps.generation_id,
        -- #10865 reclaim_rank: expired claimed/running holds sort first at
        -- the batch LIMIT (projector #10480 twin). Only status is tested
        -- because base already admits solely unleased rows (claim_until IS
        -- NULL or past), so no live lease can reach this CASE — every
        -- claimed/running row here is an expired (or anomalous NULL-lease)
        -- hold awaiting reclaim. Without this lead the hold rewrites its
        -- own updated_at at claim time and sorts after older pending rows
        -- at every updated_at tie-break, starving behind any backlog
        -- deeper than the batch.
        CASE WHEN reps.status IN ('claimed', 'running') THEN 0 ELSE 1 END AS reclaim_rank,
        CASE WHEN reps.domain = 'eshu_search_document' THEN 1 ELSE 0 END AS reducer_domain_priority,
        COALESCE(source_counts.reducer_source_inflight_count, 0) AS reducer_source_inflight_count,
        CASE WHEN reps.work_item_id = source_heads.work_item_id THEN 0 ELSE 1 END AS reducer_source_fair_rank,
        reps.reducer_domain_fair_rank AS reducer_domain_fair_rank,
        COALESCE(domain_counts.reducer_domain_inflight_count, 0) AS reducer_domain_inflight_count,
        reps.updated_at AS updated_at
    FROM reps
    LEFT JOIN reducer_source_inflight AS source_counts
      ON source_counts.reducer_source_system = reps.reducer_source_system
    LEFT JOIN reducer_domain_inflight AS domain_counts
      ON domain_counts.reducer_domain = reps.domain
    LEFT JOIN source_heads
      ON source_heads.work_item_id = reps.work_item_id
    WHERE reps.same_rn = 1
      AND reps.projector_ok
      AND reps.semantic_ok
      -- #10630 step 2: the #4137 live-lease fence reads the once-per-claim
      -- live_fence_keys set instead of re-probing fact_work_items once per
      -- representative. The key comparison reads reps.conflict_domain and
      -- reps.ckey (#10916), which base computed as the shipped
      -- (conflict_domain, COALESCE(conflict_key, scope_id)) pair verbatim;
      -- the shipped work_item_id self-exclusion is vacuous here because
      -- base pre-filters to unleased rows (see
      -- reducerClaimLiveFenceKeysCTE).
      AND NOT EXISTS (
          SELECT 1
          FROM live_fence_keys
          WHERE live_fence_keys.conflict_domain = reps.conflict_domain
            AND live_fence_keys.ckey = reps.ckey
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
` + reducerClaimStaleGateCTEs() + `,
` + reducerClaimBatchCandidateChainCTEs() + `,
-- #10972: gate-waited generations. A drain gate holds its lane while its last
-- committed chunk is still draining, so that chunk's generation waits in the
-- queue behind unrelated backlog (measured: one vuln generation pending 8h+
-- while the same machinery drains other sources ~30x faster). This CTE names
-- the generations a gate currently waits on so the rest arm below can spend
-- one batch slot unblocking them instead of deepening the hold. It sits after
-- 'candidate' (not in the shared upstream chain) so the funnel counts, which
-- compose only through 'candidate', and the frozen pre-#10916 chain the
-- rejoin-removal guard compares, stay byte-identical. It is a UNION of
-- per-gate arms; the NVD baseline drain is the first arm. A year qualifies
-- while its checkpoint is incomplete and names a last committed chunk: a
-- drained generation drops out on its own because none of its rows are live
-- candidates anymore (its work items are terminal), and a completed year
-- drops out by the year_complete fence. Claim-time fences (stale, readiness,
-- live-lease) still apply in the lock arms, so a waited-on row that fails
-- any of them is not claimable here either. MATERIALIZED like the other
-- gate sets: read once per claim.
gate_waited_generations AS MATERIALIZED (
    SELECT c.last_generation_id AS waited_generation_id,
           MIN(c.updated_at) AS gate_wait_since
    FROM vulnerability_nvd_baseline_checkpoints AS c
    WHERE NOT c.year_complete
      AND c.last_generation_id <> ''
    GROUP BY c.last_generation_id
),` + reducerClaimBatchReservedArmCTE() + `,
` + reducerClaimBatchRestArmCTE() + `,
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
        -- #11075: the prior verdict's failure evidence rides the claim so a
        -- deferred handler keeps measuring its stale clock from the cycle's
        -- first deferral instead of restarting it on every claim.
        COALESCE(work.failure_class, '') AS failure_class,
        COALESCE(work.failure_message, '') AS failure_message,
        COALESCE(work.failure_details, '') AS failure_details,
        work.payload
)` + ",\n" + reducerQueueLeaseMirrorCTE + `
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
    failure_class,
    failure_message,
    failure_details,
    payload
FROM claimed
`
