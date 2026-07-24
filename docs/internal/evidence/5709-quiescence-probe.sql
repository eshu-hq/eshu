-- #5709 quiescence-probe theory-proof shim.
-- Faithful minimal schema: the real fact_work_items_scope_generation_idx
-- (scope_id, generation_id, status, updated_at DESC) from migration 005, and
-- ingestion_scopes (scope_id, collector_kind, active_generation_id) from its
-- migration. FKs dropped for the shim (they do not affect the SELECT plan).

CREATE TABLE ingestion_scopes (
    scope_id TEXT PRIMARY KEY,
    collector_kind TEXT NOT NULL,
    active_generation_id TEXT NULL
);
CREATE INDEX ingestion_scopes_collector_kind_idx ON ingestion_scopes (collector_kind);

CREATE TABLE fact_work_items (
    work_item_id TEXT PRIMARY KEY,
    scope_id TEXT NOT NULL,
    generation_id TEXT NOT NULL,
    stage TEXT NOT NULL,
    domain TEXT NOT NULL,
    status TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX fact_work_items_scope_generation_idx
    ON fact_work_items (scope_id, generation_id, status, updated_at DESC);

-- Worst case: 500 scopes across collector kinds, each active; 20 generations
-- per scope; 50k projector work items spread across scopes/statuses. A handful
-- of scopes retain LIVE projector work (still projecting); most are quiescent.
INSERT INTO ingestion_scopes (scope_id, collector_kind, active_generation_id)
SELECT 'scope-' || s,
       (ARRAY['oci_registry','ec2','git_repository','kubernetes','cloud'])[1 + (s % 5)],
       'gen-' || s || '-' || (s % 20)
FROM generate_series(1, 500) AS s;

-- 50k projector work items; scopes 1..20 keep some 'pending'/'running' (live),
-- the rest are 'succeeded' (quiescent).
INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, status, updated_at)
SELECT 'wi-' || i,
       'scope-' || (1 + (i % 500)),
       'gen-' || (1 + (i % 500)) || '-' || (i % 20),
       'projector',
       'oci_manifest',
       CASE WHEN (1 + (i % 500)) <= 20 AND (i % 7) = 0 THEN 'pending' ELSE 'succeeded' END,
       now()
FROM generate_series(1, 50000) AS i;

ANALYZE ingestion_scopes;
ANALYZE fact_work_items;

-- The quiescence probe: producer scopes of a given collector kind that are
-- active AND have NO live projector work item. The NOT EXISTS body is
-- byte-equivalent to the production reducer claim query
-- (reducer_queue_claim_query.go:25-30).
EXPLAIN (ANALYZE, BUFFERS)
SELECT s.scope_id
FROM ingestion_scopes AS s
WHERE s.collector_kind = 'oci_registry'
  AND s.active_generation_id IS NOT NULL
  AND NOT EXISTS (
      SELECT 1
      FROM fact_work_items AS projector_work
      WHERE projector_work.stage = 'projector'
        AND projector_work.scope_id = s.scope_id
        AND projector_work.status IN ('pending', 'retrying', 'claimed', 'running')
  );
