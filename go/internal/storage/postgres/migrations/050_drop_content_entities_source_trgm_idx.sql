-- 050_drop_content_entities_source_trgm_idx.sql
--
-- Drops the pg_trgm GIN index on content_entities.source_cache. Same
-- rationale as 049: pure write-tax, never selected by the ILIKE query
-- planner, idx_scan=0 on the live e2e3586persist stack. Reclaims ~518 MB.
-- No-op on fresh databases where 004 no longer creates this index.
-- Makes EnsureContentSearchIndexes a no-op (#4862).

DROP INDEX CONCURRENTLY IF EXISTS content_entities_source_trgm_idx;
