-- 039_drop_eshu_search_index_terms_doc_idx.sql
--
-- Drops the document-keyed term index on
-- eshu_search_index_terms(scope_id, generation_id, document_id). The reducer
-- now clears all term rows for a scope/generation once before streaming pages,
-- then inserts refreshed terms without per-page document-keyed deletes. The
-- primary key on (scope_id, generation_id, term_key, document_id) remains the
-- BM25 lookup index and covers the generation-scoped clear prefix.
--
-- CONCURRENTLY keeps existing reducer writers from blocking while the index is
-- removed on populated deployments.

DROP INDEX CONCURRENTLY IF EXISTS eshu_search_index_terms_doc_idx;
