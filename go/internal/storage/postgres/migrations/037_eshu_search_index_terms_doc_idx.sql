-- 037_eshu_search_index_terms_doc_idx.sql
--
-- Historical migration: adds a covering index on
-- eshu_search_index_terms(scope_id, generation_id, document_id) for the older
-- document-keyed term refresh lifecycle:
--
--   refresh terms for documents  (document_id = ANY($3::text[]))
--   retire terms for documents   (document_id <> ALL($3::text[]))
--
-- Without this index both queries must scan the full (scope_id, generation_id)
-- PK slice — up to 4.75M rows per scope on the 43 GB / 73.5 M-row table.
-- With this index the planner seeks directly to a document's term rows.
--
-- CONCURRENTLY: bootstrap applies this migration via ApplyDefinitions which
-- runs each single-statement migration file in autocommit mode. CONCURRENTLY
-- is required here because existing deployments have a populated
-- eshu_search_index_terms table; a plain CREATE INDEX would take a table-level
-- lock that blocks reducer INSERT/DELETE for the duration of the index build.
-- CONCURRENTLY builds the index without holding that lock.
--
-- Write-amplification: one extra B-tree entry per term INSERT/DELETE.
-- Cardinality: each document has O(200) terms, so each (scope, generation,
-- document_id) entry maps to ~200 leaf rows — a good selectivity prefix.
--
-- Later migration 039 drops this index after the reducer term lifecycle moves
-- to one generation-scoped clear before refreshed page inserts. Keep this
-- migration for upgrade ordering and idempotent replay of historical schemas.

CREATE INDEX CONCURRENTLY IF NOT EXISTS eshu_search_index_terms_doc_idx
    ON eshu_search_index_terms (scope_id, generation_id, document_id);
