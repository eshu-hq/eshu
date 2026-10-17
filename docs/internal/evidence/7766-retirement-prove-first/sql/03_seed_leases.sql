\timing on
SET synchronous_commit = off;
-- live projector leases on 60 other (background) scopes, expired ones on 20 more
WITH pick AS (SELECT s.scope_id, row_number() OVER (ORDER BY s.scope_id) rn FROM ingestion_scopes s WHERE s.scope_id LIKE 'scope:bg:%' AND (substr(s.scope_id,10)::int % 150)=7)
UPDATE fact_work_items w SET status = CASE WHEN p.rn <= 60 THEN 'running' ELSE 'claimed' END,
       lease_owner = 'proj-live', claim_until = CASE WHEN p.rn <= 60 THEN now() + interval '30 days' ELSE now() - interval '1 hour' END
FROM pick p, ingestion_scopes s
WHERE s.scope_id = p.scope_id AND w.work_item_id = 'proj:'||s.active_generation_id AND p.rn <= 80;
-- live + expired reducer leases (one per scope: live-lease unique index)
WITH pick AS (SELECT s.scope_id, s.active_generation_id, row_number() OVER (ORDER BY s.scope_id) rn FROM ingestion_scopes s WHERE s.scope_id LIKE 'scope:bg:%' AND (substr(s.scope_id,10)::int % 150)=11)
UPDATE fact_work_items w SET status = CASE WHEN p.rn <= 40 THEN 'claimed' WHEN p.rn <= 60 THEN 'running' ELSE 'claimed' END,
       lease_owner = 'red-live', claim_until = CASE WHEN p.rn <= 60 THEN now() + interval '30 days' ELSE now() - interval '1 hour' END
FROM pick p
WHERE w.work_item_id = 'red:workload_materialization:'||p.active_generation_id AND p.rn <= 80;
-- reindex requests on 200 background scopes
INSERT INTO repository_reindex_requests (scope_id, requested_at)
SELECT scope_id, now() FROM ingestion_scopes WHERE scope_id LIKE 'scope:bg:%' AND (substr(scope_id,10)::int % 60)=3;
-- shared projection partition leases: 4 domains x 8 partitions, some live
INSERT INTO shared_projection_partition_leases (projection_domain, partition_id, partition_count, lease_owner, lease_expires_at, updated_at)
SELECT d.dom, p, 8, CASE WHEN (p % 3)=0 THEN 'shared-worker-'||p END,
       CASE WHEN (p % 3)=0 THEN clock_timestamp() + interval '30 days' WHEN (p%3)=1 THEN clock_timestamp() - interval '1 hour' END, now()
FROM (VALUES ('platform_infra'),('workload_dependency'),('inheritance_edges'),('sql_relationships')) d(dom), generate_series(0,7) p;
