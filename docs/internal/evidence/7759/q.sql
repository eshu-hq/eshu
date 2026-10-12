SET jit = off;
\echo '=== Q1 gate+page big/JS/Function ==='
EXPLAIN (ANALYZE, BUFFERS)
SELECT entity_id FROM content_entities
WHERE EXISTS (SELECT 1 FROM content_entities WHERE entity_type = 'Function' AND repo_id = 'repository:big' AND (language = 'javascript'))
  AND entity_type = 'Function' AND repo_id = 'repository:big' AND (language = 'javascript')
ORDER BY relative_path, start_line, entity_name LIMIT 10;
EXPLAIN (ANALYZE, BUFFERS)
SELECT entity_id FROM content_entities
WHERE EXISTS (SELECT 1 FROM content_entities WHERE entity_type = 'Function' AND repo_id = 'repository:big' AND (language = 'javascript'))
  AND entity_type = 'Function' AND repo_id = 'repository:big' AND (language = 'javascript')
ORDER BY relative_path, start_line, entity_name LIMIT 10;
\echo '=== Q1b gate repo-empty (medb/kotlin) ==='
EXPLAIN (ANALYZE, BUFFERS)
SELECT entity_id FROM content_entities
WHERE EXISTS (SELECT 1 FROM content_entities WHERE entity_type = 'Function' AND repo_id = 'repository:medb' AND (language = 'kotlin'))
  AND entity_type = 'Function' AND repo_id = 'repository:medb' AND (language = 'kotlin')
ORDER BY relative_path, start_line, entity_name LIMIT 10;
\echo '=== Q1c gate corpus-empty (hcl/TerraformResource) ==='
EXPLAIN (ANALYZE, BUFFERS)
SELECT entity_id FROM content_entities
WHERE EXISTS (SELECT 1 FROM content_entities WHERE entity_type = 'TerraformResource' AND repo_id = 'repository:big' AND (language = 'hcl'))
  AND entity_type = 'TerraformResource' AND repo_id = 'repository:big' AND (language = 'hcl')
