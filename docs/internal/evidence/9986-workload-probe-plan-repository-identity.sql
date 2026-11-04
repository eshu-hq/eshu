-- Evidence for #9986 (see 9986-workload-probe-plan.md).
-- Plans of the shipped statement and two repository-identity variants on the 15 MB repository payload scopes.
-- Run with: psql -X -d <database> -f 9986-workload-probe-plan-repository-identity.sql
\set ON_ERROR_STOP 0
\echo ===== CUR =====
DEALLOCATE ALL;
PREPARE p(text,text,text[],text[],text[],text[]) AS
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
\echo ---- CUR big_false_23k run1
EXPLAIN (ANALYZE, BUFFERS) EXECUTE p('scope-bigrepo-false','gen-bigrepo-false',ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','chart_type','dockerfile_stages','pipeline_calls','artifact_type','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','dockerfile_stages','pipeline_calls','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['chart_type'],ARRAY['artifact_type']);
\echo ---- CUR big_false_23k run2
EXPLAIN (ANALYZE, BUFFERS) EXECUTE p('scope-bigrepo-false','gen-bigrepo-false',ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','chart_type','dockerfile_stages','pipeline_calls','artifact_type','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','dockerfile_stages','pipeline_calls','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['chart_type'],ARRAY['artifact_type']);
\echo ---- CUR big_true_last_23k run1
EXPLAIN (ANALYZE, BUFFERS) EXECUTE p('scope-bigrepo-true','gen-bigrepo-true',ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','chart_type','dockerfile_stages','pipeline_calls','artifact_type','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','dockerfile_stages','pipeline_calls','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['chart_type'],ARRAY['artifact_type']);
\echo ---- CUR big_true_last_23k run2
EXPLAIN (ANALYZE, BUFFERS) EXECUTE p('scope-bigrepo-true','gen-bigrepo-true',ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','chart_type','dockerfile_stages','pipeline_calls','artifact_type','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','dockerfile_stages','pipeline_calls','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['chart_type'],ARRAY['artifact_type']);
\echo ---- CUR small_false_150 run1
EXPLAIN (ANALYZE, BUFFERS) EXECUTE p('scope-null150','gen-null150',ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','chart_type','dockerfile_stages','pipeline_calls','artifact_type','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','dockerfile_stages','pipeline_calls','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['chart_type'],ARRAY['artifact_type']);
\echo ---- CUR small_false_150 run2
EXPLAIN (ANALYZE, BUFFERS) EXECUTE p('scope-null150','gen-null150',ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','chart_type','dockerfile_stages','pipeline_calls','artifact_type','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','dockerfile_stages','pipeline_calls','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['chart_type'],ARRAY['artifact_type']);
\echo ---- CUR small_true_150 run1
EXPLAIN (ANALYZE, BUFFERS) EXECUTE p('scope-00001','gen-00001',ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','chart_type','dockerfile_stages','pipeline_calls','artifact_type','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','dockerfile_stages','pipeline_calls','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['chart_type'],ARRAY['artifact_type']);
\echo ---- CUR small_true_150 run2
EXPLAIN (ANALYZE, BUFFERS) EXECUTE p('scope-00001','gen-00001',ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','chart_type','dockerfile_stages','pipeline_calls','artifact_type','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','dockerfile_stages','pipeline_calls','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['chart_type'],ARRAY['artifact_type']);
\echo ===== C_single_detoast =====
DEALLOCATE ALL;
PREPARE p(text,text,text[],text[],text[],text[]) AS
SELECT EXISTS (
  SELECT 1
  FROM fact_records AS repo
  CROSS JOIN LATERAL jsonb_to_record(repo.payload) AS r(graph_id text, name text)
  WHERE repo.scope_id = $1
    AND repo.generation_id = $2
    AND repo.fact_kind = 'repository'
    AND r.graph_id <> ''
    AND r.name <> ''
    AND EXISTS (
      SELECT 1
      FROM fact_records AS f
      CROSS JOIN LATERAL jsonb_to_record(f.payload) AS x(repo_id text, relative_path text, parsed_file_data jsonb)
      WHERE f.scope_id = $1
        AND f.generation_id = $2
        AND f.fact_kind = 'file'
        AND (
          x.repo_id = r.graph_id
          OR regexp_replace(x.repo_id, '^[\u0009-\u000d\u0020\u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]+|[\u0009-\u000d\u0020\u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]+$', '', 'g')
           = regexp_replace(r.graph_id, '^[\u0009-\u000d\u0020\u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]+|[\u0009-\u000d\u0020\u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]+$', '', 'g')
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
\echo ---- C_single_detoast big_false_23k run1
EXPLAIN (ANALYZE, BUFFERS) EXECUTE p('scope-bigrepo-false','gen-bigrepo-false',ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','chart_type','dockerfile_stages','pipeline_calls','artifact_type','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','dockerfile_stages','pipeline_calls','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['chart_type'],ARRAY['artifact_type']);
\echo ---- C_single_detoast big_false_23k run2
EXPLAIN (ANALYZE, BUFFERS) EXECUTE p('scope-bigrepo-false','gen-bigrepo-false',ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','chart_type','dockerfile_stages','pipeline_calls','artifact_type','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','dockerfile_stages','pipeline_calls','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['chart_type'],ARRAY['artifact_type']);
\echo ---- C_single_detoast big_true_last_23k run1
EXPLAIN (ANALYZE, BUFFERS) EXECUTE p('scope-bigrepo-true','gen-bigrepo-true',ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','chart_type','dockerfile_stages','pipeline_calls','artifact_type','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','dockerfile_stages','pipeline_calls','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['chart_type'],ARRAY['artifact_type']);
\echo ---- C_single_detoast big_true_last_23k run2
EXPLAIN (ANALYZE, BUFFERS) EXECUTE p('scope-bigrepo-true','gen-bigrepo-true',ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','chart_type','dockerfile_stages','pipeline_calls','artifact_type','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','dockerfile_stages','pipeline_calls','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['chart_type'],ARRAY['artifact_type']);
\echo ---- C_single_detoast small_false_150 run1
EXPLAIN (ANALYZE, BUFFERS) EXECUTE p('scope-null150','gen-null150',ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','chart_type','dockerfile_stages','pipeline_calls','artifact_type','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','dockerfile_stages','pipeline_calls','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['chart_type'],ARRAY['artifact_type']);
\echo ---- C_single_detoast small_false_150 run2
EXPLAIN (ANALYZE, BUFFERS) EXECUTE p('scope-null150','gen-null150',ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','chart_type','dockerfile_stages','pipeline_calls','artifact_type','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','dockerfile_stages','pipeline_calls','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['chart_type'],ARRAY['artifact_type']);
\echo ---- C_single_detoast small_true_150 run1
EXPLAIN (ANALYZE, BUFFERS) EXECUTE p('scope-00001','gen-00001',ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','chart_type','dockerfile_stages','pipeline_calls','artifact_type','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','dockerfile_stages','pipeline_calls','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['chart_type'],ARRAY['artifact_type']);
\echo ---- C_single_detoast small_true_150 run2
EXPLAIN (ANALYZE, BUFFERS) EXECUTE p('scope-00001','gen-00001',ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','chart_type','dockerfile_stages','pipeline_calls','artifact_type','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','dockerfile_stages','pipeline_calls','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['chart_type'],ARRAY['artifact_type']);
\echo ===== D_stable_key_no_name =====
DEALLOCATE ALL;
PREPARE p(text,text,text[],text[],text[],text[]) AS
SELECT EXISTS (
  SELECT 1
  FROM fact_records AS repo
  WHERE repo.scope_id = $1
    AND repo.generation_id = $2
    AND repo.fact_kind = 'repository'
    AND repo.stable_fact_key LIKE 'repository:_%'
    AND EXISTS (
      SELECT 1
      FROM fact_records AS f
      CROSS JOIN LATERAL jsonb_to_record(f.payload) AS x(repo_id text, relative_path text, parsed_file_data jsonb)
      WHERE f.scope_id = $1
        AND f.generation_id = $2
        AND f.fact_kind = 'file'
        AND (
          x.repo_id = substr(repo.stable_fact_key, 12)
          OR regexp_replace(x.repo_id, '^[\u0009-\u000d\u0020\u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]+|[\u0009-\u000d\u0020\u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]+$', '', 'g')
           = regexp_replace(substr(repo.stable_fact_key, 12), '^[\u0009-\u000d\u0020\u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]+|[\u0009-\u000d\u0020\u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]+$', '', 'g')
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
\echo ---- D_stable_key_no_name big_false_23k run1
EXPLAIN (ANALYZE, BUFFERS) EXECUTE p('scope-bigrepo-false','gen-bigrepo-false',ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','chart_type','dockerfile_stages','pipeline_calls','artifact_type','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','dockerfile_stages','pipeline_calls','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['chart_type'],ARRAY['artifact_type']);
\echo ---- D_stable_key_no_name big_false_23k run2
EXPLAIN (ANALYZE, BUFFERS) EXECUTE p('scope-bigrepo-false','gen-bigrepo-false',ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','chart_type','dockerfile_stages','pipeline_calls','artifact_type','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','dockerfile_stages','pipeline_calls','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['chart_type'],ARRAY['artifact_type']);
\echo ---- D_stable_key_no_name big_true_last_23k run1
EXPLAIN (ANALYZE, BUFFERS) EXECUTE p('scope-bigrepo-true','gen-bigrepo-true',ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','chart_type','dockerfile_stages','pipeline_calls','artifact_type','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','dockerfile_stages','pipeline_calls','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['chart_type'],ARRAY['artifact_type']);
\echo ---- D_stable_key_no_name big_true_last_23k run2
EXPLAIN (ANALYZE, BUFFERS) EXECUTE p('scope-bigrepo-true','gen-bigrepo-true',ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','chart_type','dockerfile_stages','pipeline_calls','artifact_type','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','dockerfile_stages','pipeline_calls','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['chart_type'],ARRAY['artifact_type']);
\echo ---- D_stable_key_no_name small_false_150 run1
EXPLAIN (ANALYZE, BUFFERS) EXECUTE p('scope-null150','gen-null150',ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','chart_type','dockerfile_stages','pipeline_calls','artifact_type','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','dockerfile_stages','pipeline_calls','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['chart_type'],ARRAY['artifact_type']);
\echo ---- D_stable_key_no_name small_false_150 run2
EXPLAIN (ANALYZE, BUFFERS) EXECUTE p('scope-null150','gen-null150',ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','chart_type','dockerfile_stages','pipeline_calls','artifact_type','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','dockerfile_stages','pipeline_calls','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['chart_type'],ARRAY['artifact_type']);
\echo ---- D_stable_key_no_name small_true_150 run1
EXPLAIN (ANALYZE, BUFFERS) EXECUTE p('scope-00001','gen-00001',ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','chart_type','dockerfile_stages','pipeline_calls','artifact_type','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','dockerfile_stages','pipeline_calls','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['chart_type'],ARRAY['artifact_type']);
\echo ---- D_stable_key_no_name small_true_150 run2
EXPLAIN (ANALYZE, BUFFERS) EXECUTE p('scope-00001','gen-00001',ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','chart_type','dockerfile_stages','pipeline_calls','artifact_type','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['k8s_resources','argocd_applications','argocd_applicationsets','dockerfile_stages','pipeline_calls','docker_compose_services','cloudformation_resources','cloudformation_parameters','cloudformation_outputs','cloudformation_cross_stack_imports','cloudformation_cross_stack_exports'],ARRAY['chart_type'],ARRAY['artifact_type']);
