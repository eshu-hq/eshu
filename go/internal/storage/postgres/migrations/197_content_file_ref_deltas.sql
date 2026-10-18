-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #5393 Phase 1: content overlay storage for tracked non-default refs.
--
-- Branch data must never enter the canonical graph or canonical content.
-- content_files stays default-branch-only, byte-identical to today, so the
-- overlay lives in its own table -- never a ref column on content_files.
--
-- content_file_ref_deltas holds one row per (repo, ref, path) where the
-- ref's content differs from the default branch (drift-sized, ~5% of
-- files). Rows are hash-gated: the writer compares the branch content hash
-- against the default content hash and writes a row only on mismatch; a
-- converged file deletes its row instead. Files deleted on the branch keep
-- a tombstone row (content NULL, is_deleted) so a ref-scoped read can
-- suppress them instead of falling back to the default copy. The
-- tombstone-shape CHECK keeps that structural: a deleted row carries no
-- content, a live row always does.
--
-- content_file_ref_delta_bases holds one row per tracked (repo, ref): the
-- default-branch SHA the ref's deltas were diffed against
-- (base_default_sha). When the default advances past the recorded base,
-- DeltaBaseStale reports true and the caller schedules a re-diff bounded to
-- that ref's rows. Per-repo tracked refs stay capped (Phase 0 kept the
-- per-repo cap at 3), so the re-diff fan-out is bounded by construction.
--
-- Deliberately no foreign keys: a REFERENCES into content_files would take
-- SHARE ROW EXCLUSIVE on the canonical table at CREATE time and fence
-- canonical writers on every overlay write. The overlay references
-- canonical rows by value (content_hash comparison), never by constraint.
--
-- The trigram GIN mirrors content_files_content_trgm_idx so a future
-- ref-scoped content search has its own index; the primary key already
-- serves (repo_id, ref) prefix probes and single-path lookups. Plain
-- CREATE INDEX, not CONCURRENTLY: this table is empty at bootstrap, so the
-- build is instant and the migration stays one transactional unit.
--
-- Rollback (forward-only bootstrap has no down-migration step; run by hand
-- if the overlay must be removed before any reader depends on it):
--   DROP INDEX IF EXISTS content_file_ref_deltas_content_trgm_idx;
--   DROP TABLE IF EXISTS content_file_ref_deltas;
--   DROP TABLE IF EXISTS content_file_ref_delta_bases;
CREATE TABLE IF NOT EXISTS content_file_ref_deltas (
    repo_id TEXT NOT NULL,
    ref TEXT NOT NULL,
    relative_path TEXT NOT NULL,
    content TEXT NULL,
    content_hash TEXT NOT NULL,
    is_deleted BOOLEAN NOT NULL DEFAULT FALSE,
    indexed_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (repo_id, ref, relative_path),
    CHECK ((is_deleted AND content IS NULL) OR (NOT is_deleted AND content IS NOT NULL))
);

CREATE TABLE IF NOT EXISTS content_file_ref_delta_bases (
    repo_id TEXT NOT NULL,
    ref TEXT NOT NULL,
    base_default_sha TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (repo_id, ref)
);

CREATE INDEX IF NOT EXISTS content_file_ref_deltas_content_trgm_idx
    ON content_file_ref_deltas USING gin (content gin_trgm_ops);
