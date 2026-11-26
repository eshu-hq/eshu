-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- 359_supply_chain_impact_winners_enrichment_first_carry.sql (#11466): the
-- durable set-once-per-value first-carry stamp for enrichment-arrival-to-rescore
-- (first_epss, cisa_kev; part of #9209). One row per (canonical_key,
-- enrichment_source): the first instant the winners-visible row carried that
-- source's current value (the new EPSS score_date, or KnownExploited) and the
-- value's identity.
--
-- The resweep claims stamps with INSERT ... SELECT ... ON CONFLICT DO UPDATE
-- restricted to value_identity IS DISTINCT, inside the resweep transaction
-- after the rebuild (full path) or the keyed apply (keyed path), so a
-- rewrite or a same-value re-ingest that resolves the same value moves
-- nothing: the stamp is set once per value episode, and only a value change
-- (rescore, KEV flip) advances it. A KEV-false-by-default carry has no KEV
-- fact behind it; the stamp mechanism records the false episode anyway so a
-- later flip back to true moves the stamp observably, while the cohort
-- classifier excludes anchorless samples from lag measurement.
--
-- The row has no scope, generation, or winner foreign key on purpose: the
-- stamp must outlive retention of any one generation and any one winner
-- version, so a restored-after-retraction finding keeps its original
-- set-once stamp (same shape as the #11031 first-seen stamp, migration
-- 357, which this table does not reuse: that table's key space is the
-- stable fact key and predates winners visibility). Retraction never deletes
-- the row. Orphan rows are tiny (two per finding identity at most) and
-- self-describing for QA per-source lag measurement.
--
-- Additive: a new table nothing in any previous release names. A binary of
-- the previous release reads and writes every existing table unchanged; a
-- rollback leaves this table unused. A NEW migration file per issue #7002:
-- never edit an applied migration.
--
-- Numbered 359: 358 is held by the open #10880 write-stop lane (#11476),
-- which rolled first.
CREATE TABLE IF NOT EXISTS supply_chain_impact_winners_enrichment_first_carry (
    canonical_key TEXT NOT NULL,
    enrichment_source TEXT NOT NULL,
    value_identity TEXT NOT NULL,
    first_carried_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (canonical_key, enrichment_source),
    CONSTRAINT supply_chain_impact_winners_enrichment_first_carry_source_check
        CHECK (enrichment_source IN ('first_epss', 'cisa_kev'))
);

COMMENT ON TABLE supply_chain_impact_winners_enrichment_first_carry IS
    '#11466: earliest durable resweep instant per winners canonical key and enrichment source at which the winners-visible row carried the stamped value; claimed set-once per value by the resweep carry writer.';
