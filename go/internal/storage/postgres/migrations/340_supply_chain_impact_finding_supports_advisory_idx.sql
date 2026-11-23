-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #11197 (#10880 PR-B2b): the advisory_id arm of the holder read (#10408) and its key-range
-- statements. Same shape as the cve_id index.
--
-- INCLUDE (scope_id, generation_id): the key-range statements read only these two
-- columns of each matching support, so the btree can answer them index-only. At
-- 8,000,000 supports and a cve id on 1,600,000 rows, the cold 16-range read took
-- 351 s through the plain index (about one heap page per matching row) and 5.4 s
-- through this one after a VACUUM. It is a capacity multiplier of about 1/f, f being
-- the share of the id's rows on pages that are not all-visible, not a guarantee of
-- zero heap fetches: only a vacuumed table reads index-only. Cost: about +48% index
-- size (docs/internal/evidence/11197-holder-reads-supports.md).
--
-- The supply-chain impact holder fan-out reads move from the finding facts to the
-- per-support table (migration 338), so each arm of the holder read needs its own
-- index there, as it has on fact_records (migrations 288, 289 and 337). The four
-- indexes of this set are 339 to 342, one per file. Measured cost and plans:
-- docs/internal/evidence/11197-holder-reads-supports.md.
--
-- CONCURRENTLY, so the build does not block writers on the supports table.
-- Single statement: a concurrent build cannot run inside a transaction block. A
-- failed build leaves an INVALID index; the schema apply path drops it by name
-- before re-running this file (concurrentIndexNamesForInvalidCleanup). The schema
-- Job returns only after the build is valid, and the reducer that reads the index
-- rolls out after the Job. The build waits for transactions older than its
-- snapshot, so run it with no long transaction open.
CREATE INDEX CONCURRENTLY IF NOT EXISTS supply_chain_impact_finding_supports_advisory_idx
    ON supply_chain_impact_finding_supports (advisory_id, fact_id)
    INCLUDE (scope_id, generation_id)
    WHERE advisory_id <> '';
