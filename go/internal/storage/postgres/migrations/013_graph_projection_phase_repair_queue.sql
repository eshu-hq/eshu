CREATE TABLE IF NOT EXISTS graph_projection_phase_repair_queue (
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    acceptance_unit_id TEXT NOT NULL,
    source_run_id TEXT NOT NULL,
    generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE,
    keyspace TEXT NOT NULL,
    phase TEXT NOT NULL,
    committed_at TIMESTAMPTZ NOT NULL,
    enqueued_at TIMESTAMPTZ NOT NULL,
    next_attempt_at TIMESTAMPTZ NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0,
    last_error TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (scope_id, acceptance_unit_id, source_run_id, generation_id, keyspace, phase)
);
CREATE INDEX IF NOT EXISTS graph_projection_phase_repair_queue_due_idx
    ON graph_projection_phase_repair_queue (next_attempt_at ASC, enqueued_at ASC);
CREATE INDEX IF NOT EXISTS graph_projection_phase_repair_queue_updated_idx
    ON graph_projection_phase_repair_queue (updated_at DESC);
