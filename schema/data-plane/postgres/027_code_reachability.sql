CREATE TABLE IF NOT EXISTS code_reachability_rows (
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE,
    repository_id TEXT NOT NULL,
    root_entity_id TEXT NOT NULL,
    entity_id TEXT NOT NULL,
    depth INTEGER NOT NULL,
    state TEXT NOT NULL,
    confidence DOUBLE PRECISION NOT NULL,
    min_resolution_method TEXT NOT NULL,
    evidence JSONB NOT NULL,
    root_kinds JSONB NOT NULL,
    observed_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (scope_id, generation_id, repository_id, root_entity_id, entity_id)
);

CREATE INDEX IF NOT EXISTS code_reachability_latest_lookup_idx
    ON code_reachability_rows (repository_id, entity_id, state, confidence DESC);

CREATE INDEX IF NOT EXISTS code_reachability_root_idx
    ON code_reachability_rows (repository_id, root_entity_id, depth, entity_id);

CREATE TABLE IF NOT EXISTS code_reachability_repository_watermarks (
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE,
    repository_id TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (scope_id, generation_id, repository_id)
);
