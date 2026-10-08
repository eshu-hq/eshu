-- #7637 theory shim: seed a 900-scope x 25-generation store and measure the
-- old unbounded relationship listings against the floored shape.
SET search_path TO shim7637;
\set QUIET on
\timing off

-- 1. Seed scopes: 1..898 active at gen 25, 899 never-activated, 900 failed-shape.
INSERT INTO ingestion_scopes (scope_id, active_generation_id)
SELECT 'scope-' || s,
       CASE WHEN s <= 898 THEN 'gen-' || s || '-25' END
FROM generate_series(1, 900) AS s;

-- 2. Generations: 25 per scope, hourly ingested_at; scope 900's latest failed.
INSERT INTO scope_generations (generation_id, scope_id, ingested_at, status)
SELECT 'gen-' || s || '-' || g, 'scope-' || s,
       timestamptz '2026-01-01' + (g || ' hours')::interval,
       CASE WHEN s = 900 AND g = 25 THEN 'failed' ELSE 'pending' END
FROM generate_series(1, 900) AS s CROSS JOIN generate_series(1, 25) AS g;

-- 3. One succeeded row per (scope, generation, domain) for the two relationship
-- domains, mirroring "one accumulates per (scope, generation, domain)".
INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, status, updated_at)
SELECT 'dm-' || s || '-' || g, 'scope-' || s, 'gen-' || s || '-' || g,
       'reducer', 'deployment_mapping', 'succeeded', timestamptz '2026-02-01' + ((s * 25 + g) || ' seconds')::interval
FROM generate_series(1, 900) AS s CROSS JOIN generate_series(1, 25) AS g;
INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, status, updated_at)
SELECT 'ci-' || s || '-' || g, 'scope-' || s, 'gen-' || s || '-' || g,
       'reducer', 'code_import_repo_edge', 'succeeded', timestamptz '2026-02-01' + ((s * 25 + g) || ' seconds')::interval
FROM generate_series(1, 900) AS s CROSS JOIN generate_series(1, 25) AS g;

VACUUM ANALYZE ingestion_scopes;
VACUUM ANALYZE scope_generations;
VACUUM ANALYZE fact_work_items;

-- 4. Shape check: one failed generation row per domain sits AT its scope floor.
SELECT work.domain, count(*) AS failed_generation_rows
FROM fact_work_items AS work
JOIN scope_generations AS gen USING (generation_id)
WHERE work.stage = 'reducer' AND work.status = 'succeeded' AND gen.status = 'failed'
GROUP BY work.domain ORDER BY work.domain;
