CREATE TABLE IF NOT EXISTS content_substring_index_state (
    singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
    state TEXT NOT NULL CHECK (state IN ('not_built', 'building', 'ready', 'failed')),
    build_started_at TIMESTAMPTZ NULL,
    build_completed_at TIMESTAMPTZ NULL,
    failed_at TIMESTAMPTZ NULL,
    failure_class TEXT NOT NULL DEFAULT ''
      CHECK (failure_class IN ('', 'index_build_failed')),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);

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
      AND index_state.indisvalid
      AND index_state.indisready
      AND NOT index_state.indisunique
      AND index_state.indnkeyatts = 1
      AND index_state.indnatts = 1
      AND index_state.indpred IS NULL
      AND index_state.indexprs IS NULL
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
      AND index_state.indisvalid
      AND index_state.indisready
      AND NOT index_state.indisunique
      AND index_state.indnkeyatts = 1
      AND index_state.indnatts = 1
      AND index_state.indpred IS NULL
      AND index_state.indexprs IS NULL
  );
$function$;

CREATE OR REPLACE FUNCTION eshu_require_content_substring_indexes_ready()
RETURNS BOOLEAN
LANGUAGE plpgsql
STABLE
PARALLEL SAFE
AS $function$
DECLARE
  lifecycle_state TEXT;
BEGIN
  SELECT state
  INTO lifecycle_state
  FROM content_substring_index_state
  WHERE singleton = TRUE;

  IF lifecycle_state IS DISTINCT FROM 'ready'
     OR NOT eshu_content_substring_indexes_valid() THEN
    RAISE EXCEPTION 'content substring indexes are not ready'
      USING ERRCODE = '55000';
  END IF;
  RETURN TRUE;
END;
$function$;

INSERT INTO content_substring_index_state (
    singleton,
    state,
    build_completed_at,
    updated_at
)
VALUES (
    TRUE,
    CASE WHEN eshu_content_substring_indexes_valid() THEN 'ready' ELSE 'not_built' END,
    CASE WHEN eshu_content_substring_indexes_valid() THEN clock_timestamp() ELSE NULL END,
    clock_timestamp()
)
ON CONFLICT (singleton) DO NOTHING;

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
