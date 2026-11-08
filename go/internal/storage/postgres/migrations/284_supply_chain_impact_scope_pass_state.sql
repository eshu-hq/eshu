-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #10389: durable per-scope supply-chain impact pass state. The #9778
-- headroom tracker is process-local by design: a scope another replica
-- completed stays partial here until this process handles it again, and a
-- restart empties it. The status degraded reason for a scope partial for a
-- day needs the opposite: per-scope pass state every replica and restart
-- agrees on. One row per scope, written in the same write transaction as
-- the finding upsert and the #7142 admission, so the state and the rows it
-- describes commit or roll back together:
--
-- - an admitted partial pass sets partial_evidence with its causes and
--   stamps partial_since when the scope was not already partial, preserving
--   the original stuck time across further partial passes;
-- - an admitted complete pass clears partial_evidence, its causes, and
--   partial_since, lifting the degraded reason;
-- - the fencing-token guard (same <= rule as the admission) keeps an older
--   admitted pass from overwriting a newer pass's state when passes for
--   one scope interleave across generations.
--
-- Write cost: one primary-key upsert per admitted pass, in a transaction
-- that already upserts the finding rows. Read cost: the status reader
-- serves stuck rows (partial_evidence true, one row per stuck scope) live;
-- no stored-summary caching, so no summary digest changes. No secondary
-- index: the table holds one row per scope and both the upsert and the
-- status read key on scope_id.
CREATE TABLE IF NOT EXISTS supply_chain_impact_scope_pass_state (
    scope_id TEXT PRIMARY KEY,
    fencing_token BIGINT NOT NULL,
    partial_evidence BOOLEAN NOT NULL,
    partial_causes TEXT[] NOT NULL DEFAULT '{}',
    partial_since TIMESTAMPTZ,
    last_pass_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);
