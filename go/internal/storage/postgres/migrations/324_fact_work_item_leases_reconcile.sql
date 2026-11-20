-- 324_fact_work_item_leases_reconcile.sql: reconcile backfill-raced
-- mirror rows for #10879 phase 1.
--
-- The 323 snapshot can copy a lease whose main row terminalizes mid-scan,
-- orphaning a live-looking lease row for a terminal main row and wedging
-- its conflict key through the lease unique (no sweeper exists; terminal
-- clears only touch their own work item). This statement removes every
-- mirror row whose main row is missing or no longer live. The locked CTE
-- waits out in-flight terminals and rechecks their newest version, closing
-- the during-delete window for dual-write writers. Lock order stays
-- main-then-lease, the same as every queue writer. It matches the orphan
-- predicate the live tests pin, preserves live rows, and is idempotent, so
-- replays (including on databases that already applied 323) stay safe.
-- DML-only per #7961: no table-locking DDL shares this file.
WITH locked AS (
    SELECT w.work_item_id, w.stage, w.status
    FROM fact_work_items AS w
    JOIN fact_work_item_leases AS l USING (work_item_id)
    FOR UPDATE OF w
)
DELETE FROM fact_work_item_leases AS lease
WHERE NOT EXISTS (
    SELECT 1 FROM locked
    WHERE locked.work_item_id = lease.work_item_id
      AND locked.stage = 'reducer'
      AND locked.status IN ('claimed', 'running')
);
