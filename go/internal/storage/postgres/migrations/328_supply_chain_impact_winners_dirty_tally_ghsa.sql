-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #11078 (GHSA inflow split for the NVD baseline trip line): the winners
-- dirty tally (migration 299) counts NVD-proxy rows (fact rows whose
-- advisory_id starts with CVE-) and NVD scope rows, but GHSA is ~76% of
-- inflow and entirely uncounted, so the NVD/GHSA split stays NOT_CHECKED.
-- This migration adds the GHSA-proxy bucket column: fact rows whose
-- advisory_id starts with GHSA-, tallied by the same four maintainer DELETE
-- statements in the same transaction and read by the trip sampler beside
-- the ruled CVE-% rate. The latch folds only the ruled fields; the GHSA
-- rate is reported, never tripped on.
--
-- The column rides the table's existing contract: one row per capture
-- minute, written only by the maintainer drain, pruned past three hours,
-- and a drained row counts every mark it collapsed (dirty_version sums).
ALTER TABLE supply_chain_impact_winners_dirty_tally
    ADD COLUMN IF NOT EXISTS ghsa_proxy_rows BIGINT NOT NULL DEFAULT 0 CHECK (ghsa_proxy_rows >= 0);
