-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #8803: the collector terminal-failure status read runs on every status
-- scrape. Per enabled collector instance it reads the newest failed_terminal
-- work item updated inside that instance's window (3 x scan_interval, 30
-- minute floor) that no later completed item of the same instance and scope
-- superseded, ordered by updated_at DESC, work_item_id ASC. No existing index
-- serves that order: workflow_work_items_claimable_idx carries status and
-- visible_at between the instance and updated_at, so it cannot return one
-- instance's terminal rows newest first without reading and sorting them, and
-- the scope and run indexes lack the instance prefix. On PostgreSQL 18.6 with
-- 542,880 work items, one instance holding 276,480 failed_terminal rows (40
-- targets on a 15 minute cycle for 72 days), the windowed read took 83.6 ms
-- warm with a hash anti join that spilled 623 temp blocks; with this index
-- it took 1.9 ms warm, 794 buffers, no sort and no spill. The concurrent
-- build on that table took 1.5 s. See
-- docs/internal/evidence/8803-collector-terminal-failure-readiness.md.
--
-- Write tax: entries exist only for failed_terminal rows. Every work-item
-- UPDATE is already non-HOT because workflow_work_items_claimable_idx carries
-- status, visible_at, and updated_at, so this index adds one entry write on
-- the claimed -> failed_terminal transition and nothing on claims,
-- heartbeats, or completions. work_item_id is the last column so the read's
-- tiebreak is served by the index instead of an incremental sort.
--
-- Migration-only, like migration 156: a plain twin in the workflow control
-- bootstrap SQL would lock the hot queue table at coordinator start, and a
-- concurrent twin cannot run inside the bootstrap transaction. Keep this the
-- only statement in the file. PostgreSQL rejects a concurrent build inside a
-- transaction block, the migration coordinator runs a sole concurrent
-- definition in autocommit mode without the bootstrap lock_timeout (#7004),
-- and it drops an invalid same-name index before retrying a failed build.
CREATE INDEX CONCURRENTLY IF NOT EXISTS workflow_work_items_failed_terminal_instance_updated_idx
    ON workflow_work_items (collector_kind, collector_instance_id, updated_at DESC, work_item_id ASC)
    WHERE status = 'failed_terminal';
