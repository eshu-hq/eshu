CREATE TABLE IF NOT EXISTS eshu_search_vector_scope_state (
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE,
    provider_profile_id TEXT NOT NULL,
    source_class TEXT NOT NULL,
    embedding_model_id TEXT NOT NULL,
    vector_index_version TEXT NOT NULL,
    projection_revision BIGINT NOT NULL,
    build_fence BIGINT NOT NULL,
    document_cursor TEXT NOT NULL DEFAULT '',
    state TEXT NOT NULL CHECK (state IN ('building','ready','failed')),
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (scope_id, generation_id, provider_profile_id, source_class, embedding_model_id, vector_index_version)
);
ALTER TABLE eshu_search_vector_scope_state
    ADD COLUMN IF NOT EXISTS document_cursor TEXT NOT NULL DEFAULT '';
