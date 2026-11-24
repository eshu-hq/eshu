-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- The keyed winners resweep probe (buildWinnersKeyedActiveFactsFromSupportsSQL)
-- matches, per dirty identifier, the package triple plus the coalesced c0
-- (cve_id, else advisory_id) and advisory_id payload expressions on the supports
-- table. Measured on the QA replica
-- (docs/internal/evidence/10880-prc-c1-replica-plans.md): with no supports index on
-- those expressions the probe is a Parallel Seq Scan on supports at estimated cost
-- 3,047,857, against a 1,289 ms cold / 59 ms warm index lookup on fact_records
-- through fact_records_supply_chain_impact_winners_keyed_idx (migration 291).
--
-- This mirrors migration 291's five key expressions byte for byte on the supports
-- payload, then fact_id, so the probe statement stays the legacy statement with the
-- row source swapped and every equality is an index condition. The expressions
-- must stay equivalent to the probe's or the planner cannot use the index;
-- TestKeyedSupportsIndexCoversTheKeyedProbe derives both from the shipped
-- statements. No predicate: a support exists only for a live finding fact.
--
-- A lab build (TestSupplyChainImpactSupportsKeyedProbeIndexBuildLabLive, 300,000
-- rows with 9,000-byte compressible payloads) leaves the index valid and ready in
-- about a second; the QA build, which detoasts every payload, is not measured
-- here. It is not applied inside W1b.
--
-- CONCURRENTLY, so the build does not block impact writers. Single statement: a
-- concurrent build cannot run inside a transaction block. A failed build leaves an
-- INVALID index; the schema apply path drops it by name before re-running this
-- file (concurrentIndexNamesForInvalidCleanup).
CREATE INDEX CONCURRENTLY IF NOT EXISTS supply_chain_impact_finding_supports_keyed_probe_idx
    ON supply_chain_impact_finding_supports (
        (payload->>'package_id'),
        (payload->>'repository_id'),
        (payload->>'subject_digest'),
        (COALESCE(NULLIF(payload->>'cve_id', ''), NULLIF(payload->>'advisory_id', ''), '')),
        (COALESCE(payload->>'advisory_id', '')),
        fact_id ASC
    );
