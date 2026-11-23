-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- 344_supply_chain_impact_finding_supports_image_ref_idx.sql (#10880 PR-B2a, arbiter
-- ruling V and its index amendment): the partial index behind the unscoped
-- image_ref read. An image reference is a per-support column that is not part of
-- the canonical key, so copies of one finding can name different images and the
-- winners row cannot answer the filter. The read fetches every support of the image
-- through this index (a materialized candidate fetch, uncapped), picks the best per
-- key and applies the rank filters to that pick.
--
-- Partial on image_ref <> '' so supports without an image are not indexed. Measured
-- on 8,000,481 supports: 54 MB; a typical image (about 380 supports) reads in 9 to 19
-- ms and a stress image of 160,000 supports in 1.1 to 1.5 s
-- (docs/internal/evidence/10880-pr-b2a/RESULTS.md). The WAL cost belongs to the pair
-- with migration 343 (see its header).
--
-- CONCURRENTLY so bootstrap never blocks writers on this table; IF NOT EXISTS for
-- rerun safety. One statement per file so the migration coordinator runs it outside
-- a transaction on a dedicated bootstrap connection, and an INVALID index left by a
-- failed build is dropped by name before the definition reruns
-- (SQLDB.dropInvalidConcurrentIndexes). A NEW migration file per issue #7002: never
-- edit an applied migration.
CREATE INDEX CONCURRENTLY IF NOT EXISTS supply_chain_impact_finding_supports_image_ref_idx
    ON supply_chain_impact_finding_supports (image_ref)
    WHERE image_ref <> '';
