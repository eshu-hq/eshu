CREATE TABLE IF NOT EXISTS eshu_search_document_projection_state (
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE,
    projection_revision BIGINT NOT NULL,
    build_fence BIGINT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('building','ready','failed')),
    document_count BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (scope_id, generation_id)
);
