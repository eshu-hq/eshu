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

DROP INDEX IF EXISTS eshu_search_index_terms_lookup_idx;

ALTER TABLE eshu_search_index_terms
    ALTER COLUMN term_key SET NOT NULL;

ALTER TABLE eshu_search_index_terms
    DROP CONSTRAINT IF EXISTS eshu_search_index_terms_pkey;

ALTER TABLE eshu_search_index_terms
    ADD CONSTRAINT eshu_search_index_terms_pkey
    PRIMARY KEY (scope_id, generation_id, term_key, document_id);

CREATE INDEX IF NOT EXISTS eshu_search_index_documents_repo_idx
    ON eshu_search_index_documents (scope_id, generation_id, repo_id, source_kind);

CREATE INDEX IF NOT EXISTS eshu_search_index_terms_lookup_idx
    ON eshu_search_index_terms (scope_id, generation_id, term_key);

-- eshu_search_index_terms_doc_idx covers the two hot document-keyed DELETEs:
--   eshuSearchIndexRefreshDocumentTermsQuery  (document_id = ANY($3::text[]))
--   eshuSearchIndexRetireTermsQuery           (document_id <> ALL($3::text[]))
-- Without this index both queries scan the entire (scope_id, generation_id)
-- PK slice — up to 4.75M rows per scope on the 43 GB / 73.5 M-row table.
-- With this index the planner seeks directly to a document's term rows.
--
-- Applied at bootstrap startup via the idempotent IF NOT EXISTS pattern used
-- throughout this file. On an existing large table a plain CREATE INDEX takes
-- a table-level lock during the build phase; an operator adding this index to
-- a populated production database should use CREATE INDEX CONCURRENTLY
-- out-of-band to avoid locking writers. Fresh-corpus bootstrap is unaffected.
--
-- Write-amplification: one extra B-tree entry per term INSERT/DELETE.
-- Cardinality: (scope_id, generation_id, document_id) is a good prefix —
-- each document has O(200) terms, so each document_id maps to ~200 leaf rows.
CREATE INDEX IF NOT EXISTS eshu_search_index_terms_doc_idx
    ON eshu_search_index_terms (scope_id, generation_id, document_id);
