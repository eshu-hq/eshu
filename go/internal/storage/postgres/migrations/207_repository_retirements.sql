-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- Operator-driven repository retirement tombstones (#7766).
--
-- One row per retirement request. The row is the durable scope-level marker
-- the phase-1 fence, the purge runner, and the read surfaces share:
--   - an open row (readmitted_at IS NULL) in any state except 'complete'
--     refuses ingest for the scope (commit gate, webhook handoff,
--     recover-generations, reindex);
--   - a 'complete' row is the tombstone history. The next ingest of the same
--     repository re-admits it as a fresh first generation and stamps
--     readmitted_at; there is no undo.
--
-- The table carries no foreign key to ingestion_scopes on purpose: the
-- tombstone outlives the scope row, which phase 3 deletes.
CREATE TABLE IF NOT EXISTS repository_retirements (
    retirement_id TEXT PRIMARY KEY,
    repo_id TEXT NOT NULL,
    scope_id TEXT NOT NULL,
    repo_slug_key TEXT NOT NULL DEFAULT '',
    state TEXT NOT NULL CHECK (state IN ('pending','running','blocked','repairing_graph','complete','failed')),
    phase TEXT NOT NULL CHECK (phase IN ('fenced','quiesce','barrier','graph_retract','purge','census','finalize','done')),
    graph_step_cursor INTEGER NOT NULL DEFAULT 0,
    blocked_reason TEXT NOT NULL DEFAULT '' CHECK (blocked_reason IN
      ('','projector_lease_live','reducer_lease_live','shared_lease_live',
       'scope_lock_busy','claim_fence_busy','graph_unavailable')),
    failure_class TEXT NOT NULL DEFAULT '',
    failure_details JSONB NOT NULL DEFAULT '{}'::jsonb,
    reason_code TEXT NOT NULL CHECK (reason_code IN ('operator_retired')),
    reason_hash TEXT NOT NULL,
    actor_class TEXT NOT NULL,
    actor_id_hash TEXT NOT NULL DEFAULT '',
    idempotency_key_hash TEXT NOT NULL,
    scope_id_hash TEXT NOT NULL,
    generation_ids_hash TEXT NOT NULL,
    generations_fenced INTEGER NOT NULL DEFAULT 0,
    rows_deleted JSONB NOT NULL DEFAULT '{}'::jsonb,
    graph_nodes_deleted BIGINT NOT NULL DEFAULT 0,
    graph_relationships_deleted BIGINT NOT NULL DEFAULT 0,
    intent_barrier_at TIMESTAMPTZ NULL,
    lease_owner TEXT NULL,
    claim_until TIMESTAMPTZ NULL,
    attempt_count INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    requested_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    retired_at TIMESTAMPTZ NULL,
    readmitted_at TIMESTAMPTZ NULL
);

-- Exactly one open row per repository: the commit gate's lookup predicate
-- (WHERE repo_id = $1 AND readmitted_at IS NULL) is served by this partial
-- index (measured: Index Scan on the repo_id-leading partial index).
CREATE UNIQUE INDEX IF NOT EXISTS repository_retirements_open_repo_idx
    ON repository_retirements (repo_id) WHERE readmitted_at IS NULL;

-- The runner's due-work poll.
CREATE INDEX IF NOT EXISTS repository_retirements_runnable_idx
    ON repository_retirements (next_attempt_at)
    WHERE state IN ('pending','running','blocked','repairing_graph');

-- Scope lookups (reindex refusal, candidate listing join).
CREATE INDEX IF NOT EXISTS repository_retirements_scope_idx
    ON repository_retirements (scope_id);

-- Idempotency: a repeated operator request with the same key returns the
-- existing row instead of opening a second retirement.
CREATE UNIQUE INDEX IF NOT EXISTS repository_retirements_idempotency_idx
    ON repository_retirements (repo_id, idempotency_key_hash) WHERE readmitted_at IS NULL;
