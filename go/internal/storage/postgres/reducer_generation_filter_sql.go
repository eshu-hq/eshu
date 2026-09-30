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
        failure_details = (jsonb_build_object(
            'reason', 'inactive_generation',
            'scope_id', stale.scope_id,
            'work_item_id', stale.work_item_id,
            'generation_id', stale.generation_id,
            'active_generation_id', scope.active_generation_id,
            'domain', stale.domain
        ) || ` + priorFailureStaleSQL + `)::text
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

// priorFailureStaleSQL and priorFailureWorkSQL are the jsonb expression that
// folds a work row's failure evidence into the failure_details a supersede
// statement writes (#7320). A supersede overwrites failure_class, failure_message and
// failure_details with its own marker; without this fold a failed or
// dead-lettered row loses the reason it failed, and so does a claimed or
// running row that carries its last retry's cause.
//
// Every column is read from the OLD row: in an UPDATE, SET expressions see the
// row version being replaced, which under Read Committed is the version the
// locking step re-read (EvalPlanQual), so a failure committed after the
// statement's snapshot is folded, not the stale snapshot value. Five fields,
// no more: supersede leaves attempt_count and last_attempt_at in place, so
// copying them would only duplicate columns.
//
// The key exists only when the old row failed (status failed or dead_letter)
// or any of its three failure fields is non-blank, using the blank test of
// status_active_work_summary.go. A pending or retrying row that never failed
// yields an empty object, so its details stay what the statement wrote before
// this fold.
//
// The old failure_details is embedded as a JSON string, verbatim. It is free
// text or JSON (queue/failure_metadata.go), so it is never cast to jsonb: one
// non-JSON row would abort the claim statement for every worker. A NULL column
// stays JSON null, distinct from an empty string.
//
// superseded is terminal and in no supersede source set, so a row that already
// holds prior_failure is never folded again; the revive paths null the three
// fields first.
//
// The two constants are the same text for the two aliases the supersede
// statements give the row they update: stale (claim sweep, Ack obsolete
// supersede, reducer sweep) and work (Heartbeat supersede, Ack refusal). They
// are constants, not a function of the alias, so the statements that embed them
// stay constant SQL; TestPriorFailureFragmentsAgree derives one from the other
// and TestSupersedeStatementsFoldPriorFailure requires each writer to embed the
// constant that matches its own alias.
const priorFailureStaleSQL = `(CASE
        WHEN stale.status IN ('failed', 'dead_letter')
          OR NULLIF(BTRIM(COALESCE(stale.failure_class, '')), '') IS NOT NULL
          OR NULLIF(BTRIM(COALESCE(stale.failure_message, '')), '') IS NOT NULL
          OR NULLIF(BTRIM(COALESCE(stale.failure_details, '')), '') IS NOT NULL
        THEN jsonb_build_object('prior_failure', jsonb_build_object(
            'status', stale.status,
            'failure_class', stale.failure_class,
            'failure_message', stale.failure_message,
            'failure_details', stale.failure_details,
            'updated_at', stale.updated_at
        ))
        ELSE '{}'::jsonb
    END)`

const priorFailureWorkSQL = `(CASE
        WHEN work.status IN ('failed', 'dead_letter')
          OR NULLIF(BTRIM(COALESCE(work.failure_class, '')), '') IS NOT NULL
          OR NULLIF(BTRIM(COALESCE(work.failure_message, '')), '') IS NOT NULL
          OR NULLIF(BTRIM(COALESCE(work.failure_details, '')), '') IS NOT NULL
        THEN jsonb_build_object('prior_failure', jsonb_build_object(
            'status', work.status,
            'failure_class', work.failure_class,
            'failure_message', work.failure_message,
            'failure_details', work.failure_details,
            'updated_at', work.updated_at
        ))
        ELSE '{}'::jsonb
    END)`

// PriorFailureWorkSQL is priorFailureWorkSQL for a writer outside this package
// that updates fact_work_items under the `work` alias and rewrites
// failure_details, such as the admin operator note (#7388): it keeps the row's
// current failure evidence under `prior_failure` instead of replacing it. It is
// the same constant, so the fold keeps one definition.
const PriorFailureWorkSQL = priorFailureWorkSQL
