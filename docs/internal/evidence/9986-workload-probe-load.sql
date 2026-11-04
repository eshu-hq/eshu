-- Evidence for #9986 (see 9986-workload-probe-plan.md).
-- Generator step 1: 3,000 ordinary scopes, two 300,000-file monorepo scopes, repository facts, and the content/content_entity filler (about 5.25M rows). Needs the repository migrations applied first.
-- Run with: psql -X -d <database> -f 9986-workload-probe-load.sql
\timing on
SET synchronous_commit = off;
SET maintenance_work_mem = '1GB';
-- scopes/generations: 3000 normal + 2 monorepos
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id)
SELECT 'scope-'||lpad(i::text,5,'0'), 'git','git','k','git','p0', now(), now(), 'active', 'gen-'||lpad(i::text,5,'0') FROM generate_series(1,3000) i;
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id)
VALUES ('scope-mono-nosignal','git','git','k','git','p0',now(),now(),'active','gen-mono-nosignal'),
       ('scope-mono-lastsignal','git','git','k','git','p0',now(),now(),'active','gen-mono-lastsignal');
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
SELECT active_generation_id, scope_id, 'poll', now(), now(), 'active' FROM ingestion_scopes WHERE scope_id LIKE 'scope-%';

-- repository facts, one per scope (graph_id = repo-<scope>)
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT 'repo-'||s.scope_id, s.scope_id, s.active_generation_id, 'repository', 's','git','k', now(), now(), FALSE,
       jsonb_build_object('graph_id','repo-'||s.scope_id,'name',s.scope_id)
FROM ingestion_scopes s WHERE s.scope_id LIKE 'scope-%';

-- file facts: reusable payload builder
CREATE OR REPLACE FUNCTION bench_file_payload(repo text, j int, kind text) RETURNS jsonb LANGUAGE sql IMMUTABLE AS $$
  SELECT format('{"repo_id":"%s","relative_path":"src/pkg%s/file%s.go","language":"go","parsed_file_data":{%s"functions":[%s],"imports":[%s],"variables":[%s]}}',
    repo, j % 40, j,
    CASE kind WHEN 'signal' THEN '"k8s_resources":[{"kind":"Deployment","name":"web"}],'
              WHEN 'empty'  THEN '"k8s_resources":[],'
              ELSE '' END,
    rtrim(repeat('{"name":"handleRequest","line":10,"end_line":42,"signature":"func handleRequest(ctx context.Context, req *Request) error"},',14),','),
    rtrim(repeat('{"name":"github.com/example/module/pkg","alias":"pkg","line":3},',10),','),
    rtrim(repeat('{"name":"defaultTimeout","line":7,"type":"time.Duration"},',8),','))::jsonb
$$;

-- normal scopes: 150 files each. scope 1 = signal on file 1 (LAST in the DESC index scan = worst true),
-- scope 2 = signal on file 150 (FIRST in scan = best true), scope 3 = no signal, scope 4 = every file carries an empty bucket.
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT 'file-'||s.scope_id||'-'||j, s.scope_id, s.active_generation_id, 'file','s','git','k',
       timestamptz '2026-01-01' + (j||' seconds')::interval, now(), FALSE,
       bench_file_payload('repo-'||s.scope_id, j,
         CASE WHEN s.scope_id='scope-00001' AND j=1 THEN 'signal'
              WHEN s.scope_id='scope-00002' AND j=150 THEN 'signal'
              WHEN s.scope_id='scope-00004' THEN 'empty'
              ELSE 'none' END)
FROM ingestion_scopes s, generate_series(1,150) j
WHERE s.scope_id LIKE 'scope-0%' AND s.scope_id NOT LIKE 'scope-mono%';

-- monorepos: 300k files each, no signal, except the last-scan-position file of the second
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT 'file-'||s.scope_id||'-'||j, s.scope_id, s.active_generation_id, 'file','s','git','k',
       timestamptz '2026-01-01' + (j||' seconds')::interval, now(), FALSE,
       bench_file_payload('repo-'||s.scope_id, j, CASE WHEN s.scope_id='scope-mono-lastsignal' AND j=1 THEN 'signal' ELSE 'none' END)
FROM ingestion_scopes s, generate_series(1,300000) j
WHERE s.scope_id LIKE 'scope-mono%';

-- other kinds (content_entity-like) to reach ~5M rows, loaded in scope chunks
DO $$
DECLARE lo int;
BEGIN
  FOR lo IN SELECT generate_series(1,3000,250) LOOP
    INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
    SELECT 'ent-'||s.scope_id||'-'||j, s.scope_id, s.active_generation_id,
           CASE WHEN j % 7 = 0 THEN 'content' ELSE 'content_entity' END,'s','git','k',
           timestamptz '2026-01-01' + (j||' seconds')::interval, now(), FALSE,
           format('{"repo_id":"repo-%s","entity_type":"Function","entity_name":"fn%s","relative_path":"src/pkg%s/file%s.go","start_line":%s,"source_cache":"%s"}',
                  s.scope_id, j, j%40, j%150, j, repeat('x',300))::jsonb
    FROM ingestion_scopes s, generate_series(1,1400) j
    WHERE s.scope_id LIKE 'scope-0%' AND s.scope_id NOT LIKE 'scope-mono%'
      AND substr(s.scope_id,7)::int BETWEEN lo AND lo+249;
    COMMIT;
  END LOOP;
END $$;

VACUUM (ANALYZE) fact_records;
SELECT fact_kind, count(*) FROM fact_records GROUP BY 1 ORDER BY 2 DESC;
SELECT pg_size_pretty(pg_total_relation_size('fact_records')) total, pg_size_pretty(pg_relation_size('fact_records')) heap,
       (SELECT count(*) FROM fact_records) rows;
