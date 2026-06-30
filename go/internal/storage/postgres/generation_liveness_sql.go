// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// supersedeOrphanedActiveGenerationsQuery retires stale older active
// generations for a scope once a newer same-scope generation is already
// authoritative.
//
// The projector ack/claim paths normally supersede an older active generation
// when the newer generation's projector work commits (see projector_queue_sql.go).
// This query is the standalone defense-in-depth path the liveness sweep runs:
// if two active rows for one scope ever slip past the unique-active index, or a
// newer generation became authoritative without driving the older one to a
// terminal state, the older active is superseded here. The conflict domain is
// scope_id; the write only ever demotes a strictly-older active, never the
// newest one, so it is idempotent under concurrent reducer workers and safe to
// repeat (a second run finds no remaining stale active for the scope).
//
// Parameter order:
//
//	$1 now              (superseded_at stamp)
//	$2 batch limit      (max generations retired per sweep)
const supersedeOrphanedActiveGenerationsQuery = `
WITH stale_active AS (
    SELECT stale.generation_id
    FROM scope_generations AS stale
    WHERE stale.status = 'active'
      AND EXISTS (
          SELECT 1
          FROM scope_generations AS newer
          WHERE newer.scope_id = stale.scope_id
            AND newer.generation_id <> stale.generation_id
            AND newer.status = 'active'
            AND (
                newer.ingested_at > stale.ingested_at
                OR (
                    newer.ingested_at = stale.ingested_at
                    AND newer.generation_id > stale.generation_id
                )
            )
      )
    ORDER BY stale.scope_id, stale.generation_id
    LIMIT $2
)
UPDATE scope_generations AS generation
SET status = 'superseded',
    superseded_at = $1
FROM stale_active
WHERE generation.generation_id = stale_active.generation_id
  AND generation.status = 'active'
RETURNING generation.scope_id, generation.generation_id
`

// recoverWedgedActiveGenerationsQuery durably re-drives active generations that
// still have downstream blockage after the activation deadline, but only when no
// source-local projector work is already in flight or succeeded.
//
// A wedged generation is one with outstanding shared-projection intent after
// reducer fact-work drained, but without source-local projector work already
// pending, claimed, running, retrying, or succeeded. Age alone is NOT enough: a
// healthy quiet scope normally stays active and projected (the projected
// baseline is "has been active", see generation_projected_commit.go) with no
// outstanding work, and re-driving those on every poll would burn the liveness
// budget and raise false alarms. Downstream shared-projection backlog is also
// not enough: a succeeded source-local projector row means canonical graph write
// and readiness publication completed before ack, so reopening source_local only
// duplicates work while the real backlog is elsewhere. During a large bootstrap,
// shared intents may legitimately sit behind a deep reducer backlog or readiness
// gate; re-driving source-local projection at that point creates duplicate
// projector work after the bootstrap projector has already drained. This query
// therefore re-enqueues source-local projector work only for genuinely blocked
// generations, which re-publishes the canonical-nodes-committed readiness phase
// and re-triggers the downstream consumers over the existing facts (no
// re-clone). The re-drive budget lives in the work item payload
// (liveness_recovery_attempts) and is bounded by $2 so a poison scope cannot
// loop forever; once the budget is exhausted the generation is left active for
// an operator to inspect via the recovery endpoint or a manual replay.
//
// The conflict domain is scope_id. ON CONFLICT DO UPDATE makes the re-enqueue
// idempotent under concurrent sweeps and retries: a duplicate re-drive only
// refreshes visibility and increments the bounded budget rather than creating a
// second active row or a second work item.
//
// Parameter order:
//
//	$1 activation deadline   (now minus the activation-deadline window)
//	$2 max recover attempts  (re-drive budget ceiling)
//	$3 batch limit           (max generations re-driven per sweep)
//	$4 now                   (work item visibility/update stamp)
const recoverWedgedActiveGenerationsQuery = `
WITH wedged AS (
    SELECT
        generation.scope_id,
        generation.generation_id
    FROM scope_generations AS generation
    JOIN ingestion_scopes AS scope
      ON scope.scope_id = generation.scope_id
    WHERE generation.status = 'active'
      AND generation.activated_at IS NOT NULL
      AND generation.activated_at < $1
      AND scope.active_generation_id = generation.generation_id
      AND NOT EXISTS (
          SELECT 1
          FROM scope_generations AS newer
          WHERE newer.scope_id = generation.scope_id
            AND newer.generation_id <> generation.generation_id
            AND newer.status IN ('pending', 'active')
      )
      -- Downstream-blockage gate: only a generation with outstanding
      -- shared-projection work is wedged. Healthy quiet projected scopes have
      -- every intent completed and are excluded so they are never re-driven.
      AND EXISTS (
          SELECT 1
          FROM shared_projection_intents AS intent
          WHERE intent.generation_id = generation.generation_id
            AND intent.completed_at IS NULL
      )
      -- Normal reducer backlog is progress, not a wedged generation. Do not
      -- re-drive source-local projection until reducer fact-work for the same
      -- generation has drained; otherwise a full-corpus bootstrap can reopen
      -- source_local after the one-shot bootstrap projector has exited.
      AND NOT EXISTS (
          SELECT 1
          FROM fact_work_items AS reducer_work
          WHERE reducer_work.stage = 'reducer'
            AND reducer_work.scope_id = generation.scope_id
            AND reducer_work.generation_id = generation.generation_id
            AND reducer_work.status IN ('pending', 'claimed', 'running', 'retrying', 'failed', 'dead_letter')
      )
      -- A source-local projector row that is already pending or in progress is
      -- an in-flight recovery. A succeeded source-local projector row means the
      -- canonical graph write and phase publication completed before ack; any
      -- remaining shared_projection_intents backlog belongs to downstream
      -- shared projection, not source-local liveness recovery.
      AND NOT EXISTS (
          SELECT 1
          FROM fact_work_items AS projector_work
          WHERE projector_work.stage = 'projector'
            AND projector_work.domain = 'source_local'
            AND projector_work.scope_id = generation.scope_id
            AND projector_work.generation_id = generation.generation_id
            AND projector_work.status IN ('pending', 'claimed', 'running', 'retrying', 'succeeded')
      )
      AND COALESCE(
          (
              SELECT (existing.payload ->> 'liveness_recovery_attempts')::int
              FROM fact_work_items AS existing
              WHERE existing.stage = 'projector'
                AND existing.domain = 'source_local'
                AND existing.scope_id = generation.scope_id
                AND existing.generation_id = generation.generation_id
          ),
          0
      ) < $2
    ORDER BY generation.activated_at ASC, generation.generation_id ASC
    LIMIT $3
),
re_enqueued AS (
    INSERT INTO fact_work_items (
        work_item_id,
        scope_id,
        generation_id,
        stage,
        domain,
        status,
        attempt_count,
        lease_owner,
        claim_until,
        visible_at,
        last_attempt_at,
        next_attempt_at,
        failure_class,
        failure_message,
        failure_details,
        payload,
        created_at,
        updated_at
    )
    SELECT
        'projector_' || wedged.scope_id || '_' || wedged.generation_id,
        wedged.scope_id,
        wedged.generation_id,
        'projector',
        'source_local',
        'pending',
        0,
        NULL,
        NULL,
        $4,
        NULL,
        NULL,
        NULL,
        NULL,
        NULL,
        jsonb_build_object('liveness_recovery_attempts', 1),
        $4,
        $4
    FROM wedged
    ON CONFLICT (work_item_id) DO UPDATE
    SET status = 'pending',
        lease_owner = NULL,
        claim_until = NULL,
        visible_at = EXCLUDED.visible_at,
        next_attempt_at = NULL,
        failure_class = NULL,
        failure_message = NULL,
        failure_details = NULL,
        payload = jsonb_set(
            COALESCE(fact_work_items.payload, '{}'::jsonb),
            '{liveness_recovery_attempts}',
            to_jsonb(
                -- The wedged CTE already excludes generations at or past the budget
                -- ($2), so a re-driven generation is always below it here. Cap with
                -- LEAST as defense-in-depth so the durable counter can never exceed
                -- the ceiling even if the selection gate changes.
                LEAST(COALESCE((fact_work_items.payload ->> 'liveness_recovery_attempts')::int, 0) + 1, $2)
            )
        ),
        updated_at = EXCLUDED.updated_at
    RETURNING scope_id, generation_id
)
SELECT scope_id, generation_id FROM re_enqueued ORDER BY scope_id, generation_id
`

