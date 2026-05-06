CREATE TABLE IF NOT EXISTS ingestion_scopes (
    scope_id TEXT PRIMARY KEY,
    scope_kind TEXT NOT NULL,
    source_system TEXT NOT NULL,
    source_key TEXT NOT NULL,
    parent_scope_id TEXT NULL,
    collector_kind TEXT NOT NULL,
    partition_key TEXT NOT NULL,
    observed_at TIMESTAMPTZ NOT NULL,
    ingested_at TIMESTAMPTZ NOT NULL,
    status TEXT NOT NULL,
    active_generation_id TEXT NULL,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb
);

CREATE INDEX IF NOT EXISTS ingestion_scopes_source_idx
    ON ingestion_scopes (
        source_system,
        scope_kind,
        partition_key,
        observed_at DESC
    );

CREATE INDEX IF NOT EXISTS ingestion_scopes_parent_idx
    ON ingestion_scopes (parent_scope_id, observed_at DESC);

CREATE UNIQUE INDEX IF NOT EXISTS ingestion_scopes_active_generation_idx
    ON ingestion_scopes (active_generation_id)
    WHERE active_generation_id IS NOT NULL;
