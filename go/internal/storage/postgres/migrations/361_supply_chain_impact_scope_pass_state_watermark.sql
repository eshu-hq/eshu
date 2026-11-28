-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- 361_supply_chain_impact_scope_pass_state_watermark.sql (#11613 PR1): the
-- holder freshness watermark. A full evidence-complete pass records its
-- fencing token and the generation the token describes, so a later reopen
-- can compare the origin pass's token against the last proven pass instead
-- of wall times (the Step-0 time proxy overstated proven completion: claim
-- time is not pass-completion time).
--
-- Additive and nullable, no default and no backfill: a column added without
-- a default is a catalog change only (no table rewrite), and a binary of
-- the previous release, which names its columns, reads and writes the table
-- unchanged. A scope with no full complete pass yet keeps NULLs, and the
-- shadow predicate fails closed on them: no watermark is never a skip.
-- Partial passes leave the watermark on the last complete truth, and
-- scoped-complete passes leave it untouched with the stuck state; only a
-- full complete pass moves it, under the same fencing-token guard and
-- generation fence as the stuck-state write (one statement in
-- scope_pass_state.go). A NEW migration file per issue #7002: never edit
-- an applied migration.
ALTER TABLE supply_chain_impact_scope_pass_state
    ADD COLUMN IF NOT EXISTS watermark_token BIGINT;
ALTER TABLE supply_chain_impact_scope_pass_state
    ADD COLUMN IF NOT EXISTS watermark_generation_id TEXT;
ALTER TABLE supply_chain_impact_scope_pass_state
    ADD COLUMN IF NOT EXISTS watermark_evidence_complete BOOLEAN;

COMMENT ON COLUMN supply_chain_impact_scope_pass_state.watermark_token IS
'Fencing token of the last full evidence-complete pass (#11613): the holder watermark. NULL means no proven pass yet.';
COMMENT ON COLUMN supply_chain_impact_scope_pass_state.watermark_generation_id IS
'Generation the watermark token describes (#11613): the shadow predicate requires it to equal the item generation.';
COMMENT ON COLUMN supply_chain_impact_scope_pass_state.watermark_evidence_complete IS
'True when the watermarking pass loaded its whole evidence set (#11613): the shadow predicate requires it.';
