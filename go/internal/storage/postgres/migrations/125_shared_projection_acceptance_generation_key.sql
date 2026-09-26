-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #6679: shared_projection_acceptance must be advance-only on generation
-- order. The upsert guard compares the incoming generation's ordering key with
-- the stored row's own copy of it, because after a row-lock wait READ
-- COMMITTED re-checks the ON CONFLICT DO UPDATE condition only against the
-- target row's latest version; a lookup of the stored generation in
-- scope_generations reads the statement snapshot and misses a generation
-- committed after it (review F1). The key is scope_generations.ingested_at,
-- written once at generation insert and never updated, paired with
-- generation_id as the tie-break -- the order activation and supersession use.
--
-- Nullable with no default so this ALTER is catalog-only: it holds ACCESS
-- EXCLUSIVE for milliseconds (36-62 ms measured at 1M-10M rows), not for a
-- table rewrite. There is deliberately no backfill. A one-transaction
-- backfill measured 31 s at 1M, 3m11s at 5M and 6m34s at 10M rows, stalled
-- every acceptance writer for its whole run, and permanently doubled the
-- table (every update is non-HOT). Instead a NULL key -- any row written
-- before this migration, or by a pre-#6679 binary during a rolling deploy --
-- is resolved from scope_generations inside the upsert guard at write time,
-- and every applied write fills it, so legacy rows heal on their next write.
-- New migration per #7002; 011 is never edited.
ALTER TABLE shared_projection_acceptance
    ADD COLUMN IF NOT EXISTS generation_ingested_at TIMESTAMPTZ NULL;
