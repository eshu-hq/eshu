CREATE TABLE IF NOT EXISTS eshu_search_index_documents (
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE,
    document_id TEXT NOT NULL,
    fact_id TEXT NOT NULL,
    repo_id TEXT NOT NULL,
    source_kind TEXT NOT NULL,
    document JSONB NOT NULL,
    document_length INTEGER NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (scope_id, generation_id, document_id)
);

CREATE TABLE IF NOT EXISTS eshu_search_index_terms (
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE,
    document_id TEXT NOT NULL,
    term TEXT NOT NULL,
    term_frequency INTEGER NOT NULL,
    PRIMARY KEY (scope_id, generation_id, term, document_id)
);

CREATE TABLE IF NOT EXISTS eshu_search_index_stats (
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE,
    document_count INTEGER NOT NULL,
    average_document_length DOUBLE PRECISION NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (scope_id, generation_id)
);

CREATE INDEX IF NOT EXISTS eshu_search_index_documents_repo_idx
    ON eshu_search_index_documents (scope_id, generation_id, repo_id, source_kind);

CREATE INDEX IF NOT EXISTS eshu_search_index_terms_lookup_idx
    ON eshu_search_index_terms (scope_id, generation_id, term);
