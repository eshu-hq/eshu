// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// claimProjectorWorkSweepSQL is the maintenance half of the projector claim
// statement: the source scope, the stale-lease reclaim, and the
// stale-generation sweep. The pool half lives in
// claimProjectorWorkPoolSQL; the two concatenate byte for byte into
// claimProjectorWorkQuery below. The split is a file-length cut at a CTE
// boundary; keep it there.
//

// Scope claim fence (#7115). The in-flight guard below is a NOT EXISTS read
// against the statement snapshot, so on its own it cannot see a lease another
// claimer committed after that snapshot: two claimers whose snapshots disagree
// on a scope's oldest ready row would lock different rows and both claim. The
// fence closes that. The claim reads the scope's projector_scope_claim_fences
// row in its snapshot, locks the chosen work row and then that fence row (both
// SKIP LOCKED) joined on fence = snapshot fence, and bumps the fence in the
// same statement. A claimer that commits first leaves a new fence-row version;
// the later claimer's EvalPlanQual recheck compares it against the fence value
// its snapshot read, drops the row, and moves on to its next candidate. A claimer still in flight holds the fence row, so the other skips
// it without waiting.
//
// The fence lives on its own table, not on ingestion_scopes, because every
// FK child insert KEY SHAREs the scope row. A multixact holding a running KEY
// SHARE member and a committed updater sends LockRows down the update chain
// with a blocking wait that SKIP LOCKED does not cover, which deadlocked the
// claim against Ack when the fence was a scope-row column.
//
// Protocol invariant: any statement that creates a projector live lease
// (status claimed or running with a future claim_until) must lock the scope's
// projector_scope_claim_fences row and bump fence in the same transaction.
// The write-start marker (#7819) is the only other writer: it locks the fence
// row SKIP LOCKED before the generation row and bumps fence when the marker
// sets, so the marker commit and a claim's fence lock serialize. Nothing else
// may lock or update that table, no table may reference it, and this statement
// must not lock ingestion_scopes. Paths that only remove or renew an existing
// lease (Ack, Fail, retry, reclaim, heartbeat) do not touch the fence. Fence
// rows come from the ingestion_scopes AFTER INSERT trigger and the migration
// 130 backfill; a scope without one is unclaimable, which
// eshu_dp_projector_scopes_missing_claim_fence reports. Any other insert into
// the table must carry a NOT EXISTS guard, because INSERT ... ON CONFLICT waits
// on an in-flight bump of the conflicting row. The claim itself must never
// insert fence rows.
const claimProjectorWorkSweepSQL = `
WITH source_scoped_projector_work AS (
    SELECT work.work_item_id,
           candidate_scope.source_system
    FROM fact_work_items AS work
    JOIN ingestion_scopes AS candidate_scope
      ON candidate_scope.scope_id = work.scope_id
    WHERE work.stage = 'projector'
      AND (
          $4 = ''
          OR candidate_scope.source_system = $4
      )
),
-- Every maintenance branch locks its rows first with SKIP LOCKED and then
-- updates only the rows it locked (#7108). A blocking multi-row UPDATE here
-- let concurrent claimers lock the same stale rows in different orders and
-- deadlock (40P01). With every row lock non-blocking, the claim statement
-- never waits on a row lock, so it cannot join a wait cycle. The locking
-- SELECT repeats each row-self predicate so the EvalPlanQual recheck drops a
-- row another transaction changed after this statement's snapshot. NO KEY
-- UPDATE matches the lock the UPDATE takes and stays compatible with the
-- KEY SHARE locks that foreign-key inserts hold on scope_generations.
locked_stale_projector_duplicates AS (
    SELECT stale.work_item_id
    FROM fact_work_items AS stale
    JOIN source_scoped_projector_work AS scoped
      ON scoped.work_item_id = stale.work_item_id
    WHERE stale.stage = 'projector'
      AND stale.status IN ('claimed', 'running')
      AND stale.claim_until <= $1
      AND EXISTS (
          SELECT 1
          FROM fact_work_items AS live
          WHERE live.stage = 'projector'
            AND live.scope_id = stale.scope_id
            AND live.work_item_id <> stale.work_item_id
            AND live.status IN ('claimed', 'running')
            AND live.claim_until > $1
      )
    ORDER BY stale.work_item_id
    FOR NO KEY UPDATE OF stale SKIP LOCKED
),
reclaimed_stale_projector_duplicates AS (
    UPDATE fact_work_items AS stale
    SET status = 'retrying',
        lease_owner = NULL,
        claim_until = NULL,
        visible_at = $1,
        next_attempt_at = $1,
        updated_at = $1,
        failure_class = 'projector_stale_scope_reclaim',
        failure_message = 'expired duplicate projector lease reclaimed',
        failure_details = CASE
            WHEN stale.failure_class = 'projector_stale_scope_reclaim' THEN stale.failure_details
            ELSE (jsonb_build_object(
                'scope_id', stale.scope_id,
                'work_item_id', stale.work_item_id
            ) || ` + priorFailureStaleSQL + `)::text
        END
    FROM locked_stale_projector_duplicates AS locked
    WHERE stale.work_item_id = locked.work_item_id
      AND stale.stage = 'projector'
      AND stale.status IN ('claimed', 'running')
      AND stale.claim_until <= $1
),
-- supersedable_projector_generations is the snapshot view of stale work.
-- Supersede then locks each generation row and after it the work row, both
-- non-blocking, and updates only work rows it locked. The candidate never
-- claims a supersedable row, even one this statement could not lock; the
-- oldest-ready-row subquery still skips only rows superseded here, so a scope
-- whose stale row is busy yields no claim instead of a newer generation.
--
-- Two branches (#7130), disjoint by generation status, so UNION ALL adds no
-- duplicates. A pending or failed generation is stale only once a newer
-- same-scope generation has projector work. A superseded generation is
-- terminal by itself: its claimable rows (pending, retrying, and
-- expired-lease claimed/running, which the reclaim rank would otherwise
-- re-claim) must never be claimed, because projecting one retracts the
-- published generation's canonical graph before Ack refuses. A live lease is
-- left to its heartbeat, which refuses a superseded generation. failed and
-- dead_letter rows are never claim candidates and replay already leaves them
-- terminal, so this branch leaves them and their triage failure_class alone,
-- which also keeps a large legacy dead-letter backlog out of one claim
-- statement's sweep. The first branch is the pre-#7130 text, so its plan
-- (newer-sibling EXISTS as an index semi-join) is unchanged. The second scans
-- work rows through the (status) index and applies the source filter as a
-- probe that folds away when $4 is empty, instead of hash-joining every
-- projector row; an OR of the branches, or one shared scan feeding both,
-- measured slower (see the #7130 evidence note). An expired row beside a live
-- lease can also match the duplicate reclaim above; PostgreSQL applies one of
-- the two updates, and either way the row ends superseded by the next claim
-- at the latest. #7469: the first branch spares a generation that started
-- writing (marker set): its retry must run, or a partial overlay strands
-- until the collector's forced full snapshot. The superseded branch still
-- sweeps regardless of marker: that retirement already happened. #7473: the
-- first branch also spares a full generation (is_delta false) unless a newer
-- full exists. A newer delta does not cover the full's heal, so the full must
-- project first; a newer full covers it, so a full behind a newer full still
-- supersedes, as does any stale delta.
supersedable_projector_generations AS (
    SELECT stale.work_item_id,
           stale_generation.generation_id
    FROM fact_work_items AS stale
    JOIN source_scoped_projector_work AS scoped
      ON scoped.work_item_id = stale.work_item_id
    JOIN scope_generations AS stale_generation
      ON stale_generation.generation_id = stale.generation_id
    WHERE stale.stage = 'projector'
      AND stale.status IN ('pending', 'retrying', 'failed', 'dead_letter')
      AND stale_generation.status IN ('pending', 'failed')
      AND stale_generation.projection_write_started_at IS NULL
      AND EXISTS (
          SELECT 1
          FROM fact_work_items AS newer
          JOIN scope_generations AS newer_generation
            ON newer_generation.generation_id = newer.generation_id
          WHERE newer.stage = 'projector'
            AND newer.scope_id = stale.scope_id
            AND newer.work_item_id <> stale.work_item_id
            AND newer.status IN ('pending', 'retrying', 'claimed', 'running', 'succeeded', 'failed', 'dead_letter', 'superseded')
            AND (
                stale_generation.is_delta
                OR NOT newer_generation.is_delta
            )
            AND (
                newer_generation.ingested_at > stale_generation.ingested_at
                OR (
                    newer_generation.ingested_at = stale_generation.ingested_at
                    AND newer_generation.generation_id > stale_generation.generation_id
                )
            )
      )
    UNION ALL
    SELECT stale.work_item_id,
           stale_generation.generation_id
    FROM fact_work_items AS stale
    JOIN scope_generations AS stale_generation
      ON stale_generation.generation_id = stale.generation_id
    WHERE stale.stage = 'projector'
      AND stale.status IN ('pending', 'retrying', 'claimed', 'running')
      AND (
          stale.status IN ('pending', 'retrying')
          OR stale.claim_until <= $1
      )
      AND stale_generation.status = 'superseded'
      AND (
          $4 = ''
          OR EXISTS (
              SELECT 1
              FROM ingestion_scopes AS superseded_scope
              WHERE superseded_scope.scope_id = stale.scope_id
                AND superseded_scope.source_system = $4
          )
      )
),
-- The generation lock step re-reads the locked generation's status, so the
-- work rows below carry the status EvalPlanQual saw into failure_details.
-- #7469: it also carries each row's lock-time marker truth as marker_spared,
-- so a marker committed after the snapshot spares the row from the sweep and
-- holds the scope's newer rows, which the snapshot-only pool cannot see.
-- A superseded generation is never spared: its retirement already happened.
locked_stale_scope_generations AS (
    SELECT supersedable.work_item_id,
           stale_generation.generation_id AS generation_id,
           stale_generation.status AS generation_status,
           (stale_generation.status IN ('pending', 'failed')
               AND stale_generation.projection_write_started_at IS NOT NULL
           ) AS marker_spared
    FROM scope_generations AS stale_generation
    JOIN supersedable_projector_generations AS supersedable
      ON supersedable.generation_id = stale_generation.generation_id
    WHERE stale_generation.status IN ('pending', 'failed', 'superseded')
    ORDER BY stale_generation.generation_id
    FOR NO KEY UPDATE OF stale_generation SKIP LOCKED
),
-- The work lock step repeats the widened row-self predicate so EvalPlanQual
-- drops a row another transaction claimed or renewed after the snapshot.
locked_stale_projector_generations AS (
    SELECT stale.work_item_id,
           locked_generation.generation_status
    FROM fact_work_items AS stale
    JOIN locked_stale_scope_generations AS locked_generation
      ON locked_generation.work_item_id = stale.work_item_id
    WHERE NOT locked_generation.marker_spared
      AND stale.stage = 'projector'
      AND (
          stale.status IN ('pending', 'retrying', 'failed', 'dead_letter')
          OR (stale.status IN ('claimed', 'running') AND stale.claim_until <= $1)
      )
    ORDER BY stale.work_item_id
    FOR NO KEY UPDATE OF stale SKIP LOCKED
),
-- The details fold the row's prior failure into prior_failure (#7320) so a
-- failed or dead-lettered row keeps why it failed; the class and message stay
-- the supersede marker.
superseded_stale_projector_generations AS (
    UPDATE fact_work_items AS stale
    SET status = 'superseded',
        lease_owner = NULL,
        claim_until = NULL,
        visible_at = NULL,
        next_attempt_at = NULL,
        updated_at = $1,
        failure_class = 'projector_superseded_by_newer_generation',
        failure_message = 'projector work superseded by newer same-scope generation',
        failure_details = jsonb_build_object(
            'scope_id', stale.scope_id,
            'work_item_id', stale.work_item_id,
            'generation_id', stale.generation_id,
            'generation_status', locked.generation_status
        ) || ` + priorFailureStaleSQL + `
    FROM locked_stale_projector_generations AS locked
    WHERE stale.work_item_id = locked.work_item_id
      AND stale.stage = 'projector'
      AND (
          stale.status IN ('pending', 'retrying', 'failed', 'dead_letter')
          OR (stale.status IN ('claimed', 'running') AND stale.claim_until <= $1)
      )
    RETURNING stale.work_item_id, stale.generation_id
),
-- A superseded generation is already terminal, so this stays a no-op for the
-- #7130 branch and never rewrites its superseded_at.
superseded_stale_scope_generations AS (
    UPDATE scope_generations AS generation
    SET status = 'superseded',
        superseded_at = $1
    FROM superseded_stale_projector_generations AS stale
    WHERE generation.generation_id = stale.generation_id
      AND generation.status IN ('pending', 'failed')
),
`

// claimProjectorWorkQuery claims at most one projector work row: the sweep
// half above plus the pool half in claimProjectorWorkPoolSQL.
const claimProjectorWorkQuery = claimProjectorWorkSweepSQL + claimProjectorWorkPoolSQL
