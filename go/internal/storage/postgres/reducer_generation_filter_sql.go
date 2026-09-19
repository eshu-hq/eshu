// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// activeFactWorkItemsScopeStateCTE resolves each scope's active generation once
// (#6794): one row per scope, with the active generation's id and ingested_at
// when that generation exists and belongs to the scope. It is hash-joined to
// the work rows instead of joining ingestion_scopes and the active
// scope_generations row once per work item, which made Postgres underestimate
// the (scope_id, generation_id) join by orders of magnitude and pick two index
// probes per work item. AS MATERIALIZED is load-bearing: the CTE is referenced
// once, and the inlined form planned back into the same per-row nested loops.
// The LATERAL ... LIMIT 1 lookup is one primary-key probe per scope; without
// LIMIT 1 the planner flattened it into a full scan of scope_generations.
const activeFactWorkItemsScopeStateCTE = `active_fact_work_items_scope_state AS MATERIALIZED (
  SELECT scope.scope_id,
         scope.active_generation_id,
         active_generation.generation_id AS present_active_generation_id,
         active_generation.ingested_at AS active_ingested_at
  FROM ingestion_scopes AS scope
  LEFT JOIN LATERAL (
    SELECT generation.generation_id, generation.ingested_at
    FROM scope_generations AS generation
    WHERE generation.generation_id = scope.active_generation_id
      AND generation.scope_id = scope.scope_id
    -- generation_id is the primary key; LIMIT 1 keeps this a per-scope
    -- index probe instead of letting the planner flatten it into a scan.
    LIMIT 1
  ) AS active_generation ON TRUE
)`

// activeFactWorkItemsFromWhere is the FROM/WHERE body shared by every
// active_fact_work_items definition. It needs activeFactWorkItemsScopeStateCTE
// earlier in the same WITH list, requires the work item's generation to belong
// to its scope, and hides only unleased reducer rows on a generation older than
// the scope's active one (older ingested_at, or equal ingested_at with a
// smaller generation_id). Claimed/running rows stay visible so a live stale
// worker remains diagnosable instead of disappearing.
const activeFactWorkItemsFromWhere = `FROM fact_work_items AS work
  JOIN active_fact_work_items_scope_state AS scope_state
    ON scope_state.scope_id = work.scope_id
  JOIN scope_generations AS stale_generation
    ON stale_generation.scope_id = work.scope_id
   AND stale_generation.generation_id = work.generation_id
  WHERE NOT (
    work.stage = 'reducer'
    AND work.status IN ('pending', 'retrying', 'failed', 'dead_letter')
    AND scope_state.present_active_generation_id IS NOT NULL
    AND work.generation_id <> scope_state.active_generation_id
    AND (
      stale_generation.ingested_at < scope_state.active_ingested_at
      OR (
        stale_generation.ingested_at = scope_state.active_ingested_at
        AND stale_generation.generation_id < scope_state.present_active_generation_id
      )
    )
  )`

// activeFactWorkItemsCTE keeps live status, drain, and observer reads from
// reporting unleased reducer rows whose generation is older than the scope's
// current active generation (see activeFactWorkItemsFromWhere).
//
// The constant expands to two CTEs, so embed it as `WITH ` + activeFactWorkItemsCTE
// followed by any further CTEs; the name active_fact_work_items_scope_state is
// reserved by it. The status snapshot does not embed it per read: it evaluates
// active_fact_work_items once for all five consumers (activeWorkSummaryQuery).
const activeFactWorkItemsCTE = `
` + activeFactWorkItemsScopeStateCTE + `,
active_fact_work_items AS (
  SELECT work.*
  ` + activeFactWorkItemsFromWhere + `
)
`

