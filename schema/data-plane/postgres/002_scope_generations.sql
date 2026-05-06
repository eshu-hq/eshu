CREATE TABLE IF NOT EXISTS scope_generations (
    generation_id TEXT PRIMARY KEY,
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    trigger_kind TEXT NOT NULL,
    freshness_hint TEXT NULL,
    observed_at TIMESTAMPTZ NOT NULL,
    ingested_at TIMESTAMPTZ NOT NULL,
    status TEXT NOT NULL,
    activated_at TIMESTAMPTZ NULL,
    superseded_at TIMESTAMPTZ NULL,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb
);

CREATE INDEX IF NOT EXISTS scope_generations_scope_idx
    ON scope_generations (scope_id, status, ingested_at DESC);

CREATE UNIQUE INDEX IF NOT EXISTS scope_generations_active_scope_idx
    ON scope_generations (scope_id)
    WHERE status = 'active';
