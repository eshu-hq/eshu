-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #10880 PR-B2c: the partial btree that serves the package arm of the
-- security-alert reconciliation read of the supports table (#11198). The read
-- pages the supports by key: each arm (repository_id, package_id, cve_id,
-- advisory_id) is a per-key ordered LIMIT scan, so a key's page can only stop
-- early on an index that ends in fact_id. What was measured, in
-- docs/internal/evidence/10880-b2c/reader_plans.md: the superseded single
-- statement (an OR of the four arms) scanned the whole 5.2 GB heap for every shape
-- while one arm had no index (8,000,000 supports: 0.7 to 0.97 s warm, 52 to 111 s
-- cold), and with the package index present its page for a package of 1,600,000
-- supports still read 5.5 million buffers and wrote 0.94 GB of temp. The shipped
-- per-key ordered page, with this index, reads 5,958 buffers in 3.9 ms for that
-- package. The cve_id and advisory_id btrees are PR-B2b's migrations; the
-- repository_id arm is served by migration 346's (repository_id, fact_id) btree.
--
-- The predicate is the literal the read repeats on its package arm
-- (supplyChainImpactSupportsPackageIndexPredicate in the Go code), so the
-- planner can prove the partial index usable; TestSupplyChainImpactSupportsPackageIndexMatchesTheMigration
-- derives both from that one constant. (package_id, fact_id) leaves fact_id as
-- the index suffix, the order the keyset page reads.
--
-- CONCURRENTLY, so the build does not block impact writers. Single statement:
-- a concurrent build cannot run inside a transaction block. A failed build
-- leaves an INVALID index; the schema apply path drops it by name before
-- re-running this file (concurrentIndexNamesForInvalidCleanup). The schema Job
-- returns only after the build is valid, and the reducer image that reads the
-- index rolls out after the Job.
CREATE INDEX CONCURRENTLY IF NOT EXISTS supply_chain_impact_finding_supports_package_idx
    ON supply_chain_impact_finding_supports (package_id, fact_id)
    WHERE package_id <> '';
