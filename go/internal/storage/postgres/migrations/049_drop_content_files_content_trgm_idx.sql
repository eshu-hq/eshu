-- 049_drop_content_files_content_trgm_idx.sql
--
-- Drops the pg_trgm GIN index on content_files.content. The index was
-- pure write-tax and the planner never selected it for the repo-scoped
-- ILIKE search queries (content_reader.go). The repo index
-- (content_files_language_repo_idx) + ILIKE Filter consistently beat
-- the GIN on both EXPLAIN (ANALYZE) shape and wall-time. Live-stack
-- idx_scan=0 on e2e3586persist with 22h uptime confirms no query ever
-- used it. Dropping reclaims ~233 MB and speeds content inserts ~13x.
-- No-op on fresh databases where 004 no longer creates this index.
-- Makes EnsureContentSearchIndexes a no-op (#4862).

DROP INDEX CONCURRENTLY IF EXISTS content_files_content_trgm_idx;
