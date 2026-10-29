-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #9320: the status shared-projection backlog (status_queries.go
-- shared_projection_pending) now measures age from
-- MIN(COALESCE(reopened_at, created_at)) over pending intents, grouped by
-- projection_domain. shared_projection_intents_pending_idx holds
-- (projection_domain, completed_at, created_at) but not reopened_at, so the
-- new aggregate read the heap of every block holding a pending row.
--
-- On a disposable PostgreSQL 18.6 fixture (500,000 intents, 20,000 pending,
-- 12 domains) the aggregate was an Index Only Scan at 1.46-1.55 ms before
-- #9320, a Bitmap Heap Scan at 11.9-12.9 ms (8,441 heap blocks) with the new
-- expression and no index, and an Index Only Scan at 1.27-1.86 ms with this
-- index (808 kB). Fixture timings, not a deployed claim.
--
-- The partial predicate keeps completed intents out of the index, so a
-- completion or a completed-row re-upsert adds no entry. One statement per
-- file is required for a concurrent build; intent writers continue during it.
CREATE INDEX CONCURRENTLY IF NOT EXISTS shared_projection_intents_domain_pending_age_idx
    ON shared_projection_intents (projection_domain, created_at) INCLUDE (reopened_at)
    WHERE completed_at IS NULL;
