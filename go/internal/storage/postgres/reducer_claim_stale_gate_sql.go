// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// reducerClaimStaleGateCTEs renders the read-only stale-generation fence
// both reducer claim statements (and the batch funnel census) compose
// since #10863: stale_reducer_candidates lists the pending/retrying
// reducer rows the periodic sweep would terminalize, so
// `base` (batch) and `candidate` (single) can refuse them at hand-out
// without the claim locking or updating anything.
//
// Membership mirrors supersedeInactiveReducerGenerationsPredicate over
// the statuses a claim can hand out, reusing its fragments rather than
// restating them:
//
//   - generation guard: either the row's generation is not the scope's
//     active generation and is older in activation order (ingested_at, then
//     generation_id) with readiness met under own or active generation, OR
//     the scope is retired (active_generation_id IS NULL) and the row's own
//     generation is terminally 'superseded' (#11703; never 'failed', so
//     recover-failed-scopes still inspects failed scopes), read from the
//     row's own active-generation lookup (#11234).
//   - readiness hold (#4445/#7664): the same own-generation OR
//     active-generation readiness gate text the sweep applies, so a row
//     whose phases exist under neither generation is not fenced here
//     either (the claim's own readiness gate still keeps it unclaimable,
//     and the sweep spares it until the active generation carries the
//     intent forward).
//   - fan-out-debt spare (#10060): supplyChainFanOutDebtSpareSQL verbatim.
//   - Crossplane carrier (#10106/#10269): a stale Crossplane row is
//     fenced only when a newer sibling for the same scope is pending,
//     retrying, or in flight (claimed/running). The sweep proves carrier
//     membership through its locked set because superseded is
//     unreplayable; the gate is read-only and re-evaluated on every
//     claim, so a snapshot EXISTS is safe here — if an in-flight carrier
//     dead-letters, the fence lifts on the next claim and the stale row
//     executes as the rebind fallback, where the same race against the
//     terminal UPDATE would have destroyed it (#10325). A succeeded
//     carrier does not fence: its execution may predate a later quiet
//     activation only the stale row can rebind to.
//
// Statuses are pending/retrying only: failed and dead_letter rows are
// never claim candidates, and expired claimed/running rows stay
// reclaimable exactly as under the in-claim sweep (whose predicate never
// selected them). Terminalization of every status stays with the
// standalone periodic sweep (supersedeStaleReducerGenerationsQuery).
//
// Join order (#11234). The gate is driven from the pending/retrying work
// rows, never from the scope set. stale_gate_rows lists the rows not on
// their scope's active generation id, by one join of the work rows to
// ingestion_scopes, before any probe of scope_generations: with no stale
// row (the normal state) it is empty and nothing below it runs per row.
// Each such row then looks up its scope's active generation through a
// CROSS JOIN LATERAL ... OFFSET 0 of two primary-key probes; it yields no
// row when the scope has no valid active generation row, as the old inner
// join to a per-scope set did. OFFSET 0 keeps the planner from pulling the
// subquery up. The hermetic body guard pins its presence, but no live
// proof shows it is needed: without it the pin stays green with fresh
// statistics, and the no-statistics 2,000-stale cells read the same
// buffers in the same time. The proven plan fence is MATERIALIZED
// (PostgreSQL 18 docs, section 7.8.3). No CTE is joined to another CTE,
// so no plan can rescan one per row (without planner statistics the old
// per-scope set was rescanned once per stale row). Both CTEs stay MATERIALIZED: stale_gate_rows so the
// prefilter runs once ahead of every lookup, and stale_reducer_candidates
// so base's NOT EXISTS and the lock arms read one computed set instead of
// inlining the gate.
//
// The shipped #10863 text built claim_active_generations for every scope
// and joined it to the candidate rows. Postgres estimates that
// (scope_id, generation_id) join between ingestion_scopes and
// scope_generations at 1 row (#6794 records the same misestimate), so it
// drove a nested loop from that "1 row" over all 12,236 active scopes and
// probed scope_generations and fact_work_items per scope to find nothing
// (QA replica and primary plans, 2026-11-22: 210,728 of the cut's 251,521
// buffers and 8.6 of 9.8 s, 0 rows). The prefilter is implied by the
// verbatim generation guard below, so the fenced set is unchanged: the
// body of stale_reducer_candidates is the #10863 text with only its FROM
// line and its scope join replaced, and the lookup carries the #10863 ON
// text verbatim. (#10863's gate shim measured 29,684 buffers on the
// 8,713-row replay with 4,000 scopes, before QA's scope count made the
// misestimate drive the plan.) Evidence:
// #11234 (retired evidence record).
//
// The CTEs must be declared AFTER reducerClaimReadinessRequirementsCTE in
// the enclosing WITH list, like the sweep they replace: the readiness
// fragments reference reducer_claim_readiness_requirements. Only
// stale_reducer_candidates is read outside this text; stale_gate_rows must
// carry every column the body reads through the stale alias.
func reducerClaimStaleGateCTEs() string {
	return `stale_gate_rows AS MATERIALIZED (
    SELECT work.work_item_id,
           work.scope_id,
           work.generation_id,
           work.stage,
           work.domain,
           work.status,
           work.payload
    FROM fact_work_items AS work
    JOIN ingestion_scopes AS work_scope ON work_scope.scope_id = work.scope_id
    WHERE work.stage = 'reducer'
      AND work.status IN ('pending', 'retrying')
      AND ($2::text[] IS NULL OR work.domain = ANY($2::text[]))
      AND work.generation_id IS DISTINCT FROM work_scope.active_generation_id
),
stale_reducer_candidates AS MATERIALIZED (
    SELECT stale.work_item_id
    FROM stale_gate_rows AS stale
    CROSS JOIN LATERAL (
        SELECT scope.scope_id,
               scope.active_generation_id,
               active_generation.generation_id AS present_active_generation_id,
               active_generation.ingested_at AS active_ingested_at
        FROM ingestion_scopes AS scope
        LEFT JOIN scope_generations AS active_generation
          ON active_generation.generation_id = scope.active_generation_id
         AND active_generation.scope_id = scope.scope_id
        WHERE scope.scope_id = stale.scope_id
        OFFSET 0
    ) AS scope
    JOIN scope_generations AS stale_generation
      ON stale_generation.scope_id = stale.scope_id
     AND stale_generation.generation_id = stale.generation_id
    WHERE stale.stage = 'reducer'
      AND stale.status IN ('pending', 'retrying')
      AND ($2::text[] IS NULL OR stale.domain = ANY($2::text[]))
      AND (
        (
          scope.present_active_generation_id IS NOT NULL
          AND stale.generation_id <> scope.active_generation_id
          AND (
            stale_generation.ingested_at < scope.active_ingested_at
            OR (
              stale_generation.ingested_at = scope.active_ingested_at
              AND stale_generation.generation_id < scope.active_generation_id
            )
          )
          AND (
            ` + reducerClaimReadinessGateSQL("stale", "supersede_readiness_req", "supersede_readiness_phase") + `
            OR ` + reducerClaimReadinessGateForGenerationSQL("stale", "scope.active_generation_id", "supersede_active_readiness_req", "supersede_active_readiness_phase") + `
          )
        )
        OR (
          scope.active_generation_id IS NULL
          AND stale_generation.status = 'superseded'
        )
      )
      AND ` + supplyChainFanOutDebtSpareSQL + `
      AND (stale.domain <> 'crossplane_satisfied_by_materialization'
           OR EXISTS (
             SELECT 1
             FROM fact_work_items AS carrier
             JOIN scope_generations AS carrier_generation
               ON carrier_generation.scope_id = carrier.scope_id
              AND carrier_generation.generation_id = carrier.generation_id
             WHERE carrier.stage = 'reducer'
               AND carrier.domain = 'crossplane_satisfied_by_materialization'
               AND carrier.scope_id = stale.scope_id
               AND carrier.work_item_id <> stale.work_item_id
               AND carrier.status IN ('pending', 'retrying', 'claimed', 'running')
               AND (carrier_generation.ingested_at > stale_generation.ingested_at
                    OR (carrier_generation.ingested_at = stale_generation.ingested_at
                        AND carrier_generation.generation_id > stale_generation.generation_id))))
)`
}
