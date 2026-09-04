-- Drop migration 100's (entity_id, repository_id) index (#5167). Migration 101
-- creates a four-column index whose key begins with exactly those two columns,
-- so every seek this one served is served there, and keeping both would make
-- the reducer's reachability writes maintain a redundant btree.
--
-- This is a file of its own, holding exactly ONE statement, because the
-- migration runner Execs each file as a single simple-query string and Postgres
-- treats a multi-statement string as an implicit transaction block -- which
-- DROP INDEX CONCURRENTLY cannot run inside.
DROP INDEX CONCURRENTLY IF EXISTS code_reachability_entity_repository_idx;