// activeFactWorkItemsPerRowCTE is the per-row form of activeFactWorkItemsCTE
// with the same row semantics: it joins ingestion_scopes and the active
// scope_generations row once per work item instead of materializing per-scope
// state first. Selective probes use it: the drain EXISTS
// (activeReducerGraphWorkQuery) stops at the first visible match, and the
// write-backpressure count (reducerGraphWriteTimeoutDepthQuery) touches only
// its few matching rows, while the materialized form must build one row per
// ingestion scope first (#6794 review F-03, on a 5k-scope fixture: drain EXISTS
// 0.1ms to 5ms with one qualifying row, backpressure count 0.6ms to 4.8ms with
// 50). Full-set aggregates keep the materialized form.
// TestActiveFactWorkItemsFormsSelectTheSameRows keeps the two forms in step.
const activeFactWorkItemsPerRowCTE = `
active_fact_work_items AS (
  SELECT work.*
  FROM fact_work_items AS work
  JOIN ingestion_scopes AS scope
    ON scope.scope_id = work.scope_id
  JOIN scope_generations AS stale_generation
    ON stale_generation.scope_id = work.scope_id
   AND stale_generation.generation_id = work.generation_id
  LEFT JOIN scope_generations AS active_generation
    ON scope.active_generation_id = active_generation.generation_id
   AND active_generation.scope_id = work.scope_id
  WHERE NOT (
    work.stage = 'reducer'
    AND work.status IN ('pending', 'retrying', 'failed', 'dead_letter')
    AND active_generation.generation_id IS NOT NULL
    AND work.generation_id <> scope.active_generation_id
    AND (
      stale_generation.ingested_at < active_generation.ingested_at
      OR (
        stale_generation.ingested_at = active_generation.ingested_at
        AND stale_generation.generation_id < active_generation.generation_id
      )
    )
  )
)
`

// supersedeInactiveReducerGenerationsCTE terminalizes unleased older-generation
// reducer rows during claim so audit history remains durable without letting
// obsolete work keep readiness in progress forever.
//
// A readiness-gated domain (reducer_claim_readiness_requirements) must NOT be
// superseded while its required canonical-node phase is still unmet
// (#4445/A2): 'superseded' is a terminal, unreplayable status (only
// status='succeeded' rows are reopened by ReopenSucceeded/ReplayDomain), so
// superseding a row whose readiness gate never opened permanently drops that
// materialization intent and produces incomplete graph output for the
// domain. The trailing readiness-gate predicate mirrors
// reducerClaimReadinessGateSQL: it holds a stale row out of the supersede
// sweep for exactly as long as the outer candidate CTE would also refuse to
// claim it on readiness grounds, so a stale row becomes supersede-eligible
// the moment (and not before) its domain's readiness would independently
// allow a claim. Domains with no readiness requirement row are unaffected —
// the NOT EXISTS is vacuously true for them, preserving today's
// generation-ordering-only behavior.
//
// This CTE must be declared AFTER reducerClaimReadinessRequirementsCTE in the
// enclosing WITH clause so the readiness-gate predicate below can reference
// reducer_claim_readiness_requirements; Postgres CTEs may only reference
// earlier siblings in the same WITH list (barring RECURSIVE, which is not
// used here).
var supersedeInactiveReducerGenerationsCTE = `
superseded_stale_reducer_generations AS (
    UPDATE fact_work_items AS stale
    SET status = 'superseded',
        container_image_identity_v2_authorized_status = 'superseded',
        container_image_identity_v3_authorized_status = CASE
            WHEN stale.container_image_identity_v3_required THEN 'superseded'
            ELSE ''
        END,
        lease_owner = NULL,
        claim_until = NULL,
        visible_at = NULL,
        next_attempt_at = NULL,
        updated_at = $1,
        failure_class = 'reducer_superseded_by_newer_active_generation',
        failure_message = 'reducer work superseded by newer active generation',
        failure_details = jsonb_build_object(
            'reason', 'inactive_generation',
            'scope_id', stale.scope_id,
            'work_item_id', stale.work_item_id,
            'generation_id', stale.generation_id,
            'active_generation_id', scope.active_generation_id,
            'domain', stale.domain
        )::text
    FROM ingestion_scopes AS scope,
         scope_generations AS stale_generation,
         scope_generations AS active_generation
    WHERE stale.stage = 'reducer'
      AND stale.status IN ('pending', 'retrying', 'failed', 'dead_letter')
      AND ($2::text[] IS NULL OR stale.domain = ANY($2::text[]))
      AND scope.scope_id = stale.scope_id
      AND stale_generation.scope_id = stale.scope_id
      AND stale_generation.generation_id = stale.generation_id
      AND scope.active_generation_id = active_generation.generation_id
      AND active_generation.scope_id = stale.scope_id
      AND stale.generation_id <> scope.active_generation_id
      AND (
        stale_generation.ingested_at < active_generation.ingested_at
        OR (
          stale_generation.ingested_at = active_generation.ingested_at
          AND stale_generation.generation_id < active_generation.generation_id
        )
      )
      AND ` + reducerClaimReadinessGateSQL("stale", "supersede_readiness_req", "supersede_readiness_phase") + `
    RETURNING stale.work_item_id
)
`
