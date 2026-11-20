-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #11021: the 2e purge sweep deletes parked waits for retired scopes
-- (purgeDeleteWaitingItemsQuery), and without an index for its scope_id
-- predicate it would read every parked wait of every live scope on each
-- purge. The wake index leads on (waits_for_kind, waits_for_key) and the
-- reason index on (failure_class, first_deferred_at), so neither serves a
-- scope probe; scope_id leads here so the delete probes the index directly.
-- The waiting set stays small (wake and audit clear live waits), so this is
-- one narrow btree, not a covering index: no INCLUDE columns, no predicate.
--
-- CONCURRENTLY so bootstrap never blocks writers; IF NOT EXISTS for rerun
-- safety, matching the index-migration precedent. One statement per file so the
-- migration coordinator can run it outside a transaction
-- (coordination.IsSoleConcurrentIndexStatement). A NEW migration file per
-- issue #7002: never edit an applied migration.
CREATE INDEX CONCURRENTLY IF NOT EXISTS reducer_waiting_items_scope_idx
    ON reducer_waiting_items (scope_id);
