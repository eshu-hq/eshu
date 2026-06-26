-- 035_content_entities_repo_entity_idx.sql
--
-- Adds a composite index on content_entities(repo_id, entity_id) to support
-- the keyset-paginated search-document source loader introduced in #3440.
--
-- The loader queries:
--   WHERE repo_id = $1 AND entity_id > $cursor ORDER BY entity_id LIMIT $n
--
-- The prior schema had a single-column btree index on repo_id
-- (content_entities_repo_idx) and a PRIMARY KEY btree on entity_id alone.
-- With those two indexes Postgres must bitmap-AND the repo_id scan against the
-- full entity_id scan, then sort. The composite index (repo_id, entity_id)
-- allows an index-range scan that delivers rows in entity_id order for the
-- given repo_id without a separate sort step, matching the keyset cursor with
-- no merge overhead.
--
-- content_files already has PRIMARY KEY (repo_id, relative_path), which serves
-- the equivalent file keyset scan (WHERE repo_id=$1 AND relative_path>$cursor)
-- directly; no separate migration is needed for that table.

CREATE INDEX CONCURRENTLY IF NOT EXISTS content_entities_repo_entity_idx
    ON content_entities (repo_id, entity_id);
