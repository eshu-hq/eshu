-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #7033: extend the content-index lifecycle's exact catalog contract with the
-- path GIN from migration 130. A deferred bootstrap records migration 130's
-- no-op variant, so this replacement makes the durable state not_built until
-- EnsureContentSearchIndexes creates all four indexes after content drains.
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
    WHERE index_relation.oid = to_regclass('public.content_files_relative_path_trgm_idx')
      AND table_relation.oid = to_regclass('public.content_files')
      AND access_method.amname = 'gin'
      AND indexed_attribute.attname = 'relative_path'
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
