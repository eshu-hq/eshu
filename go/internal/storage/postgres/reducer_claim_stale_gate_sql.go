// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// reducerClaimStaleGateCTEs renders the read-only stale-generation fence
// both reducer claim statements (and the batch funnel census) compose
// since #10863: claim_active_generations resolves every scope's active
// generation once per statement, and stale_reducer_candidates lists the
// pending/retrying reducer rows the periodic sweep would terminalize, so
// `base` (batch) and `candidate` (single) can refuse them at hand-out
// without the claim locking or updating anything.
//
// Membership mirrors supersedeInactiveReducerGenerationsPredicate over
// the statuses a claim can hand out, reusing its fragments rather than
// restating them:
//
//   - generation guard: the row's generation is not the scope's active
//     generation and is older in activation order (ingested_at, then
//     generation_id), read from the once-per-statement
//     claim_active_generations set.
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
// Both CTEs are MATERIALIZED: claim_active_generations is one hash join
// over the scope tables, and stale_reducer_candidates joins it to the
// candidate rows once, instead of re-probing ingestion_scopes and
// scope_generations per candidate row (the per-scope index-probe shape
// that made the in-claim sweep cost ~10k buffers even when it superseded
// nothing). The #10863 gate shim measured this shape at 67 ms / 29,684
// buffers vs the shipped sweep's 83 ms / 85,476 on the 8,713-row replay
// with 4,000 scopes and 35k terminal rows, and 70 ms / 31,447 vs 275 ms /
// 299,980 with 200 stale rows pending, with candidate and hand-out sets
// identical (EXCEPT 0/0 both ways).
//
// The CTEs must be declared AFTER reducerClaimReadinessRequirementsCTE in
// the enclosing WITH list, like the sweep they replace: the readiness
// fragments reference reducer_claim_readiness_requirements.
func reducerClaimStaleGateCTEs() string {
	return `claim_active_generations AS MATERIALIZED (
    SELECT scope.scope_id,
           scope.active_generation_id,
           active_generation.ingested_at AS active_ingested_at
    FROM ingestion_scopes AS scope
    JOIN scope_generations AS active_generation
      ON active_generation.generation_id = scope.active_generation_id
     AND active_generation.scope_id = scope.scope_id
),
stale_reducer_candidates AS MATERIALIZED (
    SELECT stale.work_item_id
    FROM fact_work_items AS stale
    JOIN claim_active_generations AS scope ON scope.scope_id = stale.scope_id
    JOIN scope_generations AS stale_generation
      ON stale_generation.scope_id = stale.scope_id
     AND stale_generation.generation_id = stale.generation_id
    WHERE stale.stage = 'reducer'
      AND stale.status IN ('pending', 'retrying')
      AND ($2::text[] IS NULL OR stale.domain = ANY($2::text[]))
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