// countActiveGenerationsByAgeQuery buckets active generations by activation age
// so the liveness gauge and the stuck-generation alarm can report without an
// unbounded scan. The deadline boundary ($2) is the same window the recovery
// sweep uses, so "stuck" in the gauge means "eligible for recovery".
//
// The stuck bucket is the wedged-generation alarm and must match the recovery
// gate: a generation is only stuck when it has aged past the deadline AND has
// real downstream blockage (an outstanding shared_projection_intents row,
// completed_at IS NULL) after reducer fact-work for the same generation has
// drained, with no source-local projector row already in flight or succeeded. A
// healthy quiet projected scope that merely aged, a busy full-corpus bootstrap
// scope still moving through reducer work, or shared-projection backlog after a
// succeeded source-local projector ack is counted aging, never stuck, so the
// alarm does not fire on normal idle installations, reducer backlog, or
// downstream shared projection lag.
//
// Parameter order:
//
//	$1 aging boundary   (now minus half the activation deadline)
//	$2 stuck boundary   (now minus the activation deadline)
const countActiveGenerationsByAgeQuery = `
SELECT
    CASE
        WHEN generation.activated_at IS NULL THEN 'fresh'
        WHEN generation.activated_at < $2 AND EXISTS (
            SELECT 1
            FROM shared_projection_intents AS intent
            WHERE intent.generation_id = generation.generation_id
              AND intent.completed_at IS NULL
        ) AND NOT EXISTS (
            SELECT 1
            FROM fact_work_items AS reducer_work
            WHERE reducer_work.stage = 'reducer'
              AND reducer_work.scope_id = generation.scope_id
              AND reducer_work.generation_id = generation.generation_id
              AND reducer_work.status IN ('pending', 'claimed', 'running', 'retrying', 'failed', 'dead_letter')
        ) AND NOT EXISTS (
            SELECT 1
            FROM fact_work_items AS projector_work
            WHERE projector_work.stage = 'projector'
              AND projector_work.domain = 'source_local'
              AND projector_work.scope_id = generation.scope_id
              AND projector_work.generation_id = generation.generation_id
              AND projector_work.status IN ('pending', 'claimed', 'running', 'retrying', 'succeeded')
        ) THEN 'stuck'
        WHEN generation.activated_at < $1 THEN 'aging'
        ELSE 'fresh'
    END AS age_bucket,
    COUNT(*) AS generation_count
FROM scope_generations AS generation
WHERE generation.status = 'active'
GROUP BY age_bucket
`
