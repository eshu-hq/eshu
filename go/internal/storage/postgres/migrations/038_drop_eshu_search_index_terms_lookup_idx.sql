-- 038_drop_eshu_search_index_terms_lookup_idx.sql
--
-- Drops the redundant BM25 lookup index on
-- eshu_search_index_terms(scope_id, generation_id, term_key). The primary key
-- on (scope_id, generation_id, term_key, document_id) already provides the
-- same left-prefix lookup for BM25 query joins, while the standalone lookup
-- index adds one more B-tree write for every term insert/delete.
--
-- CONCURRENTLY keeps existing reducer writers from blocking while the index is
-- removed on populated deployments. New databases no longer create this index
-- in the embedded Go migration set's 003b_eshu_search_index.sql, so this
-- migration is a no-op there.

DROP INDEX CONCURRENTLY IF EXISTS eshu_search_index_terms_lookup_idx;
