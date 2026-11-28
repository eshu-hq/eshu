-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #11615: label each requeued reducer item with its reopen reason. One
-- nullable TEXT column on the shared queue row, written inside every
-- existing guarded requeue UPDATE alongside reopened_at. No default, no
-- CHECK, no index, no backfill: a catalog-only change, so ADD COLUMN takes
-- no rewrite and the ACCESS EXCLUSIVE lock is held only for the catalog
-- update under the migrator's lock_timeout/retry, exactly the migration-245
-- pattern (migrations/README.md).
--
-- NULL is legal only for never-requeued rows (reopened_at IS NULL): the
-- acceptance "an unset reason fails the gate" is enforced at the writer
-- level by the #11615 source guard
-- (requeue_reason_writers_guard_test.go), not by NOT NULL, which would force
-- a fake backfill value onto rows whose true reason is unknowable. A
-- backfill is forbidden: it would be bulk DML in a DDL file (refused by
-- migration_lock_shape.py), would churn WAL on the hottest queue table, and
-- could not be truthful. Keep this the only statement in the file.
ALTER TABLE fact_work_items
    ADD COLUMN IF NOT EXISTS last_requeue_reason TEXT NULL;
