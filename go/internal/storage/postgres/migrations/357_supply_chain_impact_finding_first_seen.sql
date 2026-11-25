-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- 357_supply_chain_impact_finding_first_seen.sql (#11031 criterion 6): the
-- durable set-once first-seen stamp per finding stable key. One row per
-- stable fact key across all scopes and generations: the earliest reducer
-- write instant (server clock at write time) and the publish-source credit
-- naming the collector source whose observation supplied the finding's
-- AdvisoryPublishedAt (osv, nvd, ghsa, cve_list, ...).
--
-- The writer claims stamps with INSERT ... ON CONFLICT DO NOTHING inside the
-- write transaction after fencing admission, so concurrent first claims of
-- one key collapse on the primary key (exactly one row survives, every
-- claimant resolves to it) and a rejected stale pass never stamps.
-- Retraction never deletes the row, so a restored-after-retraction finding
-- keeps its original set-once stamp. The row has no scope or generation
-- foreign key on purpose: the stamp must outlive retention of any one
-- generation, and orphan rows are tiny (one per finding identity) and
-- self-describing for QA per-source lag measurement.
--
-- Additive: a new table nothing in any previous release names. A binary of
-- the previous release reads and writes every existing table unchanged; a
-- rollback leaves this table unused. A NEW migration file per issue #7002:
-- never edit an applied migration.
CREATE TABLE IF NOT EXISTS supply_chain_impact_finding_first_seen (
    stable_fact_key TEXT PRIMARY KEY,
    first_seen_at TIMESTAMPTZ NOT NULL,
    publish_source TEXT NOT NULL DEFAULT '',
    claimed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE supply_chain_impact_finding_first_seen IS
    '#11031: earliest durable reducer write instant per finding stable key, with the publish-source credit; claimed set-once by the impact writer.';
