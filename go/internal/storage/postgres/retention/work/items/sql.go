// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package items

// pruneBatchQuery examines one primary-key window of workflow_work_items and
// deletes the rows in it that retention may remove. Arguments: $1 the
// exclusive work_item_id cursor (the empty string starts at the head), $2 the
// window size, $3 the updated_at cutoff.
//
// It returns one row: the window's last work_item_id (NULL for an empty
// window), the rows the window examined, and the deleted completed and
// failed_terminal counts. A window shorter than $2 means the walk reached the
// table end.
//
// Eligibility (all must hold):
//   - status completed or failed_terminal: claimed, pending, failed_retryable
//     and expired rows are never touched;
//   - updated_at older than the cutoff;
//   - the run is closed (complete or failed): run reconciliation counts the
//     items of open runs, and removing one would change the derived status;
//   - not the scope's active generation: collector readiness evidence joins
//     ingestion_scopes.active_generation_id to the work item;
//   - a newer row with the same status exists for the same (collector_kind,
//     collector_instance_id, scope_id). That one probe keeps three rows per
//     target: the newest completed row, which the status registry's
//     last_completed_at reads; the newest failed_terminal row, which failure
//     reporting reads; and the newest row of any status, because it is the
//     newest of its own status, so the latest-row reads keep their answer.
//
// "Newer" is the strict order (updated_at, work_item_id), so exactly one row
// per target and status is newest even when timestamps tie.
//
// Plan shape (#8829 EXPLAIN on 300k rows, 50 scopes x 6,000 cycles, PG 18.6):
// the window is an index-only primary-key range scan; each windowed row is a
// primary-key probe whose newer-row check probes
// workflow_work_items_collector_scope_generation_updated_idx once per row,
// with the (updated_at, work_item_id) comparison as an index condition and the
// status as a heap filter. 11 to 63 ms and 10k to 45k shared buffers per
// 1,000-row batch across two id layouts, no slower than the earlier two-probe
// statement in any measured window (median of three runs). The OFFSET 0
// fence keeps that check per-row: without it the planner turned it into a
// hash semi-join over a full sequential scan (127 ms with a temp spill).
// The worst case is a target whose newest completed row is older than the
// cutoff and followed by many failures: its probe heap-checks every newer
// row of the target, so it costs at most the target's row count once per
// walk.
//
// Locking: FOR UPDATE OF item SKIP LOCKED. Lifecycle writers (claim,
// heartbeat, complete, fail, release, reap) only lock rows in status claimed
// or pending, which this statement never selects, so the row sets are
// disjoint; SKIP LOCKED means a batch never waits on an item row anyway. The
// ON DELETE CASCADE into workflow_claims locks only the finished claims of the
// deleted items, which no other writer locks. The row predicates sit on the
// locked relation itself, so EvalPlanQual rechecks them if a row changed
// between snapshot and lock.
const pruneBatchQuery = `
WITH scan_window AS MATERIALIZED (
    SELECT work_item_id
    FROM workflow_work_items
    WHERE work_item_id > $1
    ORDER BY work_item_id
    LIMIT $2
),
candidates AS (
    SELECT item.work_item_id
    FROM scan_window
    JOIN workflow_work_items AS item
      ON item.work_item_id = scan_window.work_item_id
    WHERE item.status IN ('completed', 'failed_terminal')
      AND item.updated_at < $3
      AND EXISTS (
          SELECT 1
          FROM workflow_runs AS run
          WHERE run.run_id = item.run_id
            AND run.status IN ('complete', 'failed')
      )
      AND NOT EXISTS (
          SELECT 1
          FROM ingestion_scopes AS scope
          WHERE scope.scope_id = item.scope_id
            AND scope.active_generation_id = item.generation_id
      )
      AND EXISTS (
          SELECT 1
          FROM workflow_work_items AS newer
          WHERE newer.collector_kind = item.collector_kind
            AND newer.scope_id = item.scope_id
            AND newer.collector_instance_id = item.collector_instance_id
            AND newer.status = item.status
            AND (newer.updated_at, newer.work_item_id) > (item.updated_at, item.work_item_id)
          OFFSET 0
      )
    FOR UPDATE OF item SKIP LOCKED
),
deleted AS (
    DELETE FROM workflow_work_items AS doomed
    USING candidates
    WHERE doomed.work_item_id = candidates.work_item_id
    RETURNING doomed.status
)
SELECT
    (SELECT MAX(work_item_id) FROM scan_window) AS next_cursor,
    (SELECT COUNT(*) FROM scan_window) AS scanned,
    COUNT(*) FILTER (WHERE status = 'completed') AS deleted_completed,
    COUNT(*) FILTER (WHERE status = 'failed_terminal') AS deleted_failed_terminal
FROM deleted
`

// setBatchTimeoutsQuery sets the batch transaction's lock and statement
// timeouts. set_config with is_local true scopes both to the transaction, so a
// pooled connection never carries them into another caller's work.
// Arguments: $1 lock_timeout, $2 statement_timeout, both as Postgres
// duration strings such as '2000ms'.
const setBatchTimeoutsQuery = `
SELECT set_config('lock_timeout', $1, true), set_config('statement_timeout', $2, true)
`

// statementTimeoutSetting bounds one batch statement. The measured batch is
// tens of milliseconds; 30 s stops a regressed plan from holding its row
// locks and snapshot for minutes.
const statementTimeoutSetting = "30000ms"

// tableSizeQuery reads workflow_work_items' total relation size and the
// planner's live-row estimate from the catalog. Both are cheap metadata reads
// that scan no table rows; reltuples is -1 before the first ANALYZE, which
// GREATEST clamps to 0.
const tableSizeQuery = `
SELECT
    pg_total_relation_size(class.oid) AS total_bytes,
    GREATEST(class.reltuples, 0)::bigint AS estimated_rows
FROM pg_class AS class
WHERE class.oid = 'workflow_work_items'::regclass
`
