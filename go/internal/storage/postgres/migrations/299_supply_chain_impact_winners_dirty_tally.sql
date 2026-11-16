-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #10767 (NVD baseline trip line inflow): the winners maintainer drains
-- supply_chain_impact_winners_dirty_log with DELETE, so a count over the log
-- reads the rows that survived the drain, not the rows that arrived. A busy
-- hour that the maintainer keeps up with reads as quiet and the trip opens
-- late. This table keeps what the DELETE would otherwise erase: how many
-- NVD-proxy rows (the ruled definition of eshu-hq/eshu#10728 decision 3:
-- fact rows whose advisory_id starts with CVE-) and NVD scope rows (a baseline
-- commit's pointer flip, scope_id under vuln-intel://nvd/) were drained, keyed
-- by the minute they were captured in.
--
-- The tally is written by the two maintainer DELETE statements
-- (deleteWinnersKeyedAppliedDirtySQL and deleteWinnersKeyedAllDirtySQL), in the
-- same statement as the DELETE and therefore in the same transaction, which
-- runs under the single-flight resweep lock. A rolled-back tick changes
-- neither the log nor the tally, and the trip sampler adds the tally to the
-- rows still in the log inside one statement (one snapshot), so every
-- captured row is counted exactly once: in the log until the DELETE commits,
-- in the tally after. No capture statement changes: the writers, the
-- ingestion_scopes triggers and the scanner triggers never see this table.
--
-- Only rows captured in the trailing two hours of the drain's own clock are
-- tallied (a 58-hour-old backlog chunk writes nothing), and the same statement
-- removes buckets older than three hours, so the table holds a few hundred
-- rows at most. The sampler reads the trailing hour; the bound is the sampling
-- horizon plus a margin for clock skew between the coordinator and the
-- database. One row per capture minute with a primary key on it: the only
-- writer is the maintainer, so rows never contend.
CREATE TABLE IF NOT EXISTS supply_chain_impact_winners_dirty_tally (
    bucket TIMESTAMPTZ PRIMARY KEY,
    nvd_proxy_rows BIGINT NOT NULL DEFAULT 0 CHECK (nvd_proxy_rows >= 0),
    nvd_scope_rows BIGINT NOT NULL DEFAULT 0 CHECK (nvd_scope_rows >= 0)
);
