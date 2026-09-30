-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #7142: begin-before-mutate admission watermark and fencing-token sequence for
-- the supply_chain_impact reducer writer. One admission row per
-- (scope_id, generation_id) records the highest fencing token any pass has been
-- admitted to write with. A pass whose own token is older is rejected before it
-- upserts or retracts anything -- see
-- go/internal/reducer/supplychain/core/writer_admission.go.
--
-- The watermark is a table rather than MAX(fencing_token) over fact_records
-- because a fresher pass that derived an EMPTY finding set leaves no row to take
-- a maximum over, and a stale pass would then be admitted and publish findings
-- the fresher evidence says do not exist. Every admitted pass writes its row,
-- including empty and partial ones.
CREATE TABLE IF NOT EXISTS supply_chain_impact_write_admission (
    scope_id TEXT NOT NULL,
    generation_id TEXT NOT NULL,
    fencing_token BIGINT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (scope_id, generation_id)
);

-- The token is a Postgres sequence value issued when a pass starts reading
-- evidence (SupplyChainImpactHandler.Handle), before the evidence load. Every
-- reducer replica calls nextval() against the same instance, so the value
-- reflects real issuance order and never depends on a host clock -- the same
-- reason aws_cloud_runtime_drift moved off a wall-clock token (migration 089).
-- Issuing when a pass begins reading evidence, rather than at commit, orders
-- passes by when they began reading, not by commit order: a pass that read its
-- evidence and then stalled cannot publish over a later one. It does not order
-- passes whose loads interleave, which converge on the next intent for the pair.
CREATE SEQUENCE IF NOT EXISTS supply_chain_impact_fencing_token_seq;

-- Seed the sequence above the highest token already admitted, so re-applying
-- this file on a live database can only advance the sequence, never regress it
-- below a value already issued to an in-flight or committed writer. Seeded from
-- the admission table only: every finding row written before this migration
-- carries fencing_token 0, every later non-zero token comes from this sequence,
-- and an admitted watermark is at least every row token of its
-- (scope_id, generation_id), so the table bounds fact_records and no scan of
-- fact_records (or index on it) is needed. This file re-applies on every
-- reducer start, so it must stay idempotent. The advance is a read then a
-- setval, so it is a repair for a sequence that lags the admitted watermark (a
-- restore or a reset of the sequence), not a live-safe operation while other
-- writers draw values.
DO $$
DECLARE
    watermark_floor BIGINT;
    current_last_value BIGINT;
BEGIN
    SELECT COALESCE(MAX(fencing_token), 0) INTO watermark_floor
    FROM supply_chain_impact_write_admission;
    SELECT last_value INTO current_last_value FROM supply_chain_impact_fencing_token_seq;
    IF watermark_floor > current_last_value THEN
        PERFORM setval('supply_chain_impact_fencing_token_seq', watermark_floor + 1, false);
    END IF;
END $$;
