CREATE TABLE IF NOT EXISTS eshu_search_vector_metadata (
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE,
    document_id TEXT NOT NULL,
    provider_profile_id TEXT NOT NULL DEFAULT 'local',
    source_class TEXT NOT NULL DEFAULT 'search_documents',
    embedding_model_id TEXT NOT NULL,
    embedding_dimensions INTEGER NOT NULL CHECK (embedding_dimensions > 0),
    embedding_content_hash TEXT NOT NULL,
    vector_index_version TEXT NOT NULL,
    build_state TEXT NOT NULL CHECK (build_state IN ('disabled', 'queued', 'building', 'ready', 'failed', 'stale')),
    failure_class TEXT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    last_success_at TIMESTAMPTZ NULL,
    PRIMARY KEY (scope_id, generation_id, document_id, provider_profile_id, source_class, embedding_model_id, vector_index_version)
);

ALTER TABLE eshu_search_vector_metadata
    ADD COLUMN IF NOT EXISTS provider_profile_id TEXT NOT NULL DEFAULT 'local';

ALTER TABLE eshu_search_vector_metadata
    ADD COLUMN IF NOT EXISTS source_class TEXT NOT NULL DEFAULT 'search_documents';

ALTER TABLE eshu_search_vector_metadata
    DROP CONSTRAINT IF EXISTS eshu_search_vector_metadata_pkey;

ALTER TABLE eshu_search_vector_metadata
    ADD CONSTRAINT eshu_search_vector_metadata_pkey
    PRIMARY KEY (scope_id, generation_id, document_id, provider_profile_id, source_class, embedding_model_id, vector_index_version);

CREATE INDEX IF NOT EXISTS eshu_search_vector_metadata_state_idx
    ON eshu_search_vector_metadata (scope_id, generation_id, build_state);

CREATE INDEX IF NOT EXISTS eshu_search_vector_metadata_model_idx
    ON eshu_search_vector_metadata (scope_id, generation_id, embedding_model_id, vector_index_version);

CREATE INDEX IF NOT EXISTS eshu_search_vector_metadata_model_v2_idx
    ON eshu_search_vector_metadata (scope_id, generation_id, provider_profile_id, source_class, embedding_model_id, vector_index_version);
