-- Add the deferred case-sensitive entity-name search index and make lifecycle
-- readiness require the exact full index shape alongside the two source indexes.
-- Serialize this migration's check-and-create boundary. PostgreSQL documents
-- that IF NOT EXISTS does not make concurrent same-name CREATE INDEX attempts
-- atomic; the transaction-scoped lock is released on success, failure, or
-- cancellation of this multi-statement migration.
SELECT pg_advisory_xact_lock(5318, 62);

-- The function still has its pre-062 two-index contract at this point. A true
-- value therefore distinguishes normal fresh/upgrade bootstrap from the
-- explicit deferred bootstrap, whose two expensive indexes are absent.
DO $block$
BEGIN
  IF eshu_content_substring_indexes_valid() THEN
    CREATE INDEX IF NOT EXISTS content_entities_name_trgm_idx
      ON content_entities USING gin (entity_name gin_trgm_ops);
  END IF;
END;
$block$;

CREATE OR REPLACE FUNCTION eshu_content_substring_indexes_valid()
RETURNS BOOLEAN
LANGUAGE sql
STABLE
PARALLEL SAFE
AS $function$
SELECT
  EXISTS (
    SELECT 1
    FROM pg_index AS index_state
    JOIN pg_class AS index_relation ON index_relation.oid = index_state.indexrelid
    JOIN pg_class AS table_relation ON table_relation.oid = index_state.indrelid
    JOIN pg_am AS access_method ON access_method.oid = index_relation.relam
    JOIN pg_attribute AS indexed_attribute
      ON indexed_attribute.attrelid = table_relation.oid
     AND indexed_attribute.attnum = index_state.indkey[0]
    JOIN pg_opclass AS operator_class ON operator_class.oid = index_state.indclass[0]
    WHERE index_relation.oid = to_regclass('public.content_files_content_trgm_idx')
      AND table_relation.oid = to_regclass('public.content_files')
      AND access_method.amname = 'gin'
      AND indexed_attribute.attname = 'content'
      AND operator_class.opcname = 'gin_trgm_ops'
      AND index_state.indisvalid AND index_state.indisready
      AND NOT index_state.indisunique
      AND index_state.indnkeyatts = 1 AND index_state.indnatts = 1
      AND index_state.indpred IS NULL AND index_state.indexprs IS NULL
  )
  AND EXISTS (
    SELECT 1
    FROM pg_index AS index_state
    JOIN pg_class AS index_relation ON index_relation.oid = index_state.indexrelid
    JOIN pg_class AS table_relation ON table_relation.oid = index_state.indrelid
    JOIN pg_am AS access_method ON access_method.oid = index_relation.relam
    JOIN pg_attribute AS indexed_attribute
      ON indexed_attribute.attrelid = table_relation.oid
     AND indexed_attribute.attnum = index_state.indkey[0]
    JOIN pg_opclass AS operator_class ON operator_class.oid = index_state.indclass[0]
    WHERE index_relation.oid = to_regclass('public.content_entities_source_trgm_idx')
      AND table_relation.oid = to_regclass('public.content_entities')
      AND access_method.amname = 'gin'
      AND indexed_attribute.attname = 'source_cache'
      AND operator_class.opcname = 'gin_trgm_ops'
      AND index_state.indisvalid AND index_state.indisready
      AND NOT index_state.indisunique
      AND index_state.indnkeyatts = 1 AND index_state.indnatts = 1
      AND index_state.indpred IS NULL AND index_state.indexprs IS NULL
  )
  AND EXISTS (
    SELECT 1
    FROM pg_index AS index_state
    JOIN pg_class AS index_relation ON index_relation.oid = index_state.indexrelid
    JOIN pg_class AS table_relation ON table_relation.oid = index_state.indrelid
    JOIN pg_am AS access_method ON access_method.oid = index_relation.relam
    JOIN pg_attribute AS indexed_attribute
      ON indexed_attribute.attrelid = table_relation.oid
     AND indexed_attribute.attnum = index_state.indkey[0]
    JOIN pg_opclass AS operator_class ON operator_class.oid = index_state.indclass[0]
    WHERE index_relation.oid = to_regclass('public.content_entities_name_trgm_idx')
      AND table_relation.oid = to_regclass('public.content_entities')
      AND access_method.amname = 'gin'
      AND indexed_attribute.attname = 'entity_name'
      AND operator_class.opcname = 'gin_trgm_ops'
      AND index_state.indisvalid AND index_state.indisready
      AND NOT index_state.indisunique
      AND index_state.indnkeyatts = 1 AND index_state.indnatts = 1
      AND index_state.indpred IS NULL AND index_state.indexprs IS NULL
  );
$function$;

UPDATE content_substring_index_state
SET state = 'not_built',
    build_completed_at = NULL,
    updated_at = clock_timestamp()
WHERE singleton = TRUE
  AND state = 'ready'
  AND NOT eshu_content_substring_indexes_valid();

UPDATE content_substring_index_state
SET state = 'ready',
    build_completed_at = coalesce(build_completed_at, clock_timestamp()),
    failed_at = NULL,
    failure_class = '',
    updated_at = clock_timestamp()
WHERE singleton = TRUE
  AND state <> 'ready'
  AND eshu_content_substring_indexes_valid();
