-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- 362_supply_chain_impact_supports_keyed_triple_guard_idx.sql (#9975, arbiter
-- round-6 triple-guard verdict): the covering index for the rewritten keyed
-- probe (buildWinnersKeyedNarrowKeyPresenceSQL). The probe equates the plain
-- supports columns for the package triple plus the coalesced c0
-- (cve_id, else advisory_id) and advisory_id plain-column expressions, with
-- triple-only key-presence guards; every column the probe reads — the five
-- equality columns, key_presence for the guards, cve_id for the c0
-- expression, and the (fact_id, scope_id, generation_id) output — is stored
-- in this index, so the probe is an Index Only Scan with 0 heap fetches
-- (falsifier: 456 buffers whole-chunk over 125 idents vs 3577 shipped,
-- single-ident 5 buffers; docs/internal/design/9975-triple-guards.md).
--
-- This mirrors the probe's five key expressions byte for byte on the plain
-- columns, then INCLUDEs the output and guard inputs. The expressions must
-- stay equivalent to the probe's or the planner cannot use every equality as
-- an index condition; TestKeyedTripleGuardIndexCoversTheKeyedProbe derives
-- both from the shipped statements. No predicate: a support exists only for
-- a live finding fact. Migration 354 stays: the pre-marker fallback probe
-- still reads the payload expressions through it.
--
-- CONCURRENTLY, so the build does not block impact writers. Single statement: a
-- concurrent build cannot run inside a transaction block. A failed build leaves an
-- INVALID index; the schema apply path drops it by name before re-running this
-- file (concurrentIndexNamesForInvalidCleanup).
CREATE INDEX CONCURRENTLY IF NOT EXISTS supply_chain_impact_finding_supports_keyed_triple_guard_idx
    ON supply_chain_impact_finding_supports (
        (package_id),
        (repository_id),
        (subject_digest),
        (COALESCE(NULLIF(cve_id, ''), NULLIF(advisory_id, ''), '')),
        (advisory_id)
    )
    INCLUDE (fact_id, scope_id, generation_id, key_presence, cve_id);
