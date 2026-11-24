-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- 350_supply_chain_impact_finding_supports_stable_fact_key.sql (#10880 PR-C1 a):
-- the key that links a finding across generations, copied onto the support so
-- the writer's stored read can leave fact_records. The upsert fills it and sets
-- it on a row it rewrites anyway, but keeps it out of the write-on-change guard,
-- so a replay does not rewrite a row only to fill it. A support written before
-- this column existed keeps NULL, and the stored read falls back to the fact's
-- own key for those rows (COALESCE over a primary-key join).
--
-- Additive and nullable, no default and no backfill: a column added without a
-- default is a catalog change only (no table rewrite), and a binary of the
-- previous release, which names its columns, reads and writes the table
-- unchanged. The ALTER takes ACCESS EXCLUSIVE on the supports table for the
-- moment the catalog row changes; it waits behind open writer transactions and
-- the runner's lock_timeout bounds how long. A NEW migration file per issue
-- #7002: never edit an applied migration.
ALTER TABLE supply_chain_impact_finding_supports
    ADD COLUMN IF NOT EXISTS stable_fact_key TEXT;
