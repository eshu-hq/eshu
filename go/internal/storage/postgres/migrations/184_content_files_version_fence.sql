-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- Content-version fence for the config-artifact read model (#7126 PR3b-1b,
-- #8155; commit-time bump by ruling arb-8155-f1).
--
-- Every content_files write bumps config_artifact_content_versions (migration
-- 170) once per repository per transaction, at COMMIT, in the content
-- transaction, so the bump commits with the content. A derive (PR3b-2) reads
-- the version before it lists files and trusts its rows only while the
-- version has not moved. Design: docs/internal/design/7126-ingest-time-config-artifacts.md
-- and its -protocol.md companion, section 2.
--
-- Two steps. The four content_files triggers stage the touched repositories
-- into config_artifact_fence_pending: INSERT and UPDATE are statement-level
-- with a NEW TABLE, DELETE is statement-level with an OLD TABLE (the S3 arm 3
-- shape), TRUNCATE stages every repository that has a version row. A
-- deferred constraint trigger on the pending table then runs one ordered
-- upsert at COMMIT and deletes the transaction's pending rows. Constraint
-- triggers must be row-level and cannot use transition tables, which is why
-- the staging table carries the deferral; its first firing bumps every staged
-- repository and the later firings find nothing.
--
-- Lock order. A content transaction holds its content rows from its first
-- statement and takes version rows only inside COMMIT, after its last content
-- lock, in repo_id order. Nobody waits on a version row while another session
-- needs its content rows, so a transaction that deletes content_files twice
-- (the retention chunk cycle: the over-limit page, then the normal batch)
-- cannot close a cycle through a version row. SET CONSTRAINTS ALL IMMEDIATE
-- runs the bump early and gives that property up for the rest of the
-- transaction; no Eshu code issues it.
--
-- Guard. The upsert bumps a row only when (bumped_xid, bumped_at) differs from
-- (pg_current_xact_id(), now()): once per transaction, also after SET
-- CONSTRAINTS IMMEDIATE and a later statement. The pair, not the id alone,
-- because a logical restore restarts the transaction id counter while stored
-- bumped_xid values survive; now() still differs for that later transaction.
--
-- Who bumps. The triggers fire for every session in replication role origin
-- or local. session_replication_role = replica (superuser or SET privilege;
-- logical-replication apply workers) skips them, as it skips every ordinary
-- trigger, the migration 138 RI and secret-line triggers included. No Eshu
-- binary sets it; a guard test in go/internal/storage/postgres/config/artifacts
-- fails on any non-test Go use. An UPDATE that changes repo_id bumps only the
-- new repository (the UPDATE trigger reads NEW TABLE only); production never
-- changes repo_id (both upsert SET lists omit it, and the same guard test
-- fails on a Go UPDATE content_files that sets it); repo_id is the first
-- primary-key column, so a move is a delete plus an insert.
--
-- Names fix the firing order. Row-level AFTER triggers fire before
-- statement-level ones, then name order: the content_files_version_fence_
-- prefix sorts after content_files_secret_lines_, and the migration 138
-- foreign key's RI triggers are row-level, so staging runs last.
--
-- The pending table is UNLOGGED: its rows live only inside one transaction,
-- and a crash aborts that transaction anyway.
--
-- Lock window. CREATE TRIGGER takes SHARE ROW EXCLUSIVE on content_files until
-- this file commits; it waits behind a running content or retention
-- transaction, bounded by the migrator's lock_timeout and retry budget. This
-- file holds no DML: the version-0 seed is migration 185.
--
-- Re-runnable, also while writers run (review finding N1): IF NOT EXISTS and
-- CREATE OR REPLACE throughout, and DDL takes its locks in the order writers
-- do: the four content_files triggers first, the pending table's constraint
-- trigger last and only when it is missing, so a re-run never holds the
-- pending table while it waits on content_files.

CREATE UNLOGGED TABLE IF NOT EXISTS config_artifact_fence_pending (
    bumped_xid xid8 NOT NULL,
    repo_id TEXT NOT NULL,
    PRIMARY KEY (bumped_xid, repo_id)
);

CREATE OR REPLACE FUNCTION config_artifact_version_fence_stage_new()
RETURNS trigger
LANGUAGE plpgsql
AS $fn$
BEGIN
    INSERT INTO config_artifact_fence_pending (bumped_xid, repo_id)
    SELECT pg_current_xact_id(), r.repo_id
    FROM (SELECT DISTINCT repo_id FROM fence_new_rows) AS r
    ON CONFLICT DO NOTHING;
    RETURN NULL;
END;
$fn$;

CREATE OR REPLACE FUNCTION config_artifact_version_fence_stage_old()
RETURNS trigger
LANGUAGE plpgsql
AS $fn$
BEGIN
    INSERT INTO config_artifact_fence_pending (bumped_xid, repo_id)
    SELECT pg_current_xact_id(), r.repo_id
    FROM (SELECT DISTINCT repo_id FROM fence_old_rows) AS r
    ON CONFLICT DO NOTHING;
    RETURN NULL;
END;
$fn$;

CREATE OR REPLACE FUNCTION config_artifact_version_fence_stage_all()
RETURNS trigger
LANGUAGE plpgsql
AS $fn$
BEGIN
    INSERT INTO config_artifact_fence_pending (bumped_xid, repo_id)
    SELECT pg_current_xact_id(), v.repo_id
    FROM config_artifact_content_versions AS v
    ON CONFLICT DO NOTHING;
    RETURN NULL;
END;
$fn$;

CREATE OR REPLACE FUNCTION config_artifact_version_fence_commit()
RETURNS trigger
LANGUAGE plpgsql
AS $fn$
BEGIN
    INSERT INTO config_artifact_content_versions AS v (repo_id, version, bumped_xid, bumped_at)
    SELECT p.repo_id, 1, pg_current_xact_id(), now()
    FROM config_artifact_fence_pending AS p
    WHERE p.bumped_xid = pg_current_xact_id()
    ORDER BY p.repo_id
    ON CONFLICT (repo_id) DO UPDATE
    SET version = v.version + 1, bumped_xid = EXCLUDED.bumped_xid, bumped_at = EXCLUDED.bumped_at
    WHERE v.bumped_xid IS DISTINCT FROM EXCLUDED.bumped_xid
       OR v.bumped_at IS DISTINCT FROM EXCLUDED.bumped_at;
    DELETE FROM config_artifact_fence_pending WHERE bumped_xid = pg_current_xact_id();
    RETURN NULL;
END;
$fn$;

CREATE OR REPLACE TRIGGER content_files_version_fence_insert
    AFTER INSERT ON content_files
    REFERENCING NEW TABLE AS fence_new_rows
    FOR EACH STATEMENT
    EXECUTE FUNCTION config_artifact_version_fence_stage_new();

CREATE OR REPLACE TRIGGER content_files_version_fence_update
    AFTER UPDATE ON content_files
    REFERENCING NEW TABLE AS fence_new_rows
    FOR EACH STATEMENT
    EXECUTE FUNCTION config_artifact_version_fence_stage_new();

CREATE OR REPLACE TRIGGER content_files_version_fence_delete
    AFTER DELETE ON content_files
    REFERENCING OLD TABLE AS fence_old_rows
    FOR EACH STATEMENT
    EXECUTE FUNCTION config_artifact_version_fence_stage_old();

CREATE OR REPLACE TRIGGER content_files_version_fence_truncate
    AFTER TRUNCATE ON content_files
    FOR EACH STATEMENT
    EXECUTE FUNCTION config_artifact_version_fence_stage_all();

-- Last, and only when missing: a re-run must not take ACCESS EXCLUSIVE on the
-- pending table while a writer that already holds content_files waits to
-- stage into it (review finding N1). CREATE OR REPLACE does not apply to a
-- constraint trigger, so an existing one is left in place; it is bound to
-- the function above by OID, which CREATE OR REPLACE FUNCTION keeps.
DO $create_commit_trigger$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_trigger
        WHERE tgrelid = 'config_artifact_fence_pending'::regclass
          AND tgname = 'config_artifact_fence_pending_commit'
    ) THEN
        CREATE CONSTRAINT TRIGGER config_artifact_fence_pending_commit
            AFTER INSERT ON config_artifact_fence_pending
            DEFERRABLE INITIALLY DEFERRED
            FOR EACH ROW
            EXECUTE FUNCTION config_artifact_version_fence_commit();
    END IF;
END
$create_commit_trigger$;
