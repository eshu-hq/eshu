-- 037_eshu_search_index_terms_doc_idx.sql
--
-- Historical no-op. This migration used to create
-- eshu_search_index_terms_doc_idx for the older per-document term refresh
-- lifecycle. Migration 039 drops that index after the reducer moved to one
-- generation-scoped clear before refreshed page inserts.
--
-- Keep this file in the ordered bootstrap set so old applied-migration names
-- remain stable, but do not recreate the large write-amplifying index on fresh
-- databases only to drop it again two migrations later.

SELECT 1;
