-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #7620: repository_reindex_requests holds the per-repository reindex
-- watermarks behind POST /api/v0/admin/reindex with scope "repository". Each
-- row is one git default-branch repository scope and the database-clock time
-- of its newest reindex request. A request upserts
-- GREATEST(existing, now()), so a row never moves backward. Every git ingester
-- shard reads the rows newer than the fleet watermark once per cycle and
-- forces a full re-parse of each owned scope whose newest activated full
-- generation predates its row. Nothing claims, completes, or deletes a row: a
-- request is satisfied when that scope's forced full activates, and a row at
-- or before the fleet watermark has no further effect.
--
-- The table holds at most one row per repository scope ever requested. The
-- per-cycle read is a sequential scan: about 1 ms for 10k rows on PostgreSQL
-- 18, so no secondary index.
CREATE TABLE IF NOT EXISTS repository_reindex_requests (
    scope_id TEXT PRIMARY KEY,
    requested_at TIMESTAMPTZ NOT NULL
);
