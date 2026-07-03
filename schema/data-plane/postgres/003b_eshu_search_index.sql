CREATE TABLE IF NOT EXISTS eshu_search_index_documents (
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE,
    document_id TEXT NOT NULL,
    fact_id TEXT NOT NULL,
    repo_id TEXT NOT NULL,
    source_kind TEXT NOT NULL,
    content_hash TEXT NOT NULL DEFAULT '',
    document JSONB NOT NULL,
    document_length INTEGER NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (scope_id, generation_id, document_id)
);

ALTER TABLE eshu_search_index_documents
    ADD COLUMN IF NOT EXISTS content_hash TEXT NOT NULL DEFAULT '';

UPDATE eshu_search_index_documents doc
SET content_hash = fact.payload->>'content_hash'
FROM fact_records fact
WHERE doc.content_hash = ''
  AND fact.fact_id = doc.fact_id
  AND fact.payload ? 'content_hash';

CREATE TABLE IF NOT EXISTS eshu_search_index_terms (
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE,
    document_id TEXT NOT NULL,
    term_key TEXT NOT NULL,
    term TEXT NOT NULL,
    term_frequency INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS eshu_search_index_stats (
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE,
    document_count INTEGER NOT NULL,
    average_document_length DOUBLE PRECISION NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (scope_id, generation_id)
);

ALTER TABLE eshu_search_index_terms
    ADD COLUMN IF NOT EXISTS term_key TEXT;

WITH stale_index_generations AS (
    SELECT DISTINCT scope_id, generation_id
    FROM eshu_search_index_terms
    WHERE term_key IS NULL OR term_key = ''
)
DELETE FROM eshu_search_index_stats stats
USING stale_index_generations stale
WHERE stats.scope_id = stale.scope_id
  AND stats.generation_id = stale.generation_id;

DELETE FROM eshu_search_index_terms
WHERE term_key IS NULL OR term_key = '';

ALTER TABLE eshu_search_index_terms
    ALTER COLUMN term_key SET NOT NULL;

ALTER TABLE eshu_search_index_terms
    DROP CONSTRAINT IF EXISTS eshu_search_index_terms_pkey;

ALTER TABLE eshu_search_index_terms
    ADD CONSTRAINT eshu_search_index_terms_pkey
    PRIMARY KEY (scope_id, generation_id, term_key, document_id);

CREATE INDEX IF NOT EXISTS eshu_search_index_documents_repo_idx
    ON eshu_search_index_documents (scope_id, generation_id, repo_id, source_kind);
