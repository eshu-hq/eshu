-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- Cross-service commit fence for claim-plane commits (#9376). A collector
-- whose claim lives in the extracted claim plane has no core claim row, so
-- the ingestion commit cannot heartbeat it on the fact transaction. Instead
-- it pins the claim on the plane before BEGIN and records here, inside the
-- fact transaction, which claim and fencing token committed the generation
-- for that work item. A commit whose token is below the stored token, or
-- equal to it under a different claim, is refused, so a writer that was
-- reaped and reclaimed at a higher token cannot commit after the reclaim.
--
-- The key is (generation_id, work_item_id): fencing tokens are per work
-- item, and terraform-state can commit one resolved generation from two
-- work items, so tokens of different items are never compared. Only the
-- claim-plane path writes rows; the in-tree path (core claim row heartbeat
-- on the same transaction) never touches this table. The cascade lookup
-- uses the primary key's leading column. Forward-only and inert when no
-- claim plane is configured.
--
-- Lock window: the foreign key takes SHARE ROW EXCLUSIVE on scope_generations
-- (checked on PostgreSQL 18 with pg_locks inside the CREATE TABLE
-- transaction), which waits for and blocks writers of that table. The new
-- table is empty, so the lock is held only for the catalog work, and the
-- schema runner bounds the wait with its 5 s lock_timeout and retries inside
-- its 3 min lock budget, failing the bootstrap loudly with 55P03 if the
-- table stays busy. Migration 163 (producer_activation_obligations) added
-- the same foreign key to scope_generations the same way.
CREATE TABLE IF NOT EXISTS generation_commit_fences (
    generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE,
    work_item_id TEXT NOT NULL,
    scope_id TEXT NOT NULL,
    claim_id TEXT NOT NULL,
    fencing_token BIGINT NOT NULL CHECK (fencing_token > 0),
    commit_id TEXT NOT NULL,
    committed_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (generation_id, work_item_id)
);
