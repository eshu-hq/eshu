-- #5709 readiness query: is the EXISTS probe index-backed on a write-hot
-- fact_work_items, or does it degrade to a scan of the whole table?
DROP TABLE IF EXISTS fact_work_items;
CREATE TABLE fact_work_items (
  work_item_id text PRIMARY KEY, stage text NOT NULL, domain text NOT NULL,
  scope_id text NOT NULL, generation_id text NOT NULL, status text NOT NULL,
  visible_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
-- Index as shipped in migration 005.
CREATE INDEX fact_work_items_stage_domain_status_idx
  ON fact_work_items (stage, domain, status, visible_at, updated_at DESC);

-- 400,000 reducer rows across many domains. The realistic steady state: the
-- producers are DONE (succeeded), so the probe must scan for an absence --
-- the worst case for an EXISTS, since it cannot stop early.
INSERT INTO fact_work_items (work_item_id, stage, domain, scope_id, generation_id, status)
SELECT 'wi-' || value,
       'reducer',
       (ARRAY['container_image_identity','ci_cd_run_correlation','supply_chain_impact',
              'service_catalog_correlation','package_source_correlation'])[1 + (value % 5)],
       'scope-' || (value % 1000), 'gen-' || (value % 1000),
       'succeeded'
FROM generate_series(1, 400000) AS value;
ANALYZE fact_work_items;

\echo '===== ARM 1: producers all finished (absence probe -- the worst case) ====='
EXPLAIN (ANALYZE, BUFFERS, COSTS OFF)
SELECT EXISTS (
    SELECT 1 FROM fact_work_items
    WHERE stage = 'reducer'
      AND domain = ANY(ARRAY['container_image_identity']::text[])
      AND status = ANY(ARRAY['pending','claimed','retrying']::text[])
);

\echo '===== ARM 2: one producer still pending (early-exit case) ====='
UPDATE fact_work_items SET status = 'pending'
 WHERE work_item_id = 'wi-6';
ANALYZE fact_work_items;
EXPLAIN (ANALYZE, BUFFERS, COSTS OFF)
SELECT EXISTS (
    SELECT 1 FROM fact_work_items
    WHERE stage = 'reducer'
      AND domain = ANY(ARRAY['container_image_identity']::text[])
      AND status = ANY(ARRAY['pending','claimed','retrying']::text[])
);

\echo '===== ARM 3: two-producer consumer (supply_chain_impact shape) ====='
EXPLAIN (ANALYZE, BUFFERS, COSTS OFF)
SELECT EXISTS (
    SELECT 1 FROM fact_work_items
    WHERE stage = 'reducer'
      AND domain = ANY(ARRAY['container_image_identity','ci_cd_run_correlation']::text[])
      AND status = ANY(ARRAY['pending','claimed','retrying']::text[])
);
