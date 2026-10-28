-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #9320: a reopen marker for shared_projection_intents, the counterpart of
-- fact_work_items.reopened_at (migration 088).
--
-- Two writers reopen a completed intent by clearing completed_at: the admin
-- reopen (query/admin/store reopenIntentsQuery) and the rebuild reset
-- (rebuild/reset reopenSharedIntentsTemplate). Both keep created_at, because
-- created_at is the intent's acceptance epoch for the RUNS_ON readiness fence
-- and its pending order key (#7323), so the status shared-projection backlog,
-- which measured age from MIN(created_at), reported a reopened intent as old
-- as its first emission. Both writers now stamp reopened_at, and the backlog
-- reads COALESCE(reopened_at, created_at), the #9292 definition.
--
-- A nullable column with no default is a catalog-only change: no table
-- rewrite, and the ACCESS EXCLUSIVE lock is held only for the catalog update.
-- Existing rows read NULL and keep aging from created_at. Keep this the only
-- statement in the file.
ALTER TABLE shared_projection_intents
    ADD COLUMN IF NOT EXISTS reopened_at TIMESTAMPTZ NULL;
