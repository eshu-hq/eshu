-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #7142: begin-before-mutate admission watermark and fencing-token sequence for
-- the supply_chain_impact reducer writer. One admission row per
-- (scope_id, generation_id) records the highest fencing token any pass has been
-- admitted to write with. A pass whose own token is older is rejected before it
-- upserts or retracts anything -- see
-- go/internal/reducer/supplychain/core/writer_retract.go.
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

-- Seed the sequence above the highest token already admitted. The bootstrap
-- ledger records this file after its first apply and skips it from then on
-- (schema_bootstrap_lock.go), so in the service runtimes this block runs once,
-- when the admission table is empty, and is not a recurring repair; only
-- "eshu local" (localsupervisor applyLocalBootstrap) applies the definitions
-- untracked on every start, so there it runs each start as a forward-only
-- repair. It stays idempotent and forward-only so an operator can run it by
-- hand after restoring or resetting the sequence:
-- a token that comes back equal to an admitted watermark is admitted as an
-- identical re-execution even when it belongs to a different pass, so never
-- reset this sequence on its own. Seeded from the admission table only: every
-- finding row written before this migration carries fencing_token 0, every later
-- non-zero token comes from this sequence, and an admitted watermark is at least
-- every row token of its (scope_id, generation_id), so the table bounds
-- fact_records and no scan of fact_records (or index on it) is needed. The
-- advance is a read then a setval, so it is not live-safe while other writers
-- draw values: a nextval between the read and the setval is rewound and can be
-- reissued, so two passes could hold the same token. Run it by hand only with
-- every reducer replica stopped, and restart them afterwards. The guard also
-- advances when the sequence equals the watermark but has not been called, since
-- the next nextval would hand out the admitted token again.
DO $$
DECLARE
    watermark_floor BIGINT;
    current_last_value BIGINT;
    current_is_called BOOLEAN;
BEGIN
    SELECT COALESCE(MAX(fencing_token), 0) INTO watermark_floor
    FROM supply_chain_impact_write_admission;
    SELECT last_value, is_called INTO current_last_value, current_is_called
    FROM supply_chain_impact_fencing_token_seq;
    IF watermark_floor > current_last_value
        OR (watermark_floor > 0 AND watermark_floor = current_last_value AND NOT current_is_called) THEN
        PERFORM setval('supply_chain_impact_fencing_token_seq', watermark_floor + 1, false);
    END IF;
END $$;
