-- 326_fact_work_item_leases_11081_cleanup.sql: delete leftover lease-mirror
-- rows for #11081.
--
-- Claim-time stale-clear (reducerQueueLeaseMirrorCTE) heals every key a new
-- build claims, but a conflict key no new build ever reclaims keeps its
-- orphaned row (and its unique wedge) until an operator acts. This statement
-- removes every mirror row the main table disowns under the claim-time
-- liveness definition shared with the claim CTEs, heartbeat, and orphan
-- gauge: a lease row L (stage='reducer') is LIVE iff its main row M matches
-- work_item_id AND stage='reducer' AND status IN ('claimed','running') AND
-- claim_until>NOW AND lease_owner AND last_attempt_at; STALE = NOT LIVE.
-- NOW is clock_timestamp() here, matching the terminal statements and the
-- precheck probes (never the app clock the claim passes as $1).
-- Reducer-only by construction: both the locked CTE and the DELETE filter
-- on stage='reducer', so projector-stage pairs reserved for phase-1b (322)
-- are never locked nor deleted.
--
-- The predicate is stricter than 324's status-only reconcile on purpose:
-- expired or triple-mismatched mirrors are recreated by new-build
-- heartbeat/reclaim, and old builds ignore the lease table, so deleting
-- them is safe. It preserves the 324 locked-CTE shape (the locked CTE
-- waits out in-flight terminals and rechecks their newest version, closing
-- the during-delete window) and the main-then-lease lock order the queue
-- writers use. Idempotent: the locked recheck and the NOT EXISTS make a
-- replay a no-op. The liveness test is a NOT EXISTS over the locked rows,
-- the same shape the claim CTEs and the orphan gauge use: a main row whose
-- owner, claim_until, or last_attempt_at is NULL is not live, so its lease
-- row goes too (a NOT (a AND b ...) form evaluates NULL there and keeps it).
-- DML-only per #7961: no table-locking DDL shares this file. Refs #11081.
WITH locked AS (
    SELECT w.work_item_id, w.stage, w.status, w.claim_until,
        w.lease_owner, w.last_attempt_at
    FROM fact_work_items AS w
    JOIN fact_work_item_leases AS l USING (work_item_id)
    WHERE w.stage = 'reducer'
      AND l.stage = 'reducer'
    FOR UPDATE OF w
)
DELETE FROM fact_work_item_leases AS lease
USING locked
WHERE lease.stage = 'reducer'
  AND locked.work_item_id = lease.work_item_id
  AND NOT EXISTS (
      SELECT 1
      FROM locked AS live
      WHERE live.work_item_id = lease.work_item_id
        AND live.stage = 'reducer'
        AND live.status IN ('claimed', 'running')
        AND live.claim_until > clock_timestamp()
        AND live.lease_owner = lease.lease_owner
        AND live.last_attempt_at = lease.last_attempt_at
  );
