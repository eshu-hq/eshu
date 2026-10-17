-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #8283: the deferred-maintenance pass reopened every succeeded
-- deployable_unit_correlation item on every shard drain, although almost none
-- of them read an input that changed. This side table records, per work item,
-- the repository ids its fused fenced read used and a fingerprint of the
-- relationship generations that read saw (its own generation plus every active
-- generation with a resolved row touching those repositories). The reopen
-- listing recomputes the fingerprint with the same SQL expression and skips an
-- item whose recorded value still matches. A missing row means reopen.
--
-- The row is written by the deployable_unit_correlation handler before the
-- work item is acknowledged, as an idempotent upsert on work_item_id. The
-- foreign key cascades, so the reset path that deletes succeeded reducer work
-- items by (scope, generation) removes the rows with them; each success
-- overwrites its row, so nothing else needs a sweeper.
--
-- A new table: CREATE TABLE IF NOT EXISTS, no backfill (a missing row reopens
-- the item once, which is the safe direction), and no secondary index (the
-- listing joins on the primary key). Creating the foreign key takes a brief
-- SHARE ROW EXCLUSIVE lock on fact_work_items, bounded by the runner's
-- lock_timeout and retry (schema.go), as in migration 006's audit table.
CREATE TABLE IF NOT EXISTS reducer_work_input_fingerprints (
    work_item_id TEXT PRIMARY KEY REFERENCES fact_work_items(work_item_id) ON DELETE CASCADE,
    domain TEXT NOT NULL,
    repo_ids TEXT[] NOT NULL,
    fingerprint TEXT NOT NULL,
    recorded_at TIMESTAMPTZ NOT NULL
);
