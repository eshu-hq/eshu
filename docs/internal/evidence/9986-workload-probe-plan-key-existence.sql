-- Evidence for #9986 (see 9986-workload-probe-plan.md).
-- Plans of the key-existence statement (the quoting fix alone), custom then generic, with 3 bound parameters.
-- Run with: psql -X -d <database> -f 9986-workload-probe-plan-key-existence.sql
\timing off
\set ON_ERROR_STOP 0
PREPARE probe(text,text,text[]) AS
SELECT EXISTS (
  SELECT 1
  FROM fact_records AS repo
  WHERE repo.scope_id = $1
    AND repo.generation_id = $2
    AND repo.fact_kind = 'repository'
    AND repo.payload->>'graph_id' <> ''
    AND repo.payload->>'name' <> ''
    AND EXISTS (
      SELECT 1
      FROM fact_records AS f
      WHERE f.scope_id = $1
        AND f.generation_id = $2
        AND f.fact_kind = 'file'
        AND f.payload->>'repo_id' = repo.payload->>'graph_id'
        AND f.payload->'parsed_file_data' ?| $3::text[]
    )
);
\echo ===== MODE custom =====

\echo ---- custom true_best_first_in_scan scope-00002
EXPLAIN (ANALYZE, BUFFERS, SETTINGS) EXECUTE probe('scope-00002','gen-00002',ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','chart_type','dockerfile_stages','pipeline_calls','artifact_type','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports']::text[]);
\echo ---- custom true_worst_last_in_scan scope-00001
EXPLAIN (ANALYZE, BUFFERS, SETTINGS) EXECUTE probe('scope-00001','gen-00001',ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','chart_type','dockerfile_stages','pipeline_calls','artifact_type','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports']::text[]);
\echo ---- custom true_empty_bucket_every_file scope-00004
EXPLAIN (ANALYZE, BUFFERS, SETTINGS) EXECUTE probe('scope-00004','gen-00004',ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','chart_type','dockerfile_stages','pipeline_calls','artifact_type','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports']::text[]);
\echo ---- custom false_normal_scope scope-00003
EXPLAIN (ANALYZE, BUFFERS, SETTINGS) EXECUTE probe('scope-00003','gen-00003',ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','chart_type','dockerfile_stages','pipeline_calls','artifact_type','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports']::text[]);
\echo ---- custom false_many_files_no_signal_300k scope-mono-nosignal
EXPLAIN (ANALYZE, BUFFERS, SETTINGS) EXECUTE probe('scope-mono-nosignal','gen-mono-nosignal',ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','chart_type','dockerfile_stages','pipeline_calls','artifact_type','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports']::text[]);
\echo ---- custom true_worst_300k_last_in_scan scope-mono-lastsignal
EXPLAIN (ANALYZE, BUFFERS, SETTINGS) EXECUTE probe('scope-mono-lastsignal','gen-mono-lastsignal',ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','chart_type','dockerfile_stages','pipeline_calls','artifact_type','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports']::text[]);
\echo ===== MODE generic =====
DEALLOCATE ALL;
PREPARE probe(text,text,text[]) AS
SELECT EXISTS (
  SELECT 1
  FROM fact_records AS repo
  WHERE repo.scope_id = $1
    AND repo.generation_id = $2
    AND repo.fact_kind = 'repository'
    AND repo.payload->>'graph_id' <> ''
    AND repo.payload->>'name' <> ''
    AND EXISTS (
      SELECT 1
      FROM fact_records AS f
      WHERE f.scope_id = $1
        AND f.generation_id = $2
        AND f.fact_kind = 'file'
        AND f.payload->>'repo_id' = repo.payload->>'graph_id'
        AND f.payload->'parsed_file_data' ?| $3::text[]
    )
);
SET plan_cache_mode = force_generic_plan;
\echo ---- generic true_best_first_in_scan scope-00002
EXPLAIN (ANALYZE, BUFFERS, SETTINGS) EXECUTE probe('scope-00002','gen-00002',ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','chart_type','dockerfile_stages','pipeline_calls','artifact_type','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports']::text[]);
\echo ---- generic true_worst_last_in_scan scope-00001
EXPLAIN (ANALYZE, BUFFERS, SETTINGS) EXECUTE probe('scope-00001','gen-00001',ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','chart_type','dockerfile_stages','pipeline_calls','artifact_type','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports']::text[]);
\echo ---- generic true_empty_bucket_every_file scope-00004
EXPLAIN (ANALYZE, BUFFERS, SETTINGS) EXECUTE probe('scope-00004','gen-00004',ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','chart_type','dockerfile_stages','pipeline_calls','artifact_type','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports']::text[]);
\echo ---- generic false_normal_scope scope-00003
EXPLAIN (ANALYZE, BUFFERS, SETTINGS) EXECUTE probe('scope-00003','gen-00003',ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','chart_type','dockerfile_stages','pipeline_calls','artifact_type','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports']::text[]);
\echo ---- generic false_many_files_no_signal_300k scope-mono-nosignal
EXPLAIN (ANALYZE, BUFFERS, SETTINGS) EXECUTE probe('scope-mono-nosignal','gen-mono-nosignal',ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','chart_type','dockerfile_stages','pipeline_calls','artifact_type','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports']::text[]);
\echo ---- generic true_worst_300k_last_in_scan scope-mono-lastsignal
EXPLAIN (ANALYZE, BUFFERS, SETTINGS) EXECUTE probe('scope-mono-lastsignal','gen-mono-lastsignal',ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','chart_type','dockerfile_stages','pipeline_calls','artifact_type','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports']::text[]);
