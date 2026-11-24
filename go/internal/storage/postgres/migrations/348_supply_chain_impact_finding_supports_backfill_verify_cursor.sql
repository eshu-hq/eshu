-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- 348_supply_chain_impact_finding_supports_backfill_verify_cursor.sql (#10880,
-- arbiter decision 6): the resumable cursor of the supports backfill's
-- verification. The verification used to be two unbounded statements, each in
-- one snapshot, that could not resume and grew with the table (QA, 11.6M live
-- findings: the orphan statement up to 22.9 min, the missing count 45.2 min). It
-- now checks one bounded key interval per step and stores the end of the last
-- clean interval here, in (scope_id, generation_id, fact_id) order.
--
-- verify_sweep is the sweeps value the cursor belongs to. The cursor counts only
-- while verify_sweep equals sweeps: a restart (a verification that found a gap)
-- bumps sweeps, so a cursor left by an earlier sweep, or by an older binary whose
-- restart statement does not clear these columns, reads as "start over".
--
-- Additive and nullable: NULL is "no verification progress", every existing row
-- keeps its meaning, and a binary of the previous release, which names its
-- columns, reads and writes the row unchanged. A NEW migration file per issue
-- #7002: never edit an applied migration.
ALTER TABLE supply_chain_impact_finding_supports_backfill
    ADD COLUMN IF NOT EXISTS verify_cursor_scope_id TEXT,
    ADD COLUMN IF NOT EXISTS verify_cursor_generation_id TEXT,
    ADD COLUMN IF NOT EXISTS verify_cursor_fact_id TEXT,
    ADD COLUMN IF NOT EXISTS verify_sweep INTEGER;
