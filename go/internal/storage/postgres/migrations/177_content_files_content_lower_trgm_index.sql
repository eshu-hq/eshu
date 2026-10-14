-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #7975: trigram GIN over the stored lowercase copy from migration 176. The
-- unscoped search tail tests content_lower with LIKE under
-- SET LOCAL enable_indexscan = off, so without this index the planner can
-- only choose an unindexed scan; the search's readiness check (migration 176)
-- refuses to run until this index is valid instead.
--
-- This file must remain one concurrent statement: PostgreSQL rejects CREATE
-- INDEX CONCURRENTLY in a transaction block, and the migration coordinator
-- executes sole concurrent-index definitions in autocommit mode. The schema
-- executor clears an invalid same-name index before retrying a failed build.
--
-- Migration proof (local Postgres 18.3 scratch container, 2026-10-14):
-- 3,001 rows with one 49 MB value (51 MB table, shaped like the 176 proof
-- corpus). CREATE INDEX CONCURRENTLY plus ANALYZE: 4.6 s wall on an idle
-- host, so plan a minute-scale window on a loaded production primary; until
-- the index is valid the search 503s by design. Watch a live build with
-- SELECT phase, blocks_done, blocks_total FROM pg_stat_progress_create_index.
CREATE INDEX CONCURRENTLY IF NOT EXISTS content_files_content_lower_trgm_idx
    ON content_files USING gin (content_lower gin_trgm_ops);
