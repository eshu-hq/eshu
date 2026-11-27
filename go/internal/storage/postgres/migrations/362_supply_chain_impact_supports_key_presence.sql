-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- 361_supply_chain_impact_supports_key_presence.sql (#9975, arbiter round-6
-- triple-guard verdict): the per-support key-presence bitmask the keyed probe
-- rewrite reads instead of the payload for the package triple
-- (package_id = 1, repository_id = 2, subject_digest = 4; advisory keys carry
-- no bits — the c0/advisory legs stay plain COALESCE equalities with no
-- guards, preserving the shipped leniency the round-5 construction broke).
--
-- The writer fills the column for every row it writes (it shares the
-- projection's SQL expression with the backfill, bits 1/2/4 only); a
-- dedicated fill backfills older rows under its own marker row and flips the
-- probe only after a disagreement-0 verification completes it. The DEFAULT 0
-- is fail-closed: a row the fill has not reached yet matches no '' triple
-- identifier under the new probe, instead of widening to missing-key rows.
--
-- Metadata-only: PostgreSQL 18 adds a constant-default SMALLINT without a
-- table rewrite and takes only a brief share lock for the catalog change.
-- A NEW migration file per issue #7002: never edit an applied migration.
ALTER TABLE IF EXISTS supply_chain_impact_finding_supports
    ADD COLUMN IF NOT EXISTS key_presence SMALLINT NOT NULL DEFAULT 0;
