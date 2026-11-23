-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #11197 (#10880 PR-B2b): the advisory-group member arm of the holder read (#10408), the
-- mass probe and the over-1,024-byte lookup fallback. A GIN on the same members
-- expression the fact index uses (migration 289), so the ?| and ? operators hit it.
--
-- The supply-chain impact holder fan-out reads move from the finding facts to the
-- per-support table (migration 338), so each arm of the holder read needs its own
-- index there, as it has on fact_records (migrations 288, 289 and 337). The four
-- indexes of this set are 339 to 342, one per file. Measured cost and plans:
-- docs/internal/evidence/11197-holder-reads-supports.md.
--
-- fastupdate = off (arbiter-b2b-gin-covering, decision 1). A GIN with fastupdate on
-- batches inserts in a pending list that the planner prices into every scan of the
-- index: with the default 4 MB list more than about 165 pages full, the holder mass
-- probe's member arm costs more than a sequential scan of the supports table under
-- LIMIT, and at 8,000,000 supports a probe of five ids took 254 s instead of 291 ms
-- (docs/internal/evidence/11197-holder-reads-supports.md). With fastupdate off every
-- insert maintains the index in place, so there is no pending list to price and no
-- insert that stalls for the 12 to 15 s a full-list flush takes.
--
-- CONCURRENTLY, so the build does not block writers on the supports table.
-- Single statement: a concurrent build cannot run inside a transaction block. A
-- failed build leaves an INVALID index; the schema apply path drops it by name
-- before re-running this file (concurrentIndexNamesForInvalidCleanup). The schema
-- Job returns only after the build is valid, and the reducer that reads the index
-- rolls out after the Job. The build waits for transactions older than its
-- snapshot, so run it with no long transaction open.
CREATE INDEX CONCURRENTLY IF NOT EXISTS supply_chain_impact_finding_supports_members_idx
    ON supply_chain_impact_finding_supports USING GIN ((payload #> '{provenance,advisory_group_members}'))
    WITH (fastupdate = off);
