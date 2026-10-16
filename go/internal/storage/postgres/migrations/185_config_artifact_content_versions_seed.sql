-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- Version-0 seed for the config-artifact content-version fence (#7126
-- PR3b-1b, #8155).
--
-- One config_artifact_content_versions row with version 0 for each
-- repository that already has content, as migration 169 seeds from
-- content_files. The PR3b-2 walker then walks O(repositories) version rows
-- instead of scanning content_files.
--
-- Why a separate file from the migration 184 triggers. The #7961 lock-shape
-- gate refuses table-locking DDL (CREATE TRIGGER) and bulk DML in one file,
-- because the migrator sends a file as one message and PostgreSQL would hold
-- the trigger's SHARE ROW EXCLUSIVE lock on content_files through the scan.
-- This file takes ACCESS SHARE on content_files only, so content reads and
-- writes run during the seed. The triggers commit first, so every content
-- write from then on bumps through them at its commit: a write that commits
-- before this scan creates its row at version 1 and the seed leaves it alone
-- (ON CONFLICT DO NOTHING, after waiting out an uncommitted insert of the
-- same row); a write that commits after the seed bumps the seeded row. The
-- seed inserts in repo_id order, the order every fence commit takes version
-- rows in, so the two cannot deadlock (review finding F2). No reader or derive exists
-- before PR3b-2, which must deploy after this file, so no state row can trust
-- a version this ordering produces.
--
-- bumped_xid is '0'::xid8. pg_current_xact_id() never returns 0 (it is
-- InvalidTransactionId), so the trigger guard on (bumped_xid, bumped_at)
-- always lets the first real write bump a seeded row.
--
-- Re-runnable: ON CONFLICT DO NOTHING never resets or rewrites a version row.

INSERT INTO config_artifact_content_versions (repo_id, version, bumped_xid, bumped_at)
SELECT d.repo_id, 0, '0'::xid8, now()
FROM (SELECT DISTINCT repo_id FROM content_files) AS d
ORDER BY d.repo_id
ON CONFLICT (repo_id) DO NOTHING;
