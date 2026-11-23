-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #10880 PR-B2c: the partial btree that serves the package arm of the
-- security-alert reconciliation read of the supports table (#11198). That read
-- filters supports by an OR of four arms (repository_id, package_id, cve_id,
-- advisory_id); an OR is only indexable when every arm is, so without a
-- package_id index the planner scans the whole table for every shape
-- (8,000,000 supports, 5.2 GB heap: 0.7 to 0.97 s warm, 52 to 111 s cold; with
-- the index every shape is a BitmapOr over four index scans, 1.4 to 5.6 ms warm).
-- Measured in docs/internal/evidence/10880-b2c/reader_plans.md. The cve_id and
-- advisory_id btrees are PR-B2b's migrations; repository_id has its index from
-- migration 338.
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
