-- 111_code_function_fingerprint.sql
--
-- Narrow side tables persisting function body fingerprints for the
-- code-divergence report (epic #6833, child #6835). Fingerprints arrive on
-- function content entities as metadata keys (body_fp_exact,
-- body_fp_renamed, body_sketch, body_token_count); the content writer
-- fans them out here so the #6836 grouping path reads narrow columns and
-- never touches content_entities.source_cache.
--
-- code_function_fingerprint holds one row per fingerprinted function:
-- exact and alpha-renamed hashes, the MinHash sketch hex (32 LSH bands are
-- re-derived from it with fingerprint.BandHashes, so band storage stays a
-- pure function of this row), and the leaf token count. fp_renamed and
-- sketch are NULL for exact-only tiers.
--
-- code_fingerprint_band holds one row per (entity, band) for the LSH band
-- self-join. The (repo_id, band_no, band_hash) lookup index is REQUIRED by
-- the #6834 EXPLAIN evidence (single-repo band join 563.5ms unindexed with
-- LIMIT 1000 down to 4.0ms indexed); the equality grouping needs no index
-- (31.0ms seq scan + hash aggregate, planner-optimal at full-corpus scale),
-- so code_function_fingerprint carries only its primary key plus the repo
-- lookup index for per-repo maintenance.
--
-- Stale-row invariant: side rows are reaped by anti-join against
-- content_entities per repo after the entity upsert+reap in the same Write
-- call, so a retracted or churned entity_id never leaves orphan side rows.

CREATE TABLE IF NOT EXISTS code_function_fingerprint (
    entity_id TEXT PRIMARY KEY,
    repo_id TEXT NOT NULL,
    fp_exact TEXT NOT NULL,
    fp_renamed TEXT NULL,
    sketch TEXT NULL,
    token_count INTEGER NOT NULL,
    indexed_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS code_function_fingerprint_repo_idx
    ON code_function_fingerprint (repo_id);

CREATE TABLE IF NOT EXISTS code_fingerprint_band (
    repo_id TEXT NOT NULL,
    band_no SMALLINT NOT NULL,
    band_hash TEXT NOT NULL,
    entity_id TEXT NOT NULL,
    PRIMARY KEY (repo_id, band_no, band_hash, entity_id)
);

CREATE INDEX IF NOT EXISTS code_fingerprint_band_lookup_idx
    ON code_fingerprint_band (repo_id, band_no, band_hash);
