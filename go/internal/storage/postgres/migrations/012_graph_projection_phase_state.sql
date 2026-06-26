CREATE TABLE IF NOT EXISTS graph_projection_phase_state (
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    acceptance_unit_id TEXT NOT NULL,
    source_run_id TEXT NOT NULL,
    generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE,
    keyspace TEXT NOT NULL,
    phase TEXT NOT NULL,
    committed_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (scope_id, acceptance_unit_id, source_run_id, generation_id, keyspace, phase)
);
CREATE INDEX IF NOT EXISTS graph_projection_phase_state_lookup_idx
    ON graph_projection_phase_state (scope_id, acceptance_unit_id, source_run_id, generation_id, keyspace, phase);
CREATE INDEX IF NOT EXISTS graph_projection_phase_state_updated_idx
    ON graph_projection_phase_state (updated_at DESC);
