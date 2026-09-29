-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #7319: a delta generation records the commit its diff was taken from. The
-- projector compares it with the scope's active generation commit before it
-- projects and again inside Ack, and refuses a delta whose baseline is no
-- longer the active commit. NULL means a full generation or a delta written
-- before this column existed.
--
-- Nullable and without a default, so ADD COLUMN is a catalog-only change with
-- no table rewrite; its ACCESS EXCLUSIVE lock is held only for that catalog
-- update and bounded by the runner's lock_timeout and retry (schema.go), as in
-- migration 126. No index (the column is only read by primary key), no backfill
-- (an activated row cannot be fenced after the fact) and no CHECK (legacy delta
-- rows would violate it). Keep this the only statement in the file.
ALTER TABLE scope_generations
    ADD COLUMN IF NOT EXISTS delta_baseline_commit_sha TEXT NULL;
