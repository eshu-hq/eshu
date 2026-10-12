-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- 171_drop_content_files_language_repo_idx.sql
--
-- #7728: drops the (language, repo_id) definition of
-- content_files_language_repo_idx so migration 172 can rebuild the SAME NAME
-- carrying indexed_at for the by-language COUNT(*)/MAX(indexed_at) rollup.
--
-- Why the name is reused rather than replaced. Migration 004 is shipped and
-- immutable (migration_checksum_manifest_test.go), and it creates this name
-- with the old two-column definition under IF NOT EXISTS, which matches on
-- NAME only. Any database that replays 004 -- a fresh database, or an
-- existing database without eshu_schema_migrations receipts replaying the
-- tree once -- would recreate the old definition if the name were free, and
-- the covering read would silently lose its INCLUDE column on those
-- installs. With the name reused, 004 is a no-op wherever the covering
-- definition exists (the 128/129 shape for #6475).
--
-- Guarded, so it converges once: the drop fires only while the index under
-- this name is the pre-#7728 definition (its indexdef lacks indexed_at). A
-- fresh database drops 004's just-built index on a nearly empty table; an
-- upgrading database drops the old definition once; every later replay sees
-- indexed_at in the definition and does nothing, so no replay drops or
-- rebuilds the index. TestContentFilesLanguageCoveringIndexReplayConvergesLive
-- proves that on a populated store.
--
-- Plain DROP INDEX, not a concurrent one: a DO block runs in a transaction,
-- and a concurrent build cannot. The drop itself takes only a brief ACCESS
-- EXCLUSIVE lock and rewrites nothing; content writes pause momentarily and
-- proceed. Between this drop and 172's build completing, by-language reads
-- fall back to a sequential scan -- reads stay correct, only the plan
-- degrades transiently during the deploy. Replay-safe: the guard, not
-- IF EXISTS, decides, so an interrupted 172 build's INVALID leftover is
-- cleared by the schema apply path before retrying (as with 137).
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM pg_indexes
        WHERE schemaname = current_schema()
          AND tablename = 'content_files'
          AND indexname = 'content_files_language_repo_idx'
          AND indexdef NOT LIKE '%indexed_at%'
    ) THEN
        DROP INDEX content_files_language_repo_idx;
    END IF;
END
$$;
