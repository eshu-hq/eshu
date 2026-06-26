CREATE TABLE IF NOT EXISTS shared_projection_intents (
    intent_id TEXT PRIMARY KEY,
    projection_domain TEXT NOT NULL,
    partition_key TEXT NOT NULL,
    scope_id TEXT NOT NULL DEFAULT '',
    acceptance_unit_id TEXT NOT NULL DEFAULT '',
    repository_id TEXT NOT NULL,
    source_run_id TEXT NOT NULL,
    generation_id TEXT NOT NULL,
    partition_hash NUMERIC(20, 0) NULL,
    payload JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    completed_at TIMESTAMPTZ NULL
);
ALTER TABLE shared_projection_intents
    ADD COLUMN IF NOT EXISTS scope_id TEXT NOT NULL DEFAULT '';
ALTER TABLE shared_projection_intents
    ADD COLUMN IF NOT EXISTS acceptance_unit_id TEXT NOT NULL DEFAULT '';
ALTER TABLE shared_projection_intents
    ADD COLUMN IF NOT EXISTS partition_hash NUMERIC(20, 0) NULL;
ALTER TABLE shared_projection_intents
    ADD COLUMN IF NOT EXISTS is_refresh_intent BOOLEAN NOT NULL
    GENERATED ALWAYS AS (COALESCE(payload->>'action' = 'refresh', false)) STORED;
CREATE INDEX IF NOT EXISTS shared_projection_intents_repo_run_idx
    ON shared_projection_intents (repository_id, source_run_id, projection_domain, created_at);
CREATE INDEX IF NOT EXISTS shared_projection_intents_acceptance_lookup_idx
    ON shared_projection_intents (scope_id, acceptance_unit_id, source_run_id, projection_domain, created_at);
CREATE INDEX IF NOT EXISTS shared_projection_intents_acceptance_partition_pending_idx
    ON shared_projection_intents (scope_id, acceptance_unit_id, source_run_id, projection_domain, partition_key, created_at, intent_id)
    WHERE completed_at IS NULL;
CREATE INDEX IF NOT EXISTS shared_projection_intents_domain_partition_pending_idx
    ON shared_projection_intents (projection_domain, created_at, intent_id)
    WHERE completed_at IS NULL AND partition_hash IS NOT NULL;
-- Drop the #3451 index whose created_at-primary order cannot serve the
-- refresh-first-primary query (#3474).
DROP INDEX IF EXISTS shared_projection_intents_domain_partition_refresh_first_idx;
CREATE INDEX IF NOT EXISTS shared_projection_intents_domain_partition_refresh_primary_idx
    ON shared_projection_intents (
        projection_domain,
        is_refresh_intent DESC,
        created_at ASC,
        intent_id ASC
    )
    WHERE completed_at IS NULL AND partition_hash IS NOT NULL;
-- Same refresh-first-primary order for the legacy NULL-partition_hash lane so
-- unhashed refresh intents are not starved during a migration window (#3474).
DROP INDEX IF EXISTS shared_projection_intents_domain_unhashed_pending_idx;
CREATE INDEX IF NOT EXISTS shared_projection_intents_domain_unhashed_refresh_primary_idx
    ON shared_projection_intents (
        projection_domain,
        is_refresh_intent DESC,
        created_at ASC,
        intent_id ASC
    )
    WHERE completed_at IS NULL AND partition_hash IS NULL;
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
