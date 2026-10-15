-- Shim for the #7623 QA-shape re-measure (see ../7623-go-module-anchored-loader.md).
-- DISPOSABLE DATABASE ONLY. This script TRUNCATEs fact_records, scope_generations,
-- ingestion_scopes and content_files. Create a new database whose name starts with
-- eshu_7623_, apply migrations 001, 002, 003 (003_fact_records.sql and
-- 003_service_catalog_fact_record_indexes.sql), 004, 099, 134 and 169, then run it.
-- It refuses every other database name.
-- Shape: 766 active repository scopes, 766 x 166 = 127,156 active file facts, each with
-- varied (non-repeating) parsed_file_data text, and 8 go.mod rows.
\set ON_ERROR_STOP on

SELECT current_database() LIKE 'eshu_7623_%' AS eshu_7623_disposable \gset
\if :eshu_7623_disposable
\else
  \echo 'refusing non-disposable database; expected an eshu_7623_ prefix'
  SELECT 1 / 0 AS refusing_non_disposable_database;
\endif

TRUNCATE fact_records, scope_generations, ingestion_scopes, content_files CASCADE;

INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, active_generation_id)
SELECT 'scope:' || n, 'repository', 'git', 'repository:r_' || n, 'git', 'scope:' || n,
       now(), now(), 'active', 'generation-scope:' || n
FROM generate_series(1, 766) AS n;

INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at)
SELECT 'generation-scope:' || n, 'scope:' || n, 'snapshot', now(), now(), 'active', now()
FROM generate_series(1, 766) AS n;

-- 766 scopes x 166 files = 127,156 file facts. Each function's source text is built
-- from md5 values that depend on the scope, file and function, so no two differ.
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    source_system, source_fact_key, observed_at, ingested_at, payload)
SELECT 'fact-' || s || '-' || f, 'scope:' || s, 'generation-scope:' || s, 'file',
       'file:scope:' || s || ':f' || f, 'git', 'f' || f,
       now() + ((s * 1000 + f) || ' milliseconds')::interval, now(),
       jsonb_build_object(
         'repo_id', 'scope:' || s,
         'relative_path', 'pkg/f' || f || '.go',
         'parsed_file_data', jsonb_build_object(
           'functions', (SELECT jsonb_agg(jsonb_build_object('uid', 'uid:' || s || ':' || f || ':' || k,
                'name', 'Fn' || k, 'line_number', k, 'source', (SELECT string_agg(md5(s::text || ':' || f::text || ':' || k::text || ':' || j::text), '') FROM generate_series(1, 24) j),
                'scip_symbol', 'scip-go gomod example.com/m' || s || ' pkg/Fn' || f || '_' || k || '().'))
                FROM generate_series(1, 12) k),
           'classes', (SELECT jsonb_agg(jsonb_build_object('uid', 'cuid:' || s || ':' || f || ':' || k,
                'name', 'T' || k, 'line_number', k, 'source', repeat('y', 80)))
                FROM generate_series(1, 6) k),
           'variables', (SELECT jsonb_agg(jsonb_build_object('name', 'v' || k, 'line_number', k,
                'value', repeat('z', 60))) FROM generate_series(1, 12) k),
           'imports', (SELECT jsonb_agg(jsonb_build_object('name', 'i' || k, 'source', 'example.com/dep' || k))
                FROM generate_series(1, 8) k),
           'function_calls', (SELECT jsonb_agg(jsonb_build_object('name', 'c' || k, 'full_name', 'pkg.c' || k,
                'line_number', k, 'args', repeat('a', 40))) FROM generate_series(1, 24) k)
         ))
FROM generate_series(1, 766) AS s,
     generate_series(1, 166) AS f;

-- go.mod rows for 8 Go scopes. Scope 1 is the producer of example.com/lib.
INSERT INTO content_files (repo_id, relative_path, content, content_hash, line_count, language, indexed_at, generation_id)
SELECT 'repository:r_' || s, 'go.mod',
       'module ' || CASE WHEN s = 1 THEN 'example.com/lib' ELSE 'example.com/app' || s END || E'\n\ngo 1.24\n',
       md5('mod' || s), 3, 'gomod', now(), 'generation-scope:' || s
FROM generate_series(1, 8) AS s;

-- The definitions the consumer asks for live in producer scope 1.
UPDATE fact_records
SET payload = jsonb_set(payload, '{parsed_file_data,functions,0,scip_symbol}',
        to_jsonb('scip-go gomod example.com/lib pkg/New().'::text))
WHERE fact_id = 'fact-1-1';
UPDATE fact_records
SET payload = jsonb_set(payload, '{parsed_file_data,functions,0,scip_symbol}',
        to_jsonb('scip-go gomod example.com/lib pkg/Client#Do().'::text))
WHERE fact_id = 'fact-1-2';

VACUUM (ANALYZE) fact_records;
VACUUM (ANALYZE) ingestion_scopes;
VACUUM (ANALYZE) scope_generations;
VACUUM (ANALYZE) content_files;
SELECT count(*) AS facts, pg_size_pretty(pg_total_relation_size('fact_records')) AS size FROM fact_records;
