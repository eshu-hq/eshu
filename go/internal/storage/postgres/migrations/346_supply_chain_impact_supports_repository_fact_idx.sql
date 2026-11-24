-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #10880 PR-B2c: the partial composite btree that serves the repository arm of
-- the supports reads (the security-alert reconciliation loader, #11198, and the
-- service-catalog vulnerability loader, #11199). Migration 338's
-- (repository_id) btree has no fact_id suffix, so every page of a repository's
-- supports read all of them and kept the first 500 by fact_id: 108,862 buffers
-- and about 110 ms a page for a repository of 160,000 supports, 320 pages to
-- drain it. QA holds repositories of about 152,000, 126,000 and 123,000 findings (a
-- 1/256 sample of live findings, read-only on the replica).
-- With (repository_id, fact_id) the page is an ordered index scan: 5,909 buffers
-- and 3.7 ms. Measured in docs/internal/evidence/10880-b2c/reader_plans.md.
--
-- The predicate is the literal both repository arms repeat
-- (supplyChainImpactSupportsRepositoryIndexPredicate in the Go code), so the
-- planner can prove the partial index usable; the schema test derives the
-- migration and the arms from that one constant. This index serves every
-- equality probe on repository_id that 338's btree serves, so 338's btree is
-- dropped by a later, separate migration once every reader is proven on this
-- one; this migration only adds.
--
-- CONCURRENTLY, so the build does not block impact writers. Single statement:
-- a concurrent build cannot run inside a transaction block. A failed build
-- leaves an INVALID index; the schema apply path drops it by name before
-- re-running this file (concurrentIndexNamesForInvalidCleanup). The schema Job
-- returns only after the build is valid, and the reducer image that reads the
-- index rolls out after the Job.
CREATE INDEX CONCURRENTLY IF NOT EXISTS supply_chain_impact_finding_supports_repository_fact_idx
    ON supply_chain_impact_finding_supports (repository_id, fact_id)
    WHERE repository_id <> '';
