CREATE TABLE IF NOT EXISTS shared_projection_intents (
    intent_id TEXT PRIMARY KEY,
    projection_domain TEXT NOT NULL,
    partition_key TEXT NOT NULL,
    scope_id TEXT NOT NULL DEFAULT '',
    acceptance_unit_id TEXT NOT NULL DEFAULT '',
    repository_id TEXT NOT NULL,
    source_run_id TEXT NOT NULL,
    generation_id TEXT NOT NULL,
    payload JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    completed_at TIMESTAMPTZ NULL
);
ALTER TABLE shared_projection_intents
    ADD COLUMN IF NOT EXISTS scope_id TEXT NOT NULL DEFAULT '';
ALTER TABLE shared_projection_intents
    ADD COLUMN IF NOT EXISTS acceptance_unit_id TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS shared_projection_intents_repo_run_idx
    ON shared_projection_intents (repository_id, source_run_id, projection_domain, created_at);
CREATE INDEX IF NOT EXISTS shared_projection_intents_acceptance_lookup_idx
    ON shared_projection_intents (scope_id, acceptance_unit_id, source_run_id, projection_domain, created_at);
CREATE INDEX IF NOT EXISTS shared_projection_intents_pending_idx
    ON shared_projection_intents (projection_domain, completed_at, created_at);

CREATE TABLE IF NOT EXISTS shared_projection_partition_leases (
    projection_domain TEXT NOT NULL,
    partition_id INTEGER NOT NULL,
    partition_count INTEGER NOT NULL,
    lease_owner TEXT,
    lease_expires_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (projection_domain, partition_id, partition_count)
);
