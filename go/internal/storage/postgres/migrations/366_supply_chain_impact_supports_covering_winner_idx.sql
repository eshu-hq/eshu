-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- 366_supply_chain_impact_supports_covering_winner_idx.sql (#11120 lane A):
-- the covering winner index for the TOP-1-per-key probe rewrite. The keyed
-- tick's marker-gated probe equates the package triple plus the coalesced c0
-- (cve_id, else advisory_id) and advisory_id plain-column expressions with
-- the triple-guard key-presence legs, then ranks per canonical key by
-- priority_score DESC, has_payload_finding_id DESC, fact_id ASC. The index
-- carries that identifier prefix plus the complete ranking order as KEY
-- columns, so the probe is an Index Only Scan with no Sort node for the
-- winner step. has_payload_finding_id must stay a key column: an INCLUDE
-- column cannot serve ordering. cve_id rides INCLUDE (not key) because the
-- probe references it inside the c0 expression, and an expression index
-- does not count as providing its base columns for index-only scans --
-- without it the planner refuses the index-only path. INCLUDE carries at
-- minimum (scope_id, generation_id) plus the guard and filter inputs
-- (key_presence, cve_id). All ranking inputs are plain columns; the grouping
-- key is the writer-filled canonical_key, proven byte-equal to the
-- payload-derived CONCAT_WS by the lane differential.
--
-- Measured on a QA-shaped scratch replica (14,021 supports rows):
-- 4,063,232 bytes (~290 B/row, ~3.8 GB extrapolated at 13.0M QA rows, inside
-- the 5-8 GB estimate); exact QA snapshot size and build time are PR proof.
-- Migration 354 stays the pre-marker fallback probe and migration 363 stays
-- for the chunked-full path; index retirement is a follow-up after Gate B.
--
-- CONCURRENTLY, so the build does not block impact writers. Single statement: a
-- concurrent build cannot run inside a transaction block. A failed build leaves an
-- INVALID index; the schema apply path drops it by name before re-running this
-- file (concurrentIndexNamesForInvalidCleanup).
CREATE INDEX CONCURRENTLY IF NOT EXISTS supply_chain_impact_finding_supports_covering_winner_idx
    ON supply_chain_impact_finding_supports (
        (package_id),
        (repository_id),
        (subject_digest),
        (COALESCE(NULLIF(cve_id, ''), NULLIF(advisory_id, ''), '')),
        (advisory_id),
        canonical_key,
        priority_score DESC,
        has_payload_finding_id DESC,
        fact_id ASC
    )
    INCLUDE (scope_id, generation_id, key_presence, cve_id);
