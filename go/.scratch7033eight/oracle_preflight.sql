-- #7033 fixed-corpus cost gate; never use this as endpoint timing evidence.
-- Run with psql -X -v ON_ERROR_STOP=1 -v expected_database=...
--   -v expected_system_id=... -f oracle_preflight.sql on the fixed primary.
-- EXPLAIN ANALYZE executes SELECTs; this script fails closed at two seconds
-- per SELECT and leaves the transaction read-only.
\set ON_ERROR_STOP on
BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
SET LOCAL lock_timeout = '1s';
SET LOCAL statement_timeout = '2s';
SET LOCAL transaction_timeout = '30s';
SET LOCAL plan_cache_mode = force_custom_plan;

SELECT (
    current_database() = :'expected_database'
    AND NOT pg_is_in_recovery()
    AND current_setting('transaction_read_only') = 'on'
    AND (SELECT system_identifier::text FROM pg_control_system()) = :'expected_system_id'
) AS target_ok \gset
\if :target_ok
\else
\echo 'fixed-corpus target guard failed'
SELECT 1 / 0;
\endif

PREPARE oracle_entity(text) AS
SELECT e.repo_id, e.relative_path, e.entity_id, e.entity_name,
       e.entity_type, coalesce(e.language, ''), e.start_line, e.end_line
FROM content_entities e
WHERE (e.entity_name ILIKE '%' || $1 || '%'
       OR e.source_cache ILIKE '%' || $1 || '%')
  AND eshu_require_content_substring_indexes_ready()
LIMIT 251;

PREPARE oracle_path(text) AS
SELECT f.repo_id, f.relative_path, coalesce(f.language, ''),
       least(greatest(coalesce(f.line_count, 1), 1), 80)
FROM content_files f
WHERE f.relative_path ILIKE '%' || $1 || '%'
  AND eshu_require_content_substring_indexes_ready()
LIMIT 251;

PREPARE oracle_content(text) AS
SELECT f.repo_id, f.relative_path, coalesce(f.language, ''),
       least(greatest(coalesce(f.line_count, 1), 1), 80)
FROM content_files f
WHERE f.content ILIKE '%' || $1 || '%'
  AND f.relative_path NOT ILIKE '%' || $1 || '%'
  AND eshu_require_content_substring_indexes_ready()
LIMIT 251;

-- Common and rare canonical terms. Record Execution Time and shared hit/read
-- blocks for each pool. Any 2s timeout stops the preflight before ABBA.
EXPLAIN (ANALYZE, BUFFERS, TIMING OFF, FORMAT JSON) EXECUTE oracle_entity('content');
EXPLAIN (ANALYZE, BUFFERS, TIMING OFF, FORMAT JSON) EXECUTE oracle_path('content');
EXPLAIN (ANALYZE, BUFFERS, TIMING OFF, FORMAT JSON) EXECUTE oracle_content('content');
EXPLAIN (ANALYZE, BUFFERS, TIMING OFF, FORMAT JSON) EXECUTE oracle_entity('function');
EXPLAIN (ANALYZE, BUFFERS, TIMING OFF, FORMAT JSON) EXECUTE oracle_path('function');
EXPLAIN (ANALYZE, BUFFERS, TIMING OFF, FORMAT JSON) EXECUTE oracle_content('function');
EXPLAIN (ANALYZE, BUFFERS, TIMING OFF, FORMAT JSON) EXECUTE oracle_entity('deployment');
EXPLAIN (ANALYZE, BUFFERS, TIMING OFF, FORMAT JSON) EXECUTE oracle_path('deployment');
EXPLAIN (ANALYZE, BUFFERS, TIMING OFF, FORMAT JSON) EXECUTE oracle_content('deployment');
EXPLAIN (ANALYZE, BUFFERS, TIMING OFF, FORMAT JSON) EXECUTE oracle_entity('repository');
EXPLAIN (ANALYZE, BUFFERS, TIMING OFF, FORMAT JSON) EXECUTE oracle_path('repository');
EXPLAIN (ANALYZE, BUFFERS, TIMING OFF, FORMAT JSON) EXECUTE oracle_content('repository');

ROLLBACK;
