// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package poison

import (
	queuestore "github.com/eshu-hq/eshu/go/internal/storage/postgres/queue"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// CountDeadLettersQuery detects the dead-letter/poison class the #4727
// projector-claimer fix and the generation-liveness sweep do not reach: a
// fact_work_items row whose status is 'dead_letter' with no strictly-newer
// scope_generations row for the same scope. dead_letter is a terminal status
// (never re-claimed by the normal claim path), so once a scope's newest
// generation lands in this state the scope can never self-heal without an
// operator or this bounded arm. It is exported alongside
// RecoverDeadLettersQuery so the pair stays addressable as one contract: the
// parent postgres package pins the recovery statement in its container-image
// capability latch, and the in-package EXPLAIN test derives its plan from
// this constant.
//
// "Strictly newer" mirrors the existing generation-ordering comparator used by
// supersedeOrphanedActiveGenerationsQuery: (ingested_at, generation_id) compared
// lexicographically, with generation_id as the tiebreaker for equal
// ingested_at. A scope with ANY newer generation (regardless of that newer
// generation's own status) has already moved on, so the dead-letter row is
// historical, not a live poison signal — the newer generation's own reducer
// work (or its own dead-letter class) is what an operator should look at.
//
// This is a read-only aggregate: no row is claimed, locked, or mutated. Cost is
// bounded to the dead_letter subset via fact_work_items_dead_letter_poison_idx
// (partial index on status = 'dead_letter'); the NOT EXISTS anti-join reads the
// same scope_generations_scope_idx / scope_generations_scope_latest_lookup_idx
// indexes generation-liveness queries already use.
//
// Parameter order:
//
//	$1 now (oldest_poison_age_seconds anchor)
const CountDeadLettersQuery = `
SELECT
    count(DISTINCT dead.scope_id) AS poison_scopes,
    count(*) AS poison_items,
    COALESCE(EXTRACT(EPOCH FROM ($1 - min(dead.updated_at))), 0) AS oldest_poison_age_seconds
FROM fact_work_items AS dead
JOIN scope_generations AS gen ON gen.generation_id = dead.generation_id
WHERE dead.status = 'dead_letter'
  AND NOT EXISTS (
      SELECT 1
      FROM scope_generations AS newer
      WHERE newer.scope_id = dead.scope_id
        AND newer.generation_id <> gen.generation_id
        AND (
            newer.ingested_at > gen.ingested_at
            OR (
                newer.ingested_at = gen.ingested_at
                AND newer.generation_id > dead.generation_id
            )
        )
  )
`

// RecoverDeadLettersQuery bounds-recovers the poison dead-letter class by
// re-enqueuing a fresh pending attempt for each row selected by
// CountDeadLettersQuery's same predicate, gated by a per-item
// poison_recovery_attempts budget carried in the JSONB payload (mirroring
// recoverWedgedActiveGenerationsQuery's liveness_recovery_attempts idiom — no
// new schema column). This statement only runs when the operator has opted
// into bounded auto-retry (ESHU_POISON_LIVENESS_AUTO_RETRY_ENABLED); the
// default posture is surface-only via the gauge above.
//
// Concurrency: the write-time WHERE re-checks status = 'dead_letter' on the
// TARGET ROW at UPDATE time, not only the read-time candidate snapshot,
// mirroring recoverWedgedActiveGenerationsQuery's write-time in-flight
// re-verify (#4464 discipline). Under Read Committed, if a concurrent worker
// reclaims this exact row (dead_letter -> claimed) between this statement's
// snapshot and its row-level lock, the UPDATE's own WHERE re-evaluates
// (EvalPlanQual recheck) against the now-committed row and finds
// status <> 'dead_letter', so it affects zero rows instead of clobbering the
// concurrent claim. The conflict domain is work_item_id (the primary key), so
// each candidate row is an independent lock target.
//
// Lock order (#8261): the poison CTE stays an unlocked selection (its ORDER
// BY uses the database's default collation, which picks the batch). The
// poison_locked CTE then locks the selected rows that are still dead letters
// under their recovery budget, in work_item_id COLLATE "C" order, before the
// UPDATE touches any. That is the order the recovery and admin replays and
// the reducer claim sweep lock in (#8205). Without it the UPDATE locked rows
// in the selection's default-collation order: on a database whose collation
// is not C (en_US.utf8 is the common default) and work ids whose two orders
// disagree (an upper-case entity key), poison recovery and a concurrent
// replay over the same dead letters locked them in opposite orders and one
// died with 40P01. The lock step's status and budget predicates are
// rechecked on the newest row version when a lock waits (EvalPlanQual), so a
// row another worker reclaimed or re-drove after the selection read it is
// dropped there; the UPDATE repeats the status check.
//
// Retry allowance (#11217): a recovered row gets the retries a new row gets,
// the same allowance the three operator replay writers grant (#11182). The
// SET list appends queuestore.ReplayRetryAllowanceAssignments, the shared
// fragment, never a copy: replay_attempt_baseline takes the row's
// attempt_count (projector rows only) and admission_wait_attempts goes back to
// 0. It also raises a projector row's attempt_count to at least 1, as the
// replays do, so a dead letter at attempt 0 (the operator skip of a pending
// row) gets baseline 1 at attempt 1 and its first claim counts 1, not 0.
// attempt_count is never lowered: it stays the monotonic claim fence. A
// reducer row keeps attempt_count and its baseline; the reducer budget does
// not read the baseline yet (#11184). The fragment is the last assignment,
// after reopened_at, so the literal that opens the SET list still names
// reopened_at for the #9320 source guard. The right-hand sides read the row
// version the UPDATE locked, so under a concurrent commit the baseline is the
// newest committed attempt_count, not the snapshot value. The per-row bound
// is unchanged: poison_recovery_attempts < $2 is still the only gate, so a
// row gets at most 1 + $2 allowances of three projection claims and 48
// admission waits each.
//
// Parameter order:
//
//	$1 now              (updated_at / visible_at stamp)
//	$2 max recover attempts (poison_recovery_attempts budget ceiling)
//	$3 batch limit       (max rows re-driven per sweep)
//
// RecoverDeadLettersQuery is exported because the parent postgres package
// pins this statement in its container-image capability latch
// (TestContainerImageIdentityV3CapabilityLatchCoversQueueLifecycle): the
// latch asserts the recovery UPDATE advances digest-v3 authorization. It is
// a var, not a const, because the stage-gated requeue reason arrives through
// the queuestore helper (#11615).
var RecoverDeadLettersQuery = `
WITH poison AS (
    SELECT dead.work_item_id
    FROM fact_work_items AS dead
    JOIN scope_generations AS gen ON gen.generation_id = dead.generation_id
    WHERE dead.status = 'dead_letter'
      AND NOT EXISTS (
          SELECT 1
          FROM scope_generations AS newer
          WHERE newer.scope_id = dead.scope_id
            AND newer.generation_id <> gen.generation_id
            AND (
                newer.ingested_at > gen.ingested_at
                OR (
                    newer.ingested_at = gen.ingested_at
                    AND newer.generation_id > dead.generation_id
                )
            )
      )
      AND COALESCE((dead.payload ->> 'poison_recovery_attempts')::int, 0) < $2
    ORDER BY dead.work_item_id
    LIMIT $3
),
poison_locked AS (
    SELECT locked.work_item_id
    FROM fact_work_items AS locked
    WHERE locked.work_item_id = ANY(ARRAY(SELECT work_item_id FROM poison))
      AND locked.status = 'dead_letter'
      AND COALESCE((locked.payload ->> 'poison_recovery_attempts')::int, 0) < $2
    ORDER BY locked.work_item_id COLLATE "C"
    FOR NO KEY UPDATE
)
UPDATE fact_work_items AS target
SET status = 'pending',
    container_image_identity_v2_authorized_status = CASE
        WHEN target.container_image_identity_v2_required THEN 'pending'
        ELSE ''
    END,
    container_image_identity_v3_authorized_status = CASE
        WHEN target.container_image_identity_v3_required THEN 'pending'
        ELSE ''
    END,
    lease_owner = NULL,
    claim_until = NULL,
    visible_at = $1,
    next_attempt_at = NULL,
    failure_class = NULL,
    failure_message = NULL,
    failure_details = NULL,
    attempt_count = CASE
        WHEN target.stage = 'projector' THEN GREATEST(target.attempt_count, 1)
        ELSE target.attempt_count
    END,
    payload = jsonb_set(
        COALESCE(target.payload, '{}'::jsonb),
        '{poison_recovery_attempts}',
        to_jsonb(
            LEAST(COALESCE((target.payload ->> 'poison_recovery_attempts')::int, 0) + 1, $2)
        )
    ),
    updated_at = $1,
    reopened_at = $1,
    last_requeue_reason = ` + queuestore.RequeueReasonStageGatedValue(
	telemetry.RequeueReasonRecoveryRedrive.SQLLiteral(), "target.") + `,
    ` + queuestore.ReplayRetryAllowanceAssignments + `
WHERE target.work_item_id = ANY(ARRAY(SELECT work_item_id FROM poison_locked))
  -- Write-time re-verify (#4464-grade guard): only a row still genuinely
  -- dead_letter at UPDATE time is re-driven. A concurrent reclaim that moved
  -- the row to claimed/running/pending/succeeded/etc. between the poison CTE's
  -- snapshot and this UPDATE's row lock must not be clobbered back to pending.
  AND target.status = 'dead_letter'
RETURNING target.work_item_id, target.scope_id, target.generation_id,
    -- The stage rides RETURNING so the caller counts reducer requeues
    -- separately: projector rows keep NULL by the stage gate and are not
    -- counted (#11615).
    target.stage
`
