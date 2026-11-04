-- Evidence for #9986 (see 9986-workload-probe-plan.md).
-- Generator step 3: two 23,329-file scopes whose repository fact carries a 15 MB stored payload (imports_map), for the repository-identity cost section. Needs bench_null_payload and bench_file_payload from steps 1 and 2.
-- Run with: psql -X -d <database> -f 9986-workload-probe-load-big.sql
\set ON_ERROR_STOP 1
\timing on
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id)
VALUES ('scope-bigrepo-false','git','git','k','git','p0',now(),now(),'active','gen-bigrepo-false'),
       ('scope-bigrepo-true','git','git','k','git','p0',now(),now(),'active','gen-bigrepo-true');
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
VALUES ('gen-bigrepo-false','scope-bigrepo-false','poll',now(),now(),'active'),('gen-bigrepo-true','scope-bigrepo-true','poll',now(),now(),'active');
-- repository fact with a QA-sized imports_map (random hex compresses poorly): about 60k symbols x 3 paths
CREATE TEMP TABLE big_map AS
SELECT jsonb_object_agg(md5('sym'||g), jsonb_build_array(md5('a'||g)||'/'||md5('b'||g)||'.go', md5('c'||g)||'/'||md5('d'||g)||'.go', md5('e'||g)||'/'||md5('f'||g)||'.go')) AS m
FROM generate_series(1,60000) g;
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT 'repo-'||s, s, 'gen-'||substr(s,7), 'repository','repository:repo-'||s,'git','k', now()+interval '1 hour', now(), FALSE,
       jsonb_build_object('graph_id','repo-'||s,'graph_kind','repository','name',s,'repo_id','repo-'||s,'imports_map',(SELECT m FROM big_map))
FROM unnest(ARRAY['scope-bigrepo-false','scope-bigrepo-true']) s;
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT 'file-scope-bigrepo-false-'||j, 'scope-bigrepo-false','gen-bigrepo-false','file','s','git','k',now()-(j||' ms')::interval,now(),FALSE, bench_null_payload('repo-scope-bigrepo-false', j)
FROM generate_series(1,23329) j;
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT 'file-scope-bigrepo-true-'||j, 'scope-bigrepo-true','gen-bigrepo-true','file','s','git','k',now()-(j||' ms')::interval,now(),FALSE,
       CASE WHEN j=23329 THEN bench_file_payload('repo-scope-bigrepo-true', j, 'signal') ELSE bench_null_payload('repo-scope-bigrepo-true', j) END
FROM generate_series(1,23329) j;
ANALYZE fact_records;
SELECT scope_id, fact_kind, count(*), pg_size_pretty(max(pg_column_size(payload))::bigint) AS max_stored FROM fact_records WHERE scope_id LIKE 'scope-bigrepo-%' GROUP BY 1,2 ORDER BY 1,2;
