// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// reducerConflictBlockageCTEs finds reducer rows that are claimable but fenced
// by a live row on the same conflict key or by an unmet readiness requirement.
// It needs active_fact_work_items earlier in the same WITH list.
var reducerConflictBlockageCTEs = reducerClaimReadinessRequirementsCTE() + `,
eligible AS (
    SELECT work_item_id,
           scope_id,
           generation_id,
           domain,
           conflict_domain,
           COALESCE(conflict_key, scope_id) AS conflict_key,
           COALESCE(visible_at, created_at) AS available_at,
           payload
    FROM active_fact_work_items
    WHERE stage = 'reducer'
      AND status IN ('pending', 'retrying', 'claimed', 'running')
      AND (visible_at IS NULL OR visible_at <= $1)
      AND (claim_until IS NULL OR claim_until <= $1)
),
` + reducerConflictInflightLeasesCTE + reducerConflictBlockedCTE + `readiness_blocked AS (
    -- Surface each missing readiness requirement as its own bounded blockage row.
    -- Multi-key domains such as security-group reachability and EC2 profile
    -- edges appear once per missing keyspace/entity-key requirement.
    SELECT eligible.work_item_id,
           eligible.domain,
           'readiness' AS conflict_domain,
           readiness_req.keyspace || ':' || readiness_req.phase || ':' ||
               ` + reducerClaimReadinessAcceptanceUnitSQL("eligible", "readiness_req") + ` AS conflict_key,
           eligible.available_at
    FROM eligible
    JOIN reducer_claim_readiness_requirements AS readiness_req
      ON readiness_req.domain = eligible.domain
      AND NOT EXISTS (
          SELECT 1
          FROM graph_projection_phase_state AS readiness_phase
          WHERE readiness_phase.scope_id = eligible.scope_id
            AND readiness_phase.acceptance_unit_id = ` + reducerClaimReadinessAcceptanceUnitSQL("eligible", "readiness_req") + `
            AND readiness_phase.source_run_id = eligible.generation_id
            AND readiness_phase.generation_id = eligible.generation_id
            AND readiness_phase.keyspace = readiness_req.keyspace
            AND readiness_phase.phase = readiness_req.phase
      )
),
all_blocked AS (
    SELECT work_item_id, domain, conflict_domain, conflict_key, available_at FROM blocked
    UNION ALL
    SELECT work_item_id, domain, conflict_domain, conflict_key, available_at FROM readiness_blocked
),
blockage_rows AS (
    SELECT domain,
           conflict_domain,
           conflict_key,
           COUNT(DISTINCT work_item_id) AS blocked_count,
           COALESCE(EXTRACT(EPOCH FROM ($1 - MIN(available_at))), 0) AS oldest_blocked_age_seconds
    FROM all_blocked
    GROUP BY domain, conflict_domain, conflict_key
),
domain_blocked AS (
    SELECT domain,
           COUNT(DISTINCT work_item_id) AS domain_blocked_count
    FROM all_blocked
    GROUP BY domain
)`

// reducerConflictBlockageSelect reports blocked rows per domain and conflict key.
const reducerConflictBlockageSelect = `SELECT 'reducer' AS stage,
       domain,
       conflict_domain,
       conflict_key,
       domain_blocked_count AS blocked_count,
       oldest_blocked_age_seconds
FROM blockage_rows
JOIN domain_blocked USING (domain)`

// reducerConflictBlockageOrder is reducerConflictBlockageSelect's result order;
// the status surface keeps the first reducerConflictBlockageLimit rows.
const reducerConflictBlockageOrder = `blocked_count DESC, oldest_blocked_age_seconds DESC, domain ASC, conflict_key ASC`

// reducerConflictBlockageLimit bounds the blockage rows on the status surface.
const reducerConflictBlockageLimit = 10

// reducerConflictInflightLeasesCTE selects the live reducer leases (#6794):
// claimed or running reducer rows whose claim has not expired, keyed like
// fact_work_items_reducer_live_lease_uniq. That index allows at most one such
// row per conflict key, so the set holds the live claimed-or-running reducer
// rows: the running workers plus rows claimed but not yet started. It uses the
// same predicate as the reducer claim query's reducer_source_inflight, which
// counts the same rows per source system. AS MATERIALIZED is kept so the plan
// names the lease scan (CTE Scan on inflight_leases), which the blockage plan
// regression asserts on; the CTE is referenced once and would otherwise be
// inlined.
const reducerConflictInflightLeasesCTE = `inflight_leases AS MATERIALIZED (
    SELECT work_item_id,
           conflict_domain,
           COALESCE(conflict_key, scope_id) AS conflict_key
    FROM fact_work_items
    WHERE stage = 'reducer'
      AND status IN ('claimed', 'running')
      AND claim_until > $1
),
`

// reducerConflictBlockedCTE fences each eligible reducer row whose conflict key
// holds a live lease (#6794). It is a filter on eligible, not a join, so the
// planner has no join method to choose. The eligible CTE's row estimate is far
// below its real size, so with missing or stale statistics the planner ran the
// old join as a nested loop, rescanning one side once per row of the other.
//
// The uncorrelated IN (SELECT ...) is an ANY sublink, and PostgreSQL runs an
// uncorrelated ANY sublink as a hashed SubPlan (the lease set is hashed once,
// then probed once per eligible row) whenever its estimated size fits in
// work_mem * hash_mem_multiplier (subplan_is_hashable in
// optimizer/plan/subselect.c). At the default work_mem that needs an estimate
// above about 95,000 live leases, far past what one lease per conflict key
// among running reducer workers can reach. The COALESCE(..., FALSE) is
// load-bearing for the plan, not the result: the keys are NOT NULL, so IN never
// yields NULL, but an IN at the top level of WHERE is pulled up into a
// semi-join the planner may nested-loop again; inside COALESCE it stays a
// SubPlan. TestActiveWorkSummaryBlockageHashesLeasesOnce fails if the plan stops
// saying "hashed SubPlan" or if any eligible or lease scan runs more than once.
//
// The filter returns exactly the rows the old join did. An eligible row has
// claim_until NULL or not after $1 and a live lease has it after $1, so no row
// is on both sides and the join's "lease is another row" condition never
// excluded a match. IN emits an eligible row once whether one or several
// leases share its key.
const reducerConflictBlockedCTE = `blocked AS (
    SELECT work_item_id,
           domain,
           conflict_domain,
           conflict_key,
           available_at
    FROM eligible
    WHERE COALESCE(
        (conflict_domain, conflict_key) IN (SELECT conflict_domain, conflict_key FROM inflight_leases),
        FALSE)
),
`
