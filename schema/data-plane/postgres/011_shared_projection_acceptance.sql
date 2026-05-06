CREATE TABLE IF NOT EXISTS shared_projection_acceptance (
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    acceptance_unit_id TEXT NOT NULL,
    source_run_id TEXT NOT NULL,
    generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE,
    accepted_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (scope_id, acceptance_unit_id, source_run_id)
);
CREATE INDEX IF NOT EXISTS shared_projection_acceptance_scope_idx
    ON shared_projection_acceptance (scope_id, generation_id);
CREATE INDEX IF NOT EXISTS shared_projection_acceptance_updated_idx
    ON shared_projection_acceptance (updated_at DESC);
