CREATE TABLE IF NOT EXISTS eshu_search_vector_values (
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE,
    document_id TEXT NOT NULL,
    provider_profile_id TEXT NOT NULL DEFAULT 'local',
    source_class TEXT NOT NULL DEFAULT 'search_documents',
    embedding_model_id TEXT NOT NULL,
    embedding_dimensions INTEGER NOT NULL CHECK (embedding_dimensions > 0 AND embedding_dimensions <= 4096),
    embedding_content_hash TEXT NOT NULL,
    vector_index_version TEXT NOT NULL,
    vector_values DOUBLE PRECISION[] NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (scope_id, generation_id, document_id, provider_profile_id, source_class, embedding_model_id, vector_index_version),
    CHECK (cardinality(vector_values) = embedding_dimensions)
);

ALTER TABLE eshu_search_vector_values
    ADD COLUMN IF NOT EXISTS provider_profile_id TEXT NOT NULL DEFAULT 'local';

ALTER TABLE eshu_search_vector_values
    ADD COLUMN IF NOT EXISTS source_class TEXT NOT NULL DEFAULT 'search_documents';

ALTER TABLE eshu_search_vector_values
    DROP CONSTRAINT IF EXISTS eshu_search_vector_values_pkey;

ALTER TABLE eshu_search_vector_values
    ADD CONSTRAINT eshu_search_vector_values_pkey
    PRIMARY KEY (scope_id, generation_id, document_id, provider_profile_id, source_class, embedding_model_id, vector_index_version);

CREATE INDEX IF NOT EXISTS eshu_search_vector_values_model_idx
    ON eshu_search_vector_values (scope_id, generation_id, embedding_model_id, vector_index_version, document_id);

CREATE INDEX IF NOT EXISTS eshu_search_vector_values_model_v2_idx
    ON eshu_search_vector_values (scope_id, generation_id, provider_profile_id, source_class, embedding_model_id, vector_index_version, document_id);
