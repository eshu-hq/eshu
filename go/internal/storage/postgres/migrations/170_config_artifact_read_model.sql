-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- Config-artifact read model tables (#7126 PR3b-1a).
--
-- The repository story and context routes recompute the config_paths rows of
-- every related repository from content_files on every request. The decision
-- record (docs/internal/design/7126-ingest-time-config-artifacts.md) moves
-- that work to content-write time and stores the rows here, without the
-- repository name. This file creates the empty tables only. It adds no
-- trigger, no seed row, and no reader or writer, so it changes no
-- content_files behavior. The content_files version-fence triggers and the
-- one version-0 row per repository land in the next migration (PR3b-1b),
-- after the S3 arm 3 and arm 5 reruns pass.
--
-- config_artifact_content_versions: one row per repository. The PR3b-1b
-- trigger bumps version once per content transaction. A derive reads the
-- version before it lists files and writes rows only when the version or the
-- extractor version advances. Rule: delete or reset a version row only in
-- the same transaction as the repository's state row. A version row created
-- again while the state row survives counts up from 1 and can reach the old
-- state.content_version, so readers would trust stale rows. A guard test in
-- go/internal/storage/postgres/config/artifacts enforces the rule for Go.
--
-- config_artifact_repo_state: one row per derived repository. row_count is
-- NULL until the first successful derive. A failure-only row has a NULL
-- content_version. scope_id and generation_id are advisory.
-- last_failure_extractor_version supports the walker's skip rule (skip after
-- 5 failures only while it equals the current extractor version).
--
-- config_artifact_paths: the name-free rows. raw_path is the cleaned path of
-- the build with no repository name. name_scoped marks the rows the read step
-- strips the name prefix from. The key is (repo_id, row_digest): a key of the
-- four TEXT columns fails with SQLSTATE 54000 once a path reaches about 2,610
-- incompressible bytes. row_digest is defined here and never in Go. The
-- fields are length-prefixed because a path can contain any separator.
-- length() counts characters, not bytes.
--
-- convert_to is STABLE, so PostgreSQL rejects it in a generated column.
-- config_artifact_utf8 is IMMUTABLE because server_encoding is fixed for the
-- life of a database, and a dump and restore recomputes generated columns on
-- load, so a stored digest never crosses an encoding. Each binary that derives
-- or reads the model checks SHOW server_encoding at start and skips the read
-- model on a database that is not UTF8.
--
-- Each derive deletes and re-inserts a repository's rows. A read after a
-- rewrite with no vacuum measured +40% (S2), so the rows table vacuums at a
-- 2% dead-tuple fraction.
--
-- The backfill marker and the walker cursor follow migration 109. Re-runnable:
-- every statement is IF NOT EXISTS or OR REPLACE, and the file holds no DML.

CREATE TABLE IF NOT EXISTS config_artifact_content_versions (
    repo_id TEXT PRIMARY KEY,
    version BIGINT NOT NULL,
    bumped_xid xid8 NOT NULL,
    bumped_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS config_artifact_repo_state (
    repo_id TEXT PRIMARY KEY,
    content_version BIGINT NULL,
    extractor_version INTEGER NOT NULL,
    row_count INTEGER NULL,
    files_listed INTEGER NOT NULL DEFAULT 0,
    candidate_count INTEGER NOT NULL DEFAULT 0,
    candidates_read INTEGER NOT NULL DEFAULT 0,
    list_truncated BOOLEAN NOT NULL DEFAULT FALSE,
    scope_id TEXT NOT NULL DEFAULT '',
    generation_id TEXT NOT NULL DEFAULT '',
    derived_at TIMESTAMPTZ NULL,
    failure_count INTEGER NOT NULL DEFAULT 0,
    last_error_class TEXT NULL,
    last_failure_at TIMESTAMPTZ NULL,
    last_failure_extractor_version INTEGER NULL
);

CREATE OR REPLACE FUNCTION config_artifact_utf8(t text) RETURNS bytea
LANGUAGE sql IMMUTABLE PARALLEL SAFE STRICT
RETURN convert_to(t, 'UTF8');

CREATE TABLE IF NOT EXISTS config_artifact_paths (
    repo_id TEXT NOT NULL,
    raw_path TEXT NOT NULL,
    relative_path TEXT NOT NULL,
    evidence_kind TEXT NOT NULL,
    name_scoped BOOLEAN NOT NULL,
    row_digest BYTEA GENERATED ALWAYS AS (sha256(config_artifact_utf8(
        length(raw_path)::text || ':' || raw_path ||
        length(relative_path)::text || ':' || relative_path ||
        evidence_kind))) STORED,
    PRIMARY KEY (repo_id, row_digest)
) WITH (autovacuum_vacuum_scale_factor = 0.02);

CREATE TABLE IF NOT EXISTS config_artifact_backfill_markers (
    marker_name TEXT PRIMARY KEY,
    completed_at TIMESTAMPTZ NOT NULL
);

-- The reducer walker stores where its last page ended, so a restarted
-- process resumes from one primary-key lookup.
CREATE TABLE IF NOT EXISTS config_artifact_reconcile_cursor (
    walk_name TEXT PRIMARY KEY,
    cursor TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);
