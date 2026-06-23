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

// recoverWedgedActiveGenerationsQuery durably re-drives generations that are
// active but have made no forward progress past canonical-nodes-committed for
// longer than the activation deadline.
//
// A wedged generation is one whose projector work already succeeded (status
// active, activated_at set) but whose downstream reducer phases never advanced,
// and for which no newer same-scope generation exists to supersede it through
// the projector path. Such a generation sits active forever today. This query
// re-enqueues source-local projector work for it, which re-publishes the
// canonical-nodes-committed readiness phase and re-triggers the downstream
// reducer consumers over the existing facts (no re-clone). The re-drive budget
// lives in the work item payload (liveness_recovery_attempts) and is bounded by
// $2 so a poison scope cannot loop forever; once the budget is exhausted the
// generation is left active for an operator to inspect via the recovery
// endpoint or a manual replay.
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
      AND COALESCE(
          (
              SELECT (existing.payload ->> 'liveness_recovery_attempts')::int
              FROM fact_work_items AS existing
              WHERE existing.stage = 'projector'
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
// Parameter order:
//
//	$1 aging boundary   (now minus half the activation deadline)
//	$2 stuck boundary   (now minus the activation deadline)
const countActiveGenerationsByAgeQuery = `
SELECT
    CASE
        WHEN generation.activated_at IS NULL THEN 'fresh'
        WHEN generation.activated_at < $2 THEN 'stuck'
        WHEN generation.activated_at < $1 THEN 'aging'
        ELSE 'fresh'
    END AS age_bucket,
    COUNT(*) AS generation_count
FROM scope_generations AS generation
WHERE generation.status = 'active'
GROUP BY age_bucket
`
