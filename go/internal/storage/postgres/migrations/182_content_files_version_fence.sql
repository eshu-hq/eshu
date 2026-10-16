-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- Content-version fence for the config-artifact read model (#7126 PR3b-1b).
--
-- Four triggers on content_files bump config_artifact_content_versions
-- (migration 170) once per repository per transaction, for every session,
-- inside the content statement's transaction, so the bump commits with the
-- content. A derive (PR3b-2) reads the version before it lists files and
-- trusts its rows only while the version has not moved. No session setting
-- skips these triggers: every writer, older binaries and manual SQL included,
-- must bump. Design: docs/internal/design/7126-ingest-time-config-artifacts.md
-- and its -protocol.md companion, section 2.
--
-- Shape. INSERT and UPDATE are statement-level with a NEW TABLE, DELETE is
-- statement-level with an OLD TABLE (S3 arm 3 picked this shape over a
-- row-level DELETE trigger), and TRUNCATE bumps every existing version row.
-- An INSERT ... ON CONFLICT DO UPDATE fires both the INSERT and the UPDATE
-- trigger; the bumped_xid guard makes the second a no-op. pg_current_xact_id()
-- is a 64-bit id, so the guard cannot wrap.
--
-- Lock order. A statement-level trigger takes the version-row locks of a
-- multi-repository statement in its SELECT order, and ORDER BY r.repo_id makes
-- that order ascending, so two multi-repository statements cannot deadlock on
-- version rows. AFTER triggers fire at the end of the content statement, after
-- it has locked every content row it touches, so a session waiting on a
-- version row holds only content rows.
--
-- Names fix the firing order. PostgreSQL fires row-level AFTER triggers
-- before statement-level ones, and triggers of one level in name order. The
-- content_files_version_fence_ prefix sorts after the migration 138
-- content_files_secret_lines_ triggers, and the migration 138 foreign key's
-- RI triggers are row-level, so the fence fires last: the version row is held
-- only from the bump to the commit.
--
-- Lock window. CREATE TRIGGER takes SHARE ROW EXCLUSIVE on content_files until
-- this file commits, which waits behind a running retention or content
-- transaction and blocks content writes (not reads) for the catalog change
-- only; the migrator's lock_timeout and retry budget bound the wait. This file
-- holds no DML: the version-0 seed is migration 183.
--
-- Re-runnable: CREATE OR REPLACE for every function and trigger.

CREATE OR REPLACE FUNCTION config_artifact_version_fence_bump_new()
RETURNS trigger
LANGUAGE plpgsql
AS $fn$
BEGIN
    INSERT INTO config_artifact_content_versions AS v (repo_id, version, bumped_xid, bumped_at)
    SELECT r.repo_id, 1, pg_current_xact_id(), now()
    FROM (SELECT DISTINCT repo_id FROM fence_new_rows) AS r
    ORDER BY r.repo_id
    ON CONFLICT (repo_id) DO UPDATE
    SET version = v.version + 1, bumped_xid = EXCLUDED.bumped_xid, bumped_at = EXCLUDED.bumped_at
    WHERE v.bumped_xid IS DISTINCT FROM EXCLUDED.bumped_xid;
    RETURN NULL;
END;
$fn$;

CREATE OR REPLACE FUNCTION config_artifact_version_fence_bump_old()
RETURNS trigger
LANGUAGE plpgsql
AS $fn$
BEGIN
    INSERT INTO config_artifact_content_versions AS v (repo_id, version, bumped_xid, bumped_at)
    SELECT r.repo_id, 1, pg_current_xact_id(), now()
    FROM (SELECT DISTINCT repo_id FROM fence_old_rows) AS r
    ORDER BY r.repo_id
    ON CONFLICT (repo_id) DO UPDATE
    SET version = v.version + 1, bumped_xid = EXCLUDED.bumped_xid, bumped_at = EXCLUDED.bumped_at
    WHERE v.bumped_xid IS DISTINCT FROM EXCLUDED.bumped_xid;
    RETURN NULL;
END;
$fn$;

-- TRUNCATE has no transition table. It takes ACCESS EXCLUSIVE on
-- content_files, so no content writer holds a version row while this runs.
CREATE OR REPLACE FUNCTION config_artifact_version_fence_bump_all()
RETURNS trigger
LANGUAGE plpgsql
AS $fn$
BEGIN
    UPDATE config_artifact_content_versions AS v
    SET version = v.version + 1, bumped_xid = pg_current_xact_id(), bumped_at = now()
    WHERE v.bumped_xid IS DISTINCT FROM pg_current_xact_id();
    RETURN NULL;
END;
$fn$;

CREATE OR REPLACE TRIGGER content_files_version_fence_insert
    AFTER INSERT ON content_files
    REFERENCING NEW TABLE AS fence_new_rows
    FOR EACH STATEMENT
    EXECUTE FUNCTION config_artifact_version_fence_bump_new();

CREATE OR REPLACE TRIGGER content_files_version_fence_update
    AFTER UPDATE ON content_files
    REFERENCING NEW TABLE AS fence_new_rows
    FOR EACH STATEMENT
    EXECUTE FUNCTION config_artifact_version_fence_bump_new();

CREATE OR REPLACE TRIGGER content_files_version_fence_delete
    AFTER DELETE ON content_files
    REFERENCING OLD TABLE AS fence_old_rows
    FOR EACH STATEMENT
    EXECUTE FUNCTION config_artifact_version_fence_bump_old();

CREATE OR REPLACE TRIGGER content_files_version_fence_truncate
    AFTER TRUNCATE ON content_files
    FOR EACH STATEMENT
    EXECUTE FUNCTION config_artifact_version_fence_bump_all();
