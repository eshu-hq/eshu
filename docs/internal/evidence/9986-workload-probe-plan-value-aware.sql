-- Evidence for #9986 (see 9986-workload-probe-plan.md).
-- Plans of the shipped value-aware statement, custom then generic, with 6 bound parameters. Replace the statement text with the output of workloadCandidatePreconditionQuery() if it changes.
-- Run with: psql -X -d <database> -f 9986-workload-probe-plan-value-aware.sql
\set ON_ERROR_STOP 0
\echo ===== custom =====
DEALLOCATE ALL;
PREPARE probe(text,text,text[],text[],text[],text[]) AS
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
      CROSS JOIN LATERAL jsonb_to_record(f.payload) AS x(repo_id text, relative_path text, parsed_file_data jsonb)
      WHERE f.scope_id = $1
        AND f.generation_id = $2
        AND f.fact_kind = 'file'
        AND (
          x.repo_id = repo.payload->>'graph_id'
          OR regexp_replace(x.repo_id, '^[\u0009-\u000d\u0020\u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]+|[\u0009-\u000d\u0020\u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]+$', '', 'g')
           = regexp_replace(repo.payload->>'graph_id', '^[\u0009-\u000d\u0020\u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]+|[\u0009-\u000d\u0020\u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]+$', '', 'g')
        )
        AND jsonb_typeof(x.parsed_file_data) = 'object'
        AND (
          x.relative_path ~ '^[\u0009-\u000d\u0020\u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]*[jJ][eE][nN][kK\u212a][iI][nN][sS\u017f][fF][iI][lL][eE][\u0009-\u000d\u0020\u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]*$'
          OR (
            x.parsed_file_data ?| $3::text[]
            AND (
              EXISTS (
                SELECT 1 FROM unnest($4::text[]) AS k(key)
                WHERE jsonb_typeof(x.parsed_file_data->k.key) = 'array' AND x.parsed_file_data->k.key <> '[]'::jsonb
              )
              OR EXISTS (
                SELECT 1 FROM unnest($5::text[]) AS k(key)
                WHERE jsonb_typeof(x.parsed_file_data->k.key) <> 'null'
                  AND NOT (
                    jsonb_typeof(x.parsed_file_data->k.key) = 'string'
                    AND x.parsed_file_data->>k.key ~ '^[\u0009-\u000d\u0020\u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]*(<nil>)?[\u0009-\u000d\u0020\u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]*$'
                  )
              )
              OR EXISTS (
                SELECT 1 FROM unnest($6::text[]) AS k(key)
                WHERE jsonb_typeof(x.parsed_file_data->k.key) = 'string'
                  AND x.parsed_file_data->>k.key ~ '^[\u0009-\u000d\u0020\u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]*(docker_compose|github_actions_workflow)[\u0009-\u000d\u0020\u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]*$'
              )
            )
          )
        )
    )
);
\echo ---- FINAL custom true_last_in_scan_150 run1
EXPLAIN (ANALYZE, BUFFERS) EXECUTE probe('scope-00001','gen-00001',ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','chart_type','dockerfile_stages','pipeline_calls','artifact_type','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','dockerfile_stages','pipeline_calls','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['chart_type'],ARRAY['artifact_type']);
\echo ---- FINAL custom true_last_in_scan_150 run2
EXPLAIN (ANALYZE, BUFFERS) EXECUTE probe('scope-00001','gen-00001',ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','chart_type','dockerfile_stages','pipeline_calls','artifact_type','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','dockerfile_stages','pipeline_calls','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['chart_type'],ARRAY['artifact_type']);
\echo ---- FINAL custom true_first_in_scan_150 run1
EXPLAIN (ANALYZE, BUFFERS) EXECUTE probe('scope-00002','gen-00002',ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','chart_type','dockerfile_stages','pipeline_calls','artifact_type','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','dockerfile_stages','pipeline_calls','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['chart_type'],ARRAY['artifact_type']);
\echo ---- FINAL custom true_first_in_scan_150 run2
EXPLAIN (ANALYZE, BUFFERS) EXECUTE probe('scope-00002','gen-00002',ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','chart_type','dockerfile_stages','pipeline_calls','artifact_type','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','dockerfile_stages','pipeline_calls','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['chart_type'],ARRAY['artifact_type']);
\echo ---- FINAL custom false_no_keys_150 run1
EXPLAIN (ANALYZE, BUFFERS) EXECUTE probe('scope-00003','gen-00003',ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','chart_type','dockerfile_stages','pipeline_calls','artifact_type','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','dockerfile_stages','pipeline_calls','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['chart_type'],ARRAY['artifact_type']);
\echo ---- FINAL custom false_no_keys_150 run2
EXPLAIN (ANALYZE, BUFFERS) EXECUTE probe('scope-00003','gen-00003',ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','chart_type','dockerfile_stages','pipeline_calls','artifact_type','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','dockerfile_stages','pipeline_calls','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['chart_type'],ARRAY['artifact_type']);
\echo ---- FINAL custom false_empty_bucket_150 run1
EXPLAIN (ANALYZE, BUFFERS) EXECUTE probe('scope-00004','gen-00004',ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','chart_type','dockerfile_stages','pipeline_calls','artifact_type','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','dockerfile_stages','pipeline_calls','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['chart_type'],ARRAY['artifact_type']);
\echo ---- FINAL custom false_empty_bucket_150 run2
EXPLAIN (ANALYZE, BUFFERS) EXECUTE probe('scope-00004','gen-00004',ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','chart_type','dockerfile_stages','pipeline_calls','artifact_type','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','dockerfile_stages','pipeline_calls','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['chart_type'],ARRAY['artifact_type']);
\echo ---- FINAL custom false_all_null_keys_150 run1
EXPLAIN (ANALYZE, BUFFERS) EXECUTE probe('scope-null150','gen-null150',ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','chart_type','dockerfile_stages','pipeline_calls','artifact_type','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','dockerfile_stages','pipeline_calls','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['chart_type'],ARRAY['artifact_type']);
\echo ---- FINAL custom false_all_null_keys_150 run2
EXPLAIN (ANALYZE, BUFFERS) EXECUTE probe('scope-null150','gen-null150',ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','chart_type','dockerfile_stages','pipeline_calls','artifact_type','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','dockerfile_stages','pipeline_calls','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['chart_type'],ARRAY['artifact_type']);
\echo ---- FINAL custom false_no_keys_300k run1
EXPLAIN (ANALYZE, BUFFERS) EXECUTE probe('scope-mono-nosignal','gen-mono-nosignal',ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','chart_type','dockerfile_stages','pipeline_calls','artifact_type','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','dockerfile_stages','pipeline_calls','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['chart_type'],ARRAY['artifact_type']);
\echo ---- FINAL custom false_no_keys_300k run2
EXPLAIN (ANALYZE, BUFFERS) EXECUTE probe('scope-mono-nosignal','gen-mono-nosignal',ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','chart_type','dockerfile_stages','pipeline_calls','artifact_type','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','dockerfile_stages','pipeline_calls','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['chart_type'],ARRAY['artifact_type']);
\echo ---- FINAL custom true_last_300k run1
EXPLAIN (ANALYZE, BUFFERS) EXECUTE probe('scope-mono-lastsignal','gen-mono-lastsignal',ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','chart_type','dockerfile_stages','pipeline_calls','artifact_type','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','dockerfile_stages','pipeline_calls','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['chart_type'],ARRAY['artifact_type']);
\echo ---- FINAL custom true_last_300k run2
EXPLAIN (ANALYZE, BUFFERS) EXECUTE probe('scope-mono-lastsignal','gen-mono-lastsignal',ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','chart_type','dockerfile_stages','pipeline_calls','artifact_type','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','dockerfile_stages','pipeline_calls','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['chart_type'],ARRAY['artifact_type']);
\echo ---- FINAL custom false_all_null_keys_300k run1
EXPLAIN (ANALYZE, BUFFERS) EXECUTE probe('scope-mono-null','gen-mono-null',ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','chart_type','dockerfile_stages','pipeline_calls','artifact_type','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','dockerfile_stages','pipeline_calls','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['chart_type'],ARRAY['artifact_type']);
\echo ---- FINAL custom false_all_null_keys_300k run2
EXPLAIN (ANALYZE, BUFFERS) EXECUTE probe('scope-mono-null','gen-mono-null',ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','chart_type','dockerfile_stages','pipeline_calls','artifact_type','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','dockerfile_stages','pipeline_calls','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['chart_type'],ARRAY['artifact_type']);
RESET plan_cache_mode;
\echo ===== generic =====
DEALLOCATE ALL;
PREPARE probe(text,text,text[],text[],text[],text[]) AS
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
      CROSS JOIN LATERAL jsonb_to_record(f.payload) AS x(repo_id text, relative_path text, parsed_file_data jsonb)
      WHERE f.scope_id = $1
        AND f.generation_id = $2
        AND f.fact_kind = 'file'
        AND (
          x.repo_id = repo.payload->>'graph_id'
          OR regexp_replace(x.repo_id, '^[\u0009-\u000d\u0020\u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]+|[\u0009-\u000d\u0020\u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]+$', '', 'g')
           = regexp_replace(repo.payload->>'graph_id', '^[\u0009-\u000d\u0020\u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]+|[\u0009-\u000d\u0020\u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]+$', '', 'g')
        )
        AND jsonb_typeof(x.parsed_file_data) = 'object'
        AND (
          x.relative_path ~ '^[\u0009-\u000d\u0020\u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]*[jJ][eE][nN][kK\u212a][iI][nN][sS\u017f][fF][iI][lL][eE][\u0009-\u000d\u0020\u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]*$'
          OR (
            x.parsed_file_data ?| $3::text[]
            AND (
              EXISTS (
                SELECT 1 FROM unnest($4::text[]) AS k(key)
                WHERE jsonb_typeof(x.parsed_file_data->k.key) = 'array' AND x.parsed_file_data->k.key <> '[]'::jsonb
              )
              OR EXISTS (
                SELECT 1 FROM unnest($5::text[]) AS k(key)
                WHERE jsonb_typeof(x.parsed_file_data->k.key) <> 'null'
                  AND NOT (
                    jsonb_typeof(x.parsed_file_data->k.key) = 'string'
                    AND x.parsed_file_data->>k.key ~ '^[\u0009-\u000d\u0020\u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]*(<nil>)?[\u0009-\u000d\u0020\u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]*$'
                  )
              )
              OR EXISTS (
                SELECT 1 FROM unnest($6::text[]) AS k(key)
                WHERE jsonb_typeof(x.parsed_file_data->k.key) = 'string'
                  AND x.parsed_file_data->>k.key ~ '^[\u0009-\u000d\u0020\u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]*(docker_compose|github_actions_workflow)[\u0009-\u000d\u0020\u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]*$'
              )
            )
          )
        )
    )
);
SET plan_cache_mode = force_generic_plan;
\echo ---- FINAL generic true_last_in_scan_150 run1
EXPLAIN (ANALYZE, BUFFERS) EXECUTE probe('scope-00001','gen-00001',ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','chart_type','dockerfile_stages','pipeline_calls','artifact_type','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','dockerfile_stages','pipeline_calls','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['chart_type'],ARRAY['artifact_type']);
\echo ---- FINAL generic true_last_in_scan_150 run2
EXPLAIN (ANALYZE, BUFFERS) EXECUTE probe('scope-00001','gen-00001',ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','chart_type','dockerfile_stages','pipeline_calls','artifact_type','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','dockerfile_stages','pipeline_calls','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['chart_type'],ARRAY['artifact_type']);
\echo ---- FINAL generic true_first_in_scan_150 run1
EXPLAIN (ANALYZE, BUFFERS) EXECUTE probe('scope-00002','gen-00002',ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','chart_type','dockerfile_stages','pipeline_calls','artifact_type','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','dockerfile_stages','pipeline_calls','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['chart_type'],ARRAY['artifact_type']);
\echo ---- FINAL generic true_first_in_scan_150 run2
EXPLAIN (ANALYZE, BUFFERS) EXECUTE probe('scope-00002','gen-00002',ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','chart_type','dockerfile_stages','pipeline_calls','artifact_type','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','dockerfile_stages','pipeline_calls','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['chart_type'],ARRAY['artifact_type']);
\echo ---- FINAL generic false_no_keys_150 run1
EXPLAIN (ANALYZE, BUFFERS) EXECUTE probe('scope-00003','gen-00003',ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','chart_type','dockerfile_stages','pipeline_calls','artifact_type','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','dockerfile_stages','pipeline_calls','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['chart_type'],ARRAY['artifact_type']);
\echo ---- FINAL generic false_no_keys_150 run2
EXPLAIN (ANALYZE, BUFFERS) EXECUTE probe('scope-00003','gen-00003',ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','chart_type','dockerfile_stages','pipeline_calls','artifact_type','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','dockerfile_stages','pipeline_calls','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['chart_type'],ARRAY['artifact_type']);
\echo ---- FINAL generic false_empty_bucket_150 run1
EXPLAIN (ANALYZE, BUFFERS) EXECUTE probe('scope-00004','gen-00004',ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','chart_type','dockerfile_stages','pipeline_calls','artifact_type','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','dockerfile_stages','pipeline_calls','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['chart_type'],ARRAY['artifact_type']);
\echo ---- FINAL generic false_empty_bucket_150 run2
EXPLAIN (ANALYZE, BUFFERS) EXECUTE probe('scope-00004','gen-00004',ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','chart_type','dockerfile_stages','pipeline_calls','artifact_type','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','dockerfile_stages','pipeline_calls','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['chart_type'],ARRAY['artifact_type']);
\echo ---- FINAL generic false_all_null_keys_150 run1
EXPLAIN (ANALYZE, BUFFERS) EXECUTE probe('scope-null150','gen-null150',ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','chart_type','dockerfile_stages','pipeline_calls','artifact_type','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','dockerfile_stages','pipeline_calls','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['chart_type'],ARRAY['artifact_type']);
\echo ---- FINAL generic false_all_null_keys_150 run2
EXPLAIN (ANALYZE, BUFFERS) EXECUTE probe('scope-null150','gen-null150',ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','chart_type','dockerfile_stages','pipeline_calls','artifact_type','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','dockerfile_stages','pipeline_calls','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['chart_type'],ARRAY['artifact_type']);
\echo ---- FINAL generic false_no_keys_300k run1
EXPLAIN (ANALYZE, BUFFERS) EXECUTE probe('scope-mono-nosignal','gen-mono-nosignal',ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','chart_type','dockerfile_stages','pipeline_calls','artifact_type','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','dockerfile_stages','pipeline_calls','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['chart_type'],ARRAY['artifact_type']);
\echo ---- FINAL generic false_no_keys_300k run2
EXPLAIN (ANALYZE, BUFFERS) EXECUTE probe('scope-mono-nosignal','gen-mono-nosignal',ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','chart_type','dockerfile_stages','pipeline_calls','artifact_type','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','dockerfile_stages','pipeline_calls','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['chart_type'],ARRAY['artifact_type']);
\echo ---- FINAL generic true_last_300k run1
EXPLAIN (ANALYZE, BUFFERS) EXECUTE probe('scope-mono-lastsignal','gen-mono-lastsignal',ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','chart_type','dockerfile_stages','pipeline_calls','artifact_type','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','dockerfile_stages','pipeline_calls','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['chart_type'],ARRAY['artifact_type']);
\echo ---- FINAL generic true_last_300k run2
EXPLAIN (ANALYZE, BUFFERS) EXECUTE probe('scope-mono-lastsignal','gen-mono-lastsignal',ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','chart_type','dockerfile_stages','pipeline_calls','artifact_type','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','dockerfile_stages','pipeline_calls','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['chart_type'],ARRAY['artifact_type']);
\echo ---- FINAL generic false_all_null_keys_300k run1
EXPLAIN (ANALYZE, BUFFERS) EXECUTE probe('scope-mono-null','gen-mono-null',ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','chart_type','dockerfile_stages','pipeline_calls','artifact_type','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','dockerfile_stages','pipeline_calls','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['chart_type'],ARRAY['artifact_type']);
\echo ---- FINAL generic false_all_null_keys_300k run2
EXPLAIN (ANALYZE, BUFFERS) EXECUTE probe('scope-mono-null','gen-mono-null',ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','chart_type','dockerfile_stages','pipeline_calls','artifact_type','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','dockerfile_stages','pipeline_calls','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['chart_type'],ARRAY['artifact_type']);
RESET plan_cache_mode;
