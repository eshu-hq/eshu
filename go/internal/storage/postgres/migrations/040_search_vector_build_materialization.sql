CREATE TABLE IF NOT EXISTS search_vector_build_materialization (
    provider_profile_id  TEXT NOT NULL,
    source_class         TEXT NOT NULL,
    embedding_model_id   TEXT NOT NULL,
    vector_index_version TEXT NOT NULL,
    materialized_at      TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (provider_profile_id, source_class, embedding_model_id, vector_index_version)
);
