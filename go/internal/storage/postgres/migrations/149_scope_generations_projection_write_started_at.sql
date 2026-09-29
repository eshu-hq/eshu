-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #7389: a generation records when a projector attempt last reached the point
-- of writing the canonical graph and content store. The projector sets it after
-- LoadFacts and before its first write, fenced on its own claim, to the latest
-- write start (it never moves backwards). The heartbeat supersede never supersedes a generation whose
-- marker is set (it runs to Ack instead), and the git collector reads it to
-- find a superseded or failed generation that wrote the graph and never
-- activated, which forces its next sync to a full snapshot. NULL means the
-- projector has not started writing this generation, or the row predates this
-- column.
--
-- Nullable and without a default, so ADD COLUMN is a catalog-only change with
-- no table rewrite; its ACCESS EXCLUSIVE lock is held only for that catalog
-- update and bounded by the runner's lock_timeout and retry (schema.go), as in
-- migration 148. No index (the heartbeat and the marker read it by primary key,
-- and the collector probe reads one scope's rows through the existing scope
-- indexes), no backfill (an old row cannot say whether it wrote the graph) and
-- no CHECK. Keep this the only statement in the file.
ALTER TABLE scope_generations
    ADD COLUMN IF NOT EXISTS projection_write_started_at TIMESTAMPTZ NULL;
