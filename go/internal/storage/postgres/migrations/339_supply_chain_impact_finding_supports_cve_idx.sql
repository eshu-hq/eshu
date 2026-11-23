-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #11197 (#10880 PR-B2b): the cve_id arm of the holder read and its key-range statements.
-- Partial on the non-empty id, with fact_id second so a key range [lower, upper) is an
-- index condition; the finding facts store no empty cve_id the read could match, and
-- every statement carries the literal predicate that proves this index.
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
CREATE INDEX CONCURRENTLY IF NOT EXISTS supply_chain_impact_finding_supports_cve_idx
    ON supply_chain_impact_finding_supports (cve_id, fact_id)
    WHERE cve_id <> '';
