// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backfill

// The scopeReplayFloor fragments are the single definition of the reopen
// bound: the per-scope replay floor CTE plus the joins, failed-generation
// exclusion, floor tuple predicate, and ordering every floored reopen listing
// shares. ListSucceededReducerWorkItemsByDomainQuery composes them unchanged
// (its text is byte-identical to the shipped literal), and the
// deployment_mapping and code_import_repo_edge listings in
// ingestion_queries.go (#7637) compose the same fragments with their own
// select list and domain line. There is exactly one floor text: a future edit
// cannot fix the bound for one listing and silently leave the others on a
// hand copy that drifted.
const scopeReplayFloorCTE = `
WITH scope_replay_floor AS MATERIALIZED (
  SELECT scope.scope_id,
         COALESCE(active_generation.ingested_at, latest_generation.ingested_at) AS floor_ingested_at,
         COALESCE(active_generation.generation_id, latest_generation.generation_id) AS floor_generation_id
  FROM ingestion_scopes AS scope
  LEFT JOIN scope_generations AS active_generation
    ON active_generation.scope_id = scope.scope_id
   AND active_generation.generation_id = scope.active_generation_id
  LEFT JOIN LATERAL (
      SELECT candidate.ingested_at, candidate.generation_id
      FROM scope_generations AS candidate
      WHERE candidate.scope_id = scope.scope_id
      ORDER BY candidate.ingested_at DESC, candidate.generation_id DESC
      LIMIT 1
  ) AS latest_generation ON true
)
`

// scopeReplayFloorSelectWorkItemIDs is the correlation listing's select list:
// bare IDs, because the correlation reopen applies no partition-memo gate.
const scopeReplayFloorSelectWorkItemIDs = `SELECT work.work_item_id
`

// scopeReplayFloorSelectWorkItemRefs is the relationship listings' select
// list: IDs plus the (scope_id, generation_id) partition columns the reopen
// partition-memo gate keys on (issue #4770).
const scopeReplayFloorSelectWorkItemRefs = `SELECT work.work_item_id, work.scope_id, work.generation_id
`

// scopeReplayFloorJoins joins the work item to its generation row and the
// scope's floor row.
const scopeReplayFloorJoins = `FROM fact_work_items AS work
JOIN scope_generations AS work_generation
  ON work_generation.scope_id = work.scope_id
 AND work_generation.generation_id = work.generation_id
JOIN scope_replay_floor AS floor
  ON floor.scope_id = work.scope_id
`

// scopeReplayFloorStage opens the WHERE clause on the reducer stage. It is
// the single marker the partition-scoped pass inserts its partition conjunct
// after (targeted.CorrelationReopenStageMarker), so every listing carries it
// exactly once.
const scopeReplayFloorStage = `WHERE work.stage = 'reducer'
`

// scopeReplayFloorJoinsAndStage joins the work item to its generation row and
// the scope's floor row, then opens the WHERE clause on the reducer stage.
const scopeReplayFloorJoinsAndStage = scopeReplayFloorJoins + scopeReplayFloorStage

// scopeReplayFloorBound is the shared tail of every floored reopen listing:
// succeeded rows only, the failed-generation exclusion, the floor tuple
// predicate, and the stable reopen order. Each listing inserts its own domain
// line between scopeReplayFloorJoinsAndStage and this fragment.
const scopeReplayFloorBound = `  AND work.status = 'succeeded'
  AND work_generation.status <> 'failed'
  AND (work_generation.ingested_at, work_generation.generation_id)
      >= (floor.floor_ingested_at, floor.floor_generation_id)
ORDER BY work.updated_at ASC, work.work_item_id ASC
`

