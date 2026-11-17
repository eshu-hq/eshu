-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #10802: the run-age bound expiry pass (ExpireStaleOpenRuns, #10791) runs
-- every active-maintenance tick. Its candidate walk reads pending or claimed
-- work items newest first with LIMIT 100, then joins each candidate's run.
-- No existing index serves that order: workflow_work_items_run_idx leads
-- with run_id, and the claimable index carries status and visible_at between
-- the instance and updated_at. On a backlog-shaped fixture (200,000
-- fresh-run pending items, 100 stale items) the walk sequentially scanned
-- the work-item backlog on every tick, so a growing backlog slowed every
-- tick. With this index the walk scans open items newest first, probes each
-- candidate's run by primary key, and stops at the limit, with no sequential
-- scan of the work-item table; see the EXPLAIN pin in
-- TestExpireStaleOpenRunsPlanStaysIndexServed and
-- docs/internal/evidence/10791-run-age-bound.md. Runs and claims keep
-- sequential scans at fixture scale: those tables stay small (non-terminal
-- runs are bounded by the #8829 retention window, claims cover only
-- selected items), so their scans are the planner's legitimate small-table
-- choice, not a scaling hazard.
--
-- Write tax: entries exist only for pending or claimed items. A claimed,
-- completed, or terminal-failed row leaves the index. Every heartbeat is
-- already non-HOT because workflow_work_items_claimable_idx carries status,
-- visible_at, and updated_at, so this index adds one entry write per
-- heartbeat alongside the existing one, plus one per status transition into
-- or out of the open set. work_item_id is the last column so the ORDER BY
-- tiebreak is served by the index instead of an incremental sort.
--
-- Migration-only, like migration 211: a plain twin in the workflow control
-- bootstrap SQL would lock the hot queue table at coordinator start, and a
-- concurrent twin cannot run inside the bootstrap transaction. Keep this the
-- only statement in the file. PostgreSQL rejects a concurrent build inside a
-- transaction block, the migration coordinator runs a sole concurrent
-- definition in autocommit mode without the bootstrap lock_timeout (#7004),
-- and it drops an invalid same-name index before retrying a failed build.
CREATE INDEX CONCURRENTLY IF NOT EXISTS workflow_items_open_updated_idx
    ON workflow_work_items (updated_at DESC, work_item_id ASC)
    WHERE status IN ('pending', 'claimed');
