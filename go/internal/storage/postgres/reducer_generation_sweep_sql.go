// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// supersedeInactiveReducerLockedColumnsSQL is the sweep lock step's SELECT
// list: the row identity the UPDATE terminalizes plus the generation columns
// the carrier CTE reads. Shared by the claim's sweep and the funnel's
// read-only sweep so both resolve the same locked membership.
const supersedeInactiveReducerLockedColumnsSQL = `SELECT stale.work_item_id,
           stale.scope_id,
           stale.domain,
           stale.status,
           stale.generation_id,
           stale_generation.ingested_at AS generation_ingested_at`

// supersedeInactiveReducerCarrierArmSQL is the #10269 carrier arm of the sweep
// lock step's WHERE clause: it also admits a pending/retrying Crossplane row
// on the scope's ACTIVE generation when an older stale sibling is queued, so
// the UPDATE can coalesce that sibling into it. Shared by the claim's sweep
// and the funnel's read-only sweep so the carrier set cannot drift between
// them. No leading or trailing newline: each call site supplies its own.
const supersedeInactiveReducerCarrierArmSQL = `       -- #10269 carrier arm: also lock a pending/retrying Crossplane row
       -- on the scope's ACTIVE generation when an older stale sibling is
       -- queued, so the UPDATE can coalesce that sibling into it. Stale-
       -- generation carriers enter through the predicate above already;
       -- only the active generation needs this arm, and the UPDATE's
       -- generation recheck keeps these rows themselves un-superseded.
       -- One scan, one LockRows pass in work_item_id order: the #8205
       -- global lock order is unchanged.
       OR (stale.stage = 'reducer'
           AND stale.domain = 'crossplane_satisfied_by_materialization'
           AND stale.status IN ('pending', 'retrying')
           AND scope.scope_id = stale.scope_id
           AND stale_generation.scope_id = stale.scope_id
           AND stale_generation.generation_id = stale.generation_id
           AND scope.active_generation_id = active_generation.generation_id
           AND active_generation.scope_id = stale.scope_id
           AND stale.generation_id = scope.active_generation_id
           AND EXISTS (
             SELECT 1
             FROM fact_work_items AS older_sibling
             JOIN scope_generations AS older_generation
               ON older_generation.scope_id = older_sibling.scope_id
              AND older_generation.generation_id = older_sibling.generation_id
             WHERE older_sibling.stage = 'reducer'
               AND older_sibling.domain = 'crossplane_satisfied_by_materialization'
               AND ($2::text[] IS NULL OR older_sibling.domain = ANY($2::text[]))
               AND older_sibling.scope_id = stale.scope_id
               AND older_sibling.work_item_id <> stale.work_item_id
               AND older_sibling.status IN ('pending', 'retrying', 'failed', 'dead_letter')
               AND (older_generation.ingested_at < stale_generation.ingested_at
                    OR (older_generation.ingested_at = stale_generation.ingested_at
                        AND older_generation.generation_id < stale_generation.generation_id))))`

// supersededCrossplaneNewestCarrierCTE is the newest locked
// pending/retrying Crossplane carrier per scope, derived once from the locked
// set so the UPDATE probes it per row instead of re-scanning the locked set
// for every Crossplane candidate (measured: a per-row EXISTS over the locked
// CTE cost ~0.13 ms x carrier candidates on a 2,600-row sweep).
// AS MATERIALIZED is load-bearing: the inlined form re-ran the DISTINCT ON
// as a per-row subquery scan under the UPDATE's left join (same probe).
// Shared verbatim by the claim's sweep and the funnel's read-only sweep: both
// statements name their locked set superseded_stale_reducer_locked, so this
// text resolves identically in each.
const supersededCrossplaneNewestCarrierCTE = `-- Newest locked pending/retrying Crossplane carrier per scope, derived
-- once from the locked set so the UPDATE probes it per row instead of
-- re-scanning the locked set for every Crossplane candidate (measured:
-- a per-row EXISTS over the locked CTE cost ~0.13 ms x carrier
-- candidates on a 2,600-row sweep). AS MATERIALIZED is load-bearing:
-- the inlined form re-ran the DISTINCT ON as a per-row subquery scan
-- under the UPDATE's left join (same probe).
superseded_crossplane_newest_carrier AS MATERIALIZED (
    SELECT DISTINCT ON (carrier.scope_id)
           carrier.scope_id,
           carrier.generation_ingested_at,
           carrier.generation_id
    FROM superseded_stale_reducer_locked AS carrier
    WHERE carrier.domain = 'crossplane_satisfied_by_materialization'
      AND carrier.status IN ('pending', 'retrying')
    ORDER BY carrier.scope_id,
             carrier.generation_ingested_at DESC,
             carrier.generation_id DESC
),`

// supersedeInactiveReducerUpdateRowFiltersSQL is the per-row filter block of
// the sweep UPDATE's WHERE clause: the scope join plus the stage/status
// recheck, the older-generation recheck, and the #10269 carrier gate.
// Shared by the claim's sweep UPDATE and the funnel's read-only generations
// SELECT so the terminalized set cannot drift between them. Starts at the
// scope join with no leading AND: each call site supplies its own WHERE
// prefix.
const supersedeInactiveReducerUpdateRowFiltersSQL = `scope.scope_id = stale.scope_id
      AND stale.stage = 'reducer'
      AND stale.status IN ('pending', 'retrying', 'failed', 'dead_letter')
      -- The lock step also locks active-generation Crossplane carriers
      -- (#10269 arm above); they are never themselves superseded. Every
      -- other locked row passed the lock predicate's older-generation
      -- guard, so this id check is the whole generation recheck.
      AND stale.generation_id <> scope.active_generation_id
      -- #10269: a Crossplane row is superseded only when the locked set
      -- holds a newer carrier for the same scope: same domain, strictly
      -- newer generation in activation order (ingested_at, then
      -- generation_id — never updated_at, which claim/retry stamps
      -- rewrite), with the status the EvalPlanQual recheck confirmed at
      -- lock time still pending/retrying. The carrier set is
      -- conservative by design: claimed/running siblings are never in
      -- the locked set (an in-flight carrier can still dead-letter, and
      -- superseded is unreplayable, so pruning against it could destroy
      -- the only fallback), and a failed/dead_letter newest sibling
      -- absorbs nothing, so the older row survives to claim and rebind
      -- (#10106). A snapshot EXISTS over the live table was proven live
      -- to destroy that fallback when the carrier was claimed between
      -- snapshot and lock; membership in the lock step's output is the
      -- race-free form of the same check. Chain transitivity: a middle
      -- row is both carrier (for older rows) and superseded (against a
      -- newer carrier) in this one update.
      AND (stale.domain <> 'crossplane_satisfied_by_materialization'
           OR EXISTS (
             SELECT 1
             FROM superseded_crossplane_newest_carrier AS carrier
             JOIN scope_generations AS carrier_stale_generation
               ON carrier_stale_generation.scope_id = stale.scope_id
              AND carrier_stale_generation.generation_id = stale.generation_id
             WHERE carrier.scope_id = stale.scope_id
               AND (carrier.generation_ingested_at > carrier_stale_generation.ingested_at
                    OR (carrier.generation_ingested_at = carrier_stale_generation.ingested_at
                        AND carrier.generation_id > carrier_stale_generation.generation_id))))`