// ListSucceededReducerWorkItemsByDomainQuery selects the succeeded reducer work
// items for one domain that are still worth replaying. It is the
// domain-parameterized form of the deployment_mapping / code_import_repo_edge
// listings, used by the generic correlation reopen below.
//
// The bound is a per-scope REPLAY FLOOR. Each scope contributes only the work
// items on its active generation or newer; when the scope has no usable active
// generation, only its LATEST generation. Succeeded reducer rows are never
// terminalized (supersedeInactiveReducerGenerationsCTE sweeps only
// pending/retrying/failed/dead_letter), so one accumulates per (scope,
// generation, domain) for the life of the store. Without a floor the ingester's
// per-drain replay would resurrect the entire ingestion history into 'pending'
// on every drain, growing linearly with generation count, and the reducer would
// then re-terminalize it — pure churn. Measured on 900 scopes x 25 generations
// (docs/internal/evidence/5426-reopen-bound-proof.sql): 22 551 rows unbounded
// versus 903 with the floor, per domain; 135 000 versus 5 400 across the six
// domains measured before completion-driven replay removed CI/CD and supply.
// See the historical six-domain evidence table in this package's README.
//
// The floor must cover THREE shapes of "no active generation", not just the
// happy one, because active_generation_id is nullable AND carries no foreign
// key (schema/data-plane/postgres/001_ingestion_scopes.sql):
//
//   - Never activated. The activation race this replay exists for. Its latest
//     generation is the live one, so the floor keeps it. This case MUST keep
//     reopening; a bound that skipped it would skip exactly the replay the
//     change targets.
//   - Active generation failed and was nulled. failProjectorWorkQuery
//     (projector_queue_sql.go) sets active_generation_id = NULL when the active
//     generation fails. An IS NOT NULL guard alone reads that as "never
//     activated" and reopens EVERY generation of that scope on EVERY drain —
//     the exact unbounded churn the bound exists to prevent, and unreachable by
//     supersession because supersedeInactiveReducerGenerationsCTE carries the
//     same guard and never terminalizes those rows. Measured in the proof
//     script: one failed 25-generation scope contributes 25 rows per drain under
//     the guard, 1 under the floor alone, 0 once the failed-generation exclusion
//     below also applies.
//   - Dangling pointer. No FK constrains active_generation_id, so it can name a
//     generation row that is gone. The COALESCE falls back to the latest
//     generation rather than either reopening everything or, worse, silently
//     listing nothing for that scope.
//
// The floor alone still leaves the failed shape churning, which is why the WHERE
// clause carries work_generation.status <> 'failed' as well. The failed
// generation is the scope's LATEST, so the fallback picks it and its succeeded
// rows sit exactly AT the floor: one row per domain reopened on every drain,
// re-succeeded by the reducer, and reopened again forever. Nothing that
// generation re-decides can ever be read — failProjectorWorkQuery nulls the
// scope's active pointer in the same statement that fails the generation, and
// every fact-backed correlation read surface joins
// scope.active_generation_id = fact.generation_id — so the correct replay count
// for it is zero, not one.
//
// The exclusion is on the WORK ITEM's own generation rather than on the
// fallback's candidate set on purpose. Excluding failed generations from the
// fallback instead would LOWER the floor to the newest non-failed generation,
// which leaves the failed generation's rows (still at or above the lowered
// floor) churning AND starts the older generation's rows churning too — strictly
// worse, and equally unreadable while active_generation_id is NULL.
//
// A failed generation is never the active one, so this cannot under-replay a
// query-visible generation: failProjectorWorkQuery marks status = 'failed' only
// for generations in status 'pending' or 'active', and nulls active_generation_id
// in the same statement whenever the failed generation is the one it names. The
// mixed shape — a NEWER generation failing while an OLDER one stays active — is
// covered by the same predicate: the scope keeps its active floor and replays it,
// while the failed newer generation drops out.
//
// Replaying below the floor cannot change query truth for the fact-backed
// domains: facts_active_container_image_identity.go,
// facts_active_cicd_run_correlation.go, and facts_active_supply_chain_impact.go
// all join scope.active_generation_id = fact.generation_id, so a stale
// generation's re-decision lands in rows no query reads.
//
// deployable_unit_correlation and kubernetes_correlation_materialization write
// GRAPH EDGES rather than those fact rows, so that argument does not carry them.
// The floor is if anything MORE necessary for the two edge writers: a stale
// generation's re-projection writes edges anchored to a generation the read
// surfaces no longer resolve, so it costs graph writes to produce output no
// query reads. Their replay value is entirely in the current generation, which
// the floor keeps.
//
// The scope_replay_floor CTE is MATERIALIZED on purpose. Inlined, Postgres
// re-derives the floor once per candidate work item (22 551 lateral lookups);
// materialized it is computed once per scope and hash-joined. Measured at 900
// scopes x 25 generations against the PRODUCTION index
// (stage, domain, status, visible_at, updated_at DESC): 166.0 ms inlined versus
// 20.3 ms materialized, against 29.8 ms for the unbounded listing it replaces.
//
// The failed-generation exclusion costs roughly +0.3 ms on a ~20 ms listing:
// immaterial, but a cost, not noise. Both arms run in the same script and the
// same run, so the per-run gap is PAIRED, and its sign is the statistic that
// carries. Comparing that gap against either arm's spread ACROSS runs is the
// wrong test — pairing is what cancels the between-run common-mode variance that
// makes the absolute times wander by more than 1 ms, which is why the gap's sign
// stays put while the times do not. Across 28 paired runs — six in the evidence
// doc, six on an independent reviewer's Postgres 16.14, sixteen re-measured on
// this machine — 27 put the predicate arm slower. The one that does not is run
// 4 of the re-measured committed-order block, at -0.852 ms. Two further pairs
// measured earlier and outside that set of 28 also flipped: 20.053 ms with the
// predicate against 20.262 ms floor-only on the same machine, and 22.031
// against 22.836 ms from a separately sourced run. Counting those, 27 of 30
// paired runs on record put the predicate arm slower, and all three flippers
// are outliers against that record, not a counterweight to it.
//
// The cost is CPU, not I/O, which is why the buffer counts do not show it: both
// arms read exactly the same pages (Buffers: shared hit=3396 at the top of both
// plans, identical in every run), and that proves no extra I/O rather than no
// cost. work_generation.status <> 'failed' is evaluated on the seq scan that
// builds the join's hash side, so it runs across all 22 551 scope_generations
// rows to remove one; the inner hash join then emits 22 550 rows against 22 551.
// Node timings were collected only for the eight committed-order re-measured
// runs; across those eight that scan's own time goes 1.29-1.41 ms floor-only to
// 1.70-2.13 ms shipped, the predicate arm slower in all eight, which is where
// the end-to-end gap lives. Read that node carefully: a second seq scan on
// scope_generations sits inside the CTE, so a first-match parse of the plan
// picks the wrong one and shows no effect. Rows listed per domain go 903 ->
// 902, the whole difference being the failed scope's one perpetually-churning
// row.
//
// A pass runs six of these listings, so the exclusion is under 0.05% of its
// 5.927 s wall time — cheap against the churn it removes.
//
// The work_generation join is inner, which is safe only because both of its
// legs hold for every row.
//
// The generation_id leg is enforced: fact_work_items declares generation_id as
// a NOT NULL foreign key onto scope_generations
// (schema/data-plane/postgres/005_fact_work_items.sql), so the referenced
// generation row always exists.
//
// The scope_id leg is NOT enforced. fact_work_items carries two independent
// single-column foreign keys — scope_id onto ingestion_scopes and generation_id
// onto scope_generations — and scope_generations' primary key is generation_id
// alone, so no composite key stops a work item from naming a generation that
// belongs to a DIFFERENT scope. It holds because every producer writes both
// columns from one intent: enqueueProjectorWorkQuery and the reducer batch
// enqueue bind the pair from a single work intent (projector_queue_sql.go,
// reducer_queue.go); the liveness re-enqueue selects both columns off the same
// scope_generations row (generation_liveness_sql.go); and the recovery
// refinalize selects scope.scope_id with that scope's own
// scope.active_generation_id (recovery.go).
//
// If that ever broke, the failure direction is under-replay — the mismatched
// row drops out of this listing rather than reopening spuriously — which is the
// same silent direction as the bug this reopen exists to fix, so it is named
// here rather than assumed. The constraint is not added because closing it
// needs a UNIQUE (generation_id, scope_id) on scope_generations plus a
// validating composite FK on fact_work_items, i.e. a full-table ALTER on the
// busiest queue table of every existing install — not cheap, and out of
// proportion to a hazard no producer can currently reach.
//
// The listing is NOT where this pass spends its time. Production issues one
// client round-trip per reopened row (ReopenSucceededReducerWorkItems loops over
// queue.ReopenSucceeded), so in the historical six-domain measurement the
// listings cost 91 ms while the whole pass cost 5.927 s. The pre-change ingester
// baseline was ZERO, because it ran none of this. See
// TestCorrelationReopenPerDrainCostProof for the measurement and for the part
// no shim measures: the reducer re-executing every reopened item on every
// drain, forever.
const ListSucceededReducerWorkItemsByDomainQuery = scopeReplayFloorCTE +
	scopeReplayFloorSelectWorkItemIDs +
	scopeReplayFloorJoinsAndStage +
	`  AND work.domain = $1
` +
	scopeReplayFloorBound

