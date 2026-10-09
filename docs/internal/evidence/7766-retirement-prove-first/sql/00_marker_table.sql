-- Proposed table from the design (section 1), applied verbatim to the scratch DB only.
CREATE TABLE IF NOT EXISTS repository_retirements (
    retirement_id TEXT PRIMARY KEY,
    repo_id TEXT NOT NULL,
    scope_id TEXT NOT NULL,
    repo_slug_key TEXT NOT NULL DEFAULT '',
    state TEXT NOT NULL CHECK (state IN ('pending','running','blocked','repairing_graph','complete','failed')),
    phase TEXT NOT NULL CHECK (phase IN ('fenced','graph_retract','purge','finalize','done')),
    graph_step_cursor INTEGER NOT NULL DEFAULT 0,
    blocked_reason TEXT NOT NULL DEFAULT '' CHECK (blocked_reason IN
      ('','projector_lease_live','shared_lease_horizon','scope_lock_busy','graph_unavailable')),
    failure_class TEXT NOT NULL DEFAULT '',
    reason_code TEXT NOT NULL CHECK (reason_code IN ('operator_retired')),
    reason_hash TEXT NOT NULL, actor_class TEXT NOT NULL, actor_id_hash TEXT NOT NULL DEFAULT '',
    idempotency_key_hash TEXT NOT NULL, scope_id_hash TEXT NOT NULL,
    generation_ids_hash TEXT NOT NULL, generations_fenced INTEGER NOT NULL,
    rows_deleted JSONB NOT NULL DEFAULT '{}'::jsonb,
    graph_nodes_deleted BIGINT NOT NULL DEFAULT 0,
    graph_relationships_deleted BIGINT NOT NULL DEFAULT 0,
    shared_lease_horizon TIMESTAMPTZ NULL,
    lease_owner TEXT NULL, claim_until TIMESTAMPTZ NULL,
    attempt_count INTEGER NOT NULL DEFAULT 0, next_attempt_at TIMESTAMPTZ NOT NULL,
    requested_at TIMESTAMPTZ NOT NULL, updated_at TIMESTAMPTZ NOT NULL,
    retired_at TIMESTAMPTZ NULL, readmitted_at TIMESTAMPTZ NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS repository_retirements_open_repo_idx
    ON repository_retirements (repo_id) WHERE readmitted_at IS NULL;
CREATE INDEX IF NOT EXISTS repository_retirements_runnable_idx
    ON repository_retirements (next_attempt_at)
    WHERE state IN ('pending','running','blocked','repairing_graph');
CREATE INDEX IF NOT EXISTS repository_retirements_scope_idx ON repository_retirements (scope_id);
