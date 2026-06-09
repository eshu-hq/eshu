CREATE TABLE IF NOT EXISTS service_materialization_generations (
    generation_id TEXT PRIMARY KEY,
    service_id TEXT NOT NULL,
    trigger_kind TEXT NOT NULL,
    source_intent_id TEXT NULL,
    observed_at TIMESTAMPTZ NOT NULL,
    ingested_at TIMESTAMPTZ NOT NULL,
    status TEXT NOT NULL,
    activated_at TIMESTAMPTZ NULL,
    superseded_at TIMESTAMPTZ NULL,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb
);

CREATE INDEX IF NOT EXISTS service_materialization_generations_service_idx
    ON service_materialization_generations (service_id, status, ingested_at DESC);

CREATE INDEX IF NOT EXISTS service_materialization_generations_observed_idx
    ON service_materialization_generations (service_id, observed_at DESC, generation_id);

CREATE UNIQUE INDEX IF NOT EXISTS service_materialization_generations_active_service_idx
    ON service_materialization_generations (service_id)
    WHERE status = 'active';
