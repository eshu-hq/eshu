-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- 351_supply_chain_impact_retracted_finding_keys.sql (#10880 PR-C1 c): what a
-- retraction removed from a (scope, generation). The impact writer's recovery
-- reads (the CVE ids and the advisory-group ids a crashed attempt owes) used to
-- find retracted findings as tombstoned fact rows. PR-C stops tombstoning facts,
-- so the retraction records the keys it removes here, in the same statement and
-- transaction as the tombstone, and the recovery reads take them from here.
--
-- One row per distinct retracted key of a pair: the CVE id, the advisory id and
-- the advisory-group member set (the provenance JSON text), plus a sha256 of the
-- three so the primary key stays small. The primary key leads with generation_id,
-- which is also what the generation foreign key's cascade probes: a generation
-- delete (retention, repository retirement) removes its rows with DELETE FROM
-- ONLY <table> WHERE generation_id = $1, answered from the primary key's leading
-- column, so no separate generation index is needed. The foreign key cascades as
-- migration 338's does, so retention and retirement remove these rows with their
-- generation.
--
-- Additive: a new table nothing reads in the release before the recovery reads
-- change, and nothing in an older binary names it. A NEW migration file per issue
-- #7002: never edit an applied migration.
CREATE TABLE IF NOT EXISTS supply_chain_impact_retracted_finding_keys (
    generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE,
    scope_id      TEXT NOT NULL,
    key_hash      TEXT NOT NULL,
    cve_id        TEXT NOT NULL DEFAULT '',
    advisory_id   TEXT NOT NULL DEFAULT '',
    members       TEXT NOT NULL DEFAULT '[]',
    PRIMARY KEY (generation_id, scope_id, key_hash)
);