// ListSucceededDeploymentMappingWorkItemsQuery selects succeeded
// deployment_mapping reducer work items, including their (scope_id,
// generation_id) partition columns (issue #4770). The reopen
// partition-memo gate (applyReopenPartitionMemoGate) uses these to skip
// reopening a work item whose partition already committed backward evidence
// under the CURRENT catalog fingerprint, mirroring
// backfill.ApplyDeferredPartitionMemoGate's read-side logic for the deferred
// backfill's own fact load.
//
// The listing carries the same per-scope replay floor and failed-generation
// exclusion as listSucceededReducerWorkItemsByDomainQuery (issue #7637),
// composed from the shared scopeReplayFloor fragments — each scope
// contributes only its active generation or newer, or its latest generation
// when it has no usable active generation. Without the floor the pass
// reopened every succeeded row in the store on every drain, including dead
// history on superseded generations. See that query's doc comment for the
// bound's rationale, the three no-active-generation shapes, and the
// measurement.
const ListSucceededDeploymentMappingWorkItemsQuery = scopeReplayFloorCTE +
	scopeReplayFloorSelectWorkItemRefs +
	scopeReplayFloorJoinsAndStage +
	`  AND work.domain = 'deployment_mapping'
` +
	scopeReplayFloorBound

// ListSucceededCodeImportRepoEdgeWorkItemsQuery selects the code-import
// repo-edge reducer work items that already succeeded, including their
// (scope_id, generation_id) partition columns (issue #4770 — see
// ListSucceededDeploymentMappingWorkItemsQuery). They are replayed after
// deferred maintenance so the projection re-runs once the cross-scope
// package-registry ownership facts it joins against are present — the same
// after-the-fact dependency the deployment_mapping reopen handles. It
// carries the same replay floor, composed from the same shared fragments.
const ListSucceededCodeImportRepoEdgeWorkItemsQuery = scopeReplayFloorCTE +
	scopeReplayFloorSelectWorkItemRefs +
	scopeReplayFloorJoinsAndStage +
	`  AND work.domain = 'code_import_repo_edge'
` +
	scopeReplayFloorBound
