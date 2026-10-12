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
CREATE INDEX CONCURRENTLY IF NOT EXISTS content_files_content_lower_trgm_idx
    ON content_files USING gin (content_lower gin_trgm_ops);
