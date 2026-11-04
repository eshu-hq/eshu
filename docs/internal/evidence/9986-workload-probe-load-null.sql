-- Evidence for #9986 (see 9986-workload-probe-plan.md).
-- Generator step 2: the QA all-null shape (every signal key stored as JSON null or empty): a 150-file scope and a 300,000-file scope. Run after step 1.
-- Run with: psql -X -d <database> -f 9986-workload-probe-load-null.sql
\set ON_ERROR_STOP 1
\timing on
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id)
VALUES ('scope-null150','git','git','k','git','p0',now(),now(),'active','gen-null150'),
       ('scope-mono-null','git','git','k','git','p0',now(),now(),'active','gen-mono-null');
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
VALUES ('gen-null150','scope-null150','poll',now(),now(),'active'),('gen-mono-null','scope-mono-null','poll',now(),now(),'active');
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT 'repo-'||s, s, 'gen-'||substr(s,7), 'repository','s','git','k',now(),now(),FALSE, jsonb_build_object('graph_id','repo-'||s,'name',s)
FROM unnest(ARRAY['scope-null150','scope-mono-null']) s;
CREATE OR REPLACE FUNCTION bench_null_payload(repo text, j int) RETURNS jsonb LANGUAGE sql IMMUTABLE AS $$
  SELECT format('{"repo_id":"%s","relative_path":"src/pkg%s/file%s.yaml","language":"yaml","parsed_file_data":{"k8s_resources":null,"argocd_applications":null,"argocd_applicationsets":null,"chart_type":null,"artifact_type":"","cloudformation_resources":null,"cloudformation_parameters":null,"cloudformation_outputs":null,"cloudformation_cross_stack_imports":null,"cloudformation_cross_stack_exports":null,"dockerfile_stages":[],"pipeline_calls":[],"docker_compose_services":[],"functions":[%s],"imports":[%s],"variables":[%s]}}',
    repo, j % 40, j,
    rtrim(repeat('{"name":"handleRequest","line":10,"end_line":42,"signature":"func handleRequest(ctx context.Context, req *Request) error"},',14),','),
    rtrim(repeat('{"name":"github.com/example/module/pkg","alias":"pkg","line":3},',10),','),
    rtrim(repeat('{"name":"defaultTimeout","line":7,"type":"time.Duration"},',8),','))::jsonb
$$;
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT 'file-scope-null150-'||j, 'scope-null150','gen-null150','file','s','git','k',now()-(j||' ms')::interval,now(),FALSE, bench_null_payload('repo-scope-null150', j)
FROM generate_series(1,150) j;
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT 'file-scope-mono-null-'||j, 'scope-mono-null','gen-mono-null','file','s','git','k',now()-(j||' ms')::interval,now(),FALSE, bench_null_payload('repo-scope-mono-null', j)
FROM generate_series(1,300000) j;
ANALYZE fact_records;
