-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #6679: shared_projection_acceptance must be advance-only on generation
-- order. The upsert guard compares the incoming generation's ordering key with
-- the stored row's own copy of it, because after a row-lock wait READ
-- COMMITTED re-checks the ON CONFLICT DO UPDATE condition only against the
-- target row's latest version; a lookup of the stored generation in
-- scope_generations would read the statement snapshot and miss a generation
-- committed after it (review F1). The key is scope_generations.ingested_at,
-- which is written once at generation insert and never updated, paired with
-- generation_id as the tie-break -- the order activation and supersession use.
--
-- Nullable with no default so this ALTER is a catalog-only change: it holds
-- ACCESS EXCLUSIVE for milliseconds, not for a table rewrite. A NULL stored key
-- sorts as older than any incoming generation (the guard always advances it),
-- which covers rows written by a pre-#6679 binary during a rolling deploy. The
-- backfill lives in 126 so this lock is not held while rows are rewritten.
-- New migration per #7002; 011 is never edited.
ALTER TABLE shared_projection_acceptance
    ADD COLUMN IF NOT EXISTS generation_ingested_at TIMESTAMPTZ NULL;
