SET jit = off;
\echo '=== S1 codetopic tiny-repo rare ==='
EXPLAIN (ANALYZE, BUFFERS)
WITH terms(term) AS MATERIALIZED (VALUES ('zxqvlogin')),
entity_probe AS (
  SELECT terms.term AS matched_term, m.repo_id, m.relative_path, m.entity_id
  FROM terms CROSS JOIN LATERAL (
    SELECT e.repo_id, e.relative_path, e.entity_id FROM content_entities e
    WHERE (e.entity_name ILIKE '%' || terms.term || '%' OR e.source_cache ILIKE '%' || terms.term || '%')
      AND e.repo_id = 'repository:tiny'
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
      AND e.repo_id = 'repository:tiny'
    LIMIT 200
  ) m
) SELECT count(*) FROM entity_probe;
\echo '=== S2 codetopic small-repo rare ==='
EXPLAIN (ANALYZE, BUFFERS)
WITH terms(term) AS MATERIALIZED (VALUES ('zxqvlogin')),
entity_probe AS (
  SELECT terms.term AS matched_term, m.repo_id, m.relative_path, m.entity_id
  FROM terms CROSS JOIN LATERAL (
    SELECT e.repo_id, e.relative_path, e.entity_id FROM content_entities e
    WHERE (e.entity_name ILIKE '%' || terms.term || '%' OR e.source_cache ILIKE '%' || terms.term || '%')
      AND e.repo_id = 'repository:small'
    LIMIT 200
  ) m
) SELECT count(*) FROM entity_probe;
\echo '=== S3 k8s-ssd big ==='
SET random_page_cost = 1.1;
EXPLAIN (ANALYZE, BUFFERS)
SELECT entity_id, entity_name FROM content_entities
WHERE repo_id = 'repository:big' AND entity_type = 'K8sResource'
ORDER BY relative_path, start_line, entity_id LIMIT 500;
RESET random_page_cost;
\echo '=== S4 k8s-ssd k8smono ==='
SET random_page_cost = 1.1;
EXPLAIN (ANALYZE, BUFFERS)
SELECT entity_id, entity_name FROM content_entities
WHERE repo_id = 'repository:k8smono' AND entity_type = 'K8sResource'
ORDER BY relative_path, start_line, entity_id LIMIT 500;
RESET random_page_cost;
\echo '=== S5 reap-leg tiny ==='
EXPLAIN (ANALYZE, BUFFERS)
SELECT ce.entity_id FROM content_entities ce WHERE ce.repo_id = 'repository:tiny';
