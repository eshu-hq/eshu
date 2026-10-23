-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #8780: a cumulative delta generation records the commit of the newest
-- activated full generation its changed set was diffed from: the union base
-- the projector overlays it on. delta_baseline_commit_sha keeps its #7319
-- meaning (the active commit when the delta was diffed, which the fence
-- compares at Ack). Readers resolve the base at
-- COALESCE(delta_union_base_commit_sha, delta_baseline_commit_sha): before this
-- column only a delta diffed from an active full could activate, so a legacy
-- delta's baseline is its base full's commit. NULL means a full generation or
-- a delta written before this column existed.
--
-- Nullable and without a default, so ADD COLUMN is a catalog-only change with
-- no table rewrite; its ACCESS EXCLUSIVE lock is held only for that catalog
-- update and bounded by the runner's lock_timeout and retry (schema.go), as in
-- migration 148. No index (the column is only read by primary key), no
-- backfill, and no CHECK (legacy delta rows would violate it). Keep this the
-- only statement in the file.
ALTER TABLE scope_generations
    ADD COLUMN IF NOT EXISTS delta_union_base_commit_sha TEXT NULL;
