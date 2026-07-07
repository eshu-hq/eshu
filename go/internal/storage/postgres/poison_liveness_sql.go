// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// countPoisonDeadLettersQuery detects the dead-letter/poison class the #4727
// projector-claimer fix and the generation-liveness sweep do not reach: a
// fact_work_items row whose status is 'dead_letter' with no strictly-newer
// scope_generations row for the same scope. dead_letter is a terminal status
// (never re-claimed by the normal claim path), so once a scope's newest
// generation lands in this state the scope can never self-heal without an
// operator or this bounded arm.
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
const countPoisonDeadLettersQuery = `
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

// recoverPoisonDeadLettersQuery bounds-recovers the poison dead-letter class by
// re-enqueuing a fresh pending attempt for each row selected by
// countPoisonDeadLettersQuery's same predicate, gated by a per-item
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
// each candidate row is an independent lock target — no cross-row lock
// ordering is introduced.
//
// Parameter order:
//
//	$1 now              (updated_at / visible_at stamp)
//	$2 max recover attempts (poison_recovery_attempts budget ceiling)
//	$3 batch limit       (max rows re-driven per sweep)
const recoverPoisonDeadLettersQuery = `
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
)
UPDATE fact_work_items AS target
SET status = 'pending',
    lease_owner = NULL,
    claim_until = NULL,
    visible_at = $1,
    next_attempt_at = NULL,
    failure_class = NULL,
    failure_message = NULL,
    failure_details = NULL,
    payload = jsonb_set(
        COALESCE(target.payload, '{}'::jsonb),
        '{poison_recovery_attempts}',
        to_jsonb(
            LEAST(COALESCE((target.payload ->> 'poison_recovery_attempts')::int, 0) + 1, $2)
        )
    ),
    updated_at = $1
FROM poison
WHERE target.work_item_id = poison.work_item_id
  -- Write-time re-verify (#4464-grade guard): only a row still genuinely
  -- dead_letter at UPDATE time is re-driven. A concurrent reclaim that moved
  -- the row to claimed/running/pending/succeeded/etc. between the poison CTE's
  -- snapshot and this UPDATE's row lock must not be clobbered back to pending.
  AND target.status = 'dead_letter'
RETURNING target.work_item_id, target.scope_id, target.generation_id
`