ORDER BY relative_path, start_line, entity_name LIMIT 10;
\echo '=== Q2 list-repo-entities big ==='
EXPLAIN (ANALYZE, BUFFERS)
SELECT entity_id FROM content_entities WHERE repo_id = 'repository:big' ORDER BY relative_path, start_line LIMIT 500;
EXPLAIN (ANALYZE, BUFFERS)
SELECT entity_id FROM content_entities WHERE repo_id = 'repository:big' ORDER BY relative_path, start_line LIMIT 500;
\echo '=== Q3 by-type big/Class ==='
EXPLAIN (ANALYZE, BUFFERS)
SELECT entity_id FROM content_entities WHERE repo_id = 'repository:big' AND entity_type = 'Class' ORDER BY relative_path, start_line, entity_id LIMIT 500;
EXPLAIN (ANALYZE, BUFFERS)
SELECT entity_id FROM content_entities WHERE repo_id = 'repository:big' AND entity_type = 'Class' ORDER BY relative_path, start_line, entity_id LIMIT 500;
\echo '=== Q4 by-types k8smono ==='
EXPLAIN (ANALYZE, BUFFERS)
SELECT entity_id FROM content_entities WHERE repo_id = 'repository:k8smono' AND entity_type = ANY('{K8sResource,ArgoCDApplication}'::text[]) ORDER BY relative_path, start_line, entity_id LIMIT 500;
EXPLAIN (ANALYZE, BUFFERS)
SELECT entity_id FROM content_entities WHERE repo_id = 'repository:k8smono' AND entity_type = ANY('{K8sResource,ArgoCDApplication}'::text[]) ORDER BY relative_path, start_line, entity_id LIMIT 500;
\echo '=== Q5 k8s-candidates k8smono ==='
EXPLAIN (ANALYZE, BUFFERS)
SELECT entity_id, entity_name FROM content_entities
WHERE repo_id = 'repository:k8smono' AND entity_type = 'K8sResource'
ORDER BY relative_path, start_line, entity_id LIMIT 500;
EXPLAIN (ANALYZE, BUFFERS)
SELECT entity_id, entity_name FROM content_entities
WHERE repo_id = 'repository:k8smono' AND entity_type = 'K8sResource'
ORDER BY relative_path, start_line, entity_id LIMIT 500;
\echo '=== Q6a name-rare big/zxqvlogin ==='
EXPLAIN (ANALYZE, BUFFERS)
SELECT entity_id FROM content_entities WHERE repo_id = 'repository:big' AND entity_name ILIKE '%zxqvlogin%' ORDER BY relative_path, start_line, entity_id LIMIT 50;
EXPLAIN (ANALYZE, BUFFERS)
SELECT entity_id FROM content_entities WHERE repo_id = 'repository:big' AND entity_name ILIKE '%zxqvlogin%' ORDER BY relative_path, start_line, entity_id LIMIT 50;
\echo '=== Q6b name-common big/handler ==='
EXPLAIN (ANALYZE, BUFFERS)
SELECT entity_id FROM content_entities WHERE repo_id = 'repository:big' AND entity_name ILIKE '%handler%' ORDER BY relative_path, start_line, entity_id LIMIT 50;
EXPLAIN (ANALYZE, BUFFERS)
SELECT entity_id FROM content_entities WHERE repo_id = 'repository:big' AND entity_name ILIKE '%handler%' ORDER BY relative_path, start_line, entity_id LIMIT 50;
\echo '=== Q7 codetopic-scoped-one-term big/zxqvlogin ==='
EXPLAIN (ANALYZE, BUFFERS)
WITH terms(term) AS MATERIALIZED (VALUES ('zxqvlogin')),
entity_probe AS (
  SELECT terms.term AS matched_term, m.repo_id, m.relative_path, m.entity_id
  FROM terms CROSS JOIN LATERAL (
    SELECT e.repo_id, e.relative_path, e.entity_id FROM content_entities e
    WHERE (e.entity_name ILIKE '%' || terms.term || '%' OR e.source_cache ILIKE '%' || terms.term || '%')
      AND e.repo_id = 'repository:big'
    LIMIT 200
  ) m
) SELECT count(*) FROM entity_probe;
EXPLAIN (ANALYZE, BUFFERS)
WITH terms(term) AS MATERIALIZED (VALUES ('zxqvlogin')),
entity_probe AS (
  SELECT terms.term AS matched_term, m.repo_id, m.relative_path, m.entity_id
  FROM terms CROSS JOIN LATERAL (
    SELECT e.repo_id, e.relative_path, e.entity_id FROM content_entities e
    WHERE (e.entity_name ILIKE '%' || terms.term || '%' OR e.source_cache ILIKE '%' || terms.term || '%')
      AND e.repo_id = 'repository:big'
    LIMIT 200
  ) m
) SELECT count(*) FROM entity_probe;
\echo '=== Q8 reap-leg big ==='
EXPLAIN (ANALYZE, BUFFERS)
SELECT ce.entity_id FROM content_entities ce WHERE ce.repo_id = 'repository:big';
\echo '=== Q9 jsx-component big/TopNav ==='
EXPLAIN (ANALYZE, BUFFERS)
SELECT entity_id FROM content_entities WHERE repo_id = 'repository:big' AND coalesce(metadata -> 'jsx_component_usage', '[]'::jsonb) ? 'TopNav' ORDER BY relative_path, start_line, entity_name LIMIT 50;
EXPLAIN (ANALYZE, BUFFERS)
SELECT entity_id FROM content_entities WHERE repo_id = 'repository:big' AND coalesce(metadata -> 'jsx_component_usage', '[]'::jsonb) ? 'TopNav' ORDER BY relative_path, start_line, entity_name LIMIT 50;
\echo '=== Q10 deadcode-scoped big/Function/javascript ==='
EXPLAIN (ANALYZE, BUFFERS)
SELECT entity_id FROM content_entities
WHERE ('repository:big' = '' OR repo_id = 'repository:big') AND entity_type = 'Function' AND ('' = '' OR lower(coalesce(language, '')) = '')
ORDER BY repo_id, relative_path, entity_name, entity_id LIMIT 50 OFFSET 0;
\echo '=== Q11 by-ids-leg big ==='
EXPLAIN (ANALYZE, BUFFERS)
SELECT entity_id FROM content_entities WHERE repo_id = 'repository:big' AND entity_id = ANY('{repository:big:e1,repository:big:e2,repository:big:e999}'::text[]);
