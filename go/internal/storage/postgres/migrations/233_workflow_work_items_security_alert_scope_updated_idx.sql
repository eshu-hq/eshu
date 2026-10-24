-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #8840: the security-alert empty-answer collection-state read finds, per
-- target scope, the newest security_alert work item ordered by updated_at
-- DESC, work_item_id ASC. The only index with the (collector_kind, scope_id)
-- prefix, workflow_work_items_collector_scope_generation_updated_idx, puts
-- generation_id (a hash) before updated_at, so the read scanned every work
-- item the target ever had and top-1 sorted them. Its cost grew with the
-- target's history. This index matches the read's ORDER BY, so the lateral
-- becomes an index-only probe that reads one entry per target.
--
-- It is partial on collector_kind = 'security_alert', which the read states
-- as a literal. The work-item retention prune's "newer row" probe matches
-- collector_kind against the outer row, so the planner cannot prove this
-- predicate there and keeps the prune on its existing plan. A full
-- (collector_kind, scope_id, updated_at DESC) key made the prune walk every
-- newer row whose status differed: 241 ms to 3,074 ms per 2,000-row batch on
-- a seed where each target failed for its older half and then recovered.
-- Only security_alert rows pay the extra B-tree entry on insert and update.
--
-- On PostgreSQL 18.6 with 561,120 work items, one target holding 120,000
-- security_alert items, the anchored read went from a scan and top-N sort of
-- the target's history (tens of milliseconds, thousands of buffers) to an
-- index-only probe of about 10 buffers. See
-- docs/internal/evidence/8840-security-alert-latest-work-item-index.md.
--
-- Migration-only, like migrations 156 and 211: a plain twin in the workflow
-- control bootstrap SQL would lock the hot queue table at coordinator start,
-- and a concurrent twin cannot run inside the bootstrap transaction. Keep
-- this the only statement in the file. PostgreSQL rejects a concurrent build
-- inside a transaction block. The migration coordinator runs a sole
-- concurrent definition in autocommit mode without the bootstrap
-- lock_timeout (#7004), and it drops an invalid same-name index before it
-- retries a failed build.
CREATE INDEX CONCURRENTLY IF NOT EXISTS workflow_work_items_security_alert_scope_updated_idx
    ON workflow_work_items (scope_id, updated_at DESC, work_item_id ASC)
    WHERE collector_kind = 'security_alert';
