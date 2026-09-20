-- 117_code_function_fingerprint_shingles.sql
--
-- Shingle-set column persisting the renamed 5-shingle identity set for the
-- code-divergence report (epic #6833, child #6837). Fingerprints arrive on
-- function content entities as the body_shingles metadata key (sorted unique
-- FNV-64a identities, EncodeShingles hex); the content writer fans it out
-- here so the #6837 reducer can verify exact Jaccard from this narrow
-- column without reading content_entities.source_cache.
--
-- shingles is NULL for exact-only tiers and for rows written before this
-- migration (pre-#6837 payloads carry no shingle set): absent means "not
-- persisted", never "unique". A re-fingerprint upsert overwrites the set in
-- the same ON CONFLICT row update as the sketch, so bands and shingles stay
-- a pure function of the current fp row.
--
-- No new index: the reducer reads shingle sets by entity_id (primary key)
-- for the LSH-nominated candidate pairs, never by shingle value, so the
-- repo lookup index from migration 111 needs no companion.

ALTER TABLE IF EXISTS code_function_fingerprint
    ADD COLUMN IF NOT EXISTS shingles TEXT NULL;
