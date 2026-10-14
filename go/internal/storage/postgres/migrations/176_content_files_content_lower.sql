-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #7975: stored lowercase copy of content_files.content for the unscoped
-- search recheck. A per-row ILIKE pays a whole-value lowercase copy per
-- candidate inside an uninterruptible stretch (about 6x the LIKE cost on the
-- 53 MB scratch proof: 527 ms warm ILIKE vs 83 ms LIKE, 577 ms vs 129 ms
-- cold), so one large document holds a common term's tail past the budget.
-- Testing the stored copy with LIKE folds the pattern once instead.
--
-- Storage cost: the copy roughly doubles the content TOAST (one more
-- out-of-line value per row). Case folding is identical by construction --
-- ILIKE lowercases both sides with the same lower() -- and the criterion-2
-- differential (token list over upper, mixed, wildcard, single-character,
-- and non-ASCII tokens) proves row equality on the backend.
--
-- Lock and deploy behavior (Postgres 18): unlike a plain NULL column, ADD
-- COLUMN ... STORED GENERATED rewrites the table to compute every value, so
-- this takes ACCESS EXCLUSIVE on content_files for the rewrite (minutes on a
-- corpus with multi-megabyte values) and blocks writers and readers while it
-- runs. Deploy in a low-write window. The generated value follows content on
-- every write with no writer change, so there is no NULL edge: old code
-- ignores the column, and new code must only deploy after this migration.
-- Rollback drops the column (copies lost); re-upgrade recomputes them.
-- The readiness function below lets the unscoped search fail loud (503) when
-- the 177 index is absent instead of falling back to an unindexed scan; it
-- raises the same SQLSTATE and message as the existing substring-index check
-- so the existing 503 mapping covers it, and the function name says which
-- index is missing.
--
-- Migration proof (local Postgres 18.6 scratch container, 2026-10-12): 3,001
-- rows with one 53 MB value. ADD COLUMN rewrite: 17.1 s including the lower()
-- computation. ANALYZE: under 1 s.
ALTER TABLE content_files
    ADD COLUMN IF NOT EXISTS content_lower TEXT GENERATED ALWAYS AS (lower(content)) STORED;

CREATE OR REPLACE FUNCTION eshu_require_content_lower_trgm_index_ready()
RETURNS BOOLEAN
LANGUAGE plpgsql
STABLE
PARALLEL SAFE
AS $function$
BEGIN
  IF NOT EXISTS (
    SELECT 1
    FROM pg_index AS index_state
    JOIN pg_class AS index_relation ON index_relation.oid = index_state.indexrelid
    JOIN pg_class AS table_relation ON table_relation.oid = index_state.indrelid
    JOIN pg_am AS access_method ON access_method.oid = index_relation.relam
    JOIN pg_attribute AS indexed_attribute
      ON indexed_attribute.attrelid = table_relation.oid
     AND indexed_attribute.attnum = index_state.indkey[0]
    JOIN pg_opclass AS operator_class ON operator_class.oid = index_state.indclass[0]
    WHERE index_relation.oid = to_regclass('public.content_files_content_lower_trgm_idx')
      AND table_relation.oid = to_regclass('public.content_files')
      AND access_method.amname = 'gin'
      AND indexed_attribute.attname = 'content_lower'
      AND operator_class.opcname = 'gin_trgm_ops'
      AND index_state.indisvalid
      AND index_state.indisready
      AND NOT index_state.indisunique
      AND index_state.indnkeyatts = 1
      AND index_state.indnatts = 1
      AND index_state.indpred IS NULL
      AND index_state.indexprs IS NULL
  ) THEN
    RAISE EXCEPTION 'content substring indexes are not ready'
      USING ERRCODE = '55000';
  END IF;
  RETURN TRUE;
END;
$function$;

ANALYZE content_files;
