\timing on
SET synchronous_commit = off;
-- projector row per generation
INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, status, visible_at, created_at, updated_at, failure_class)
SELECT 'proj:'||g.generation_id, g.scope_id, g.generation_id, 'projector', 'source_local',
       CASE g.status WHEN 'pending' THEN 'pending' WHEN 'failed' THEN 'failed' ELSE 'succeeded' END,
       CASE WHEN g.status='pending' THEN g.ingested_at END,
       g.ingested_at, g.ingested_at + interval '3 minutes',
       CASE WHEN g.status='failed' THEN 'projection_error' END
FROM scope_generations g WHERE g.scope_id LIKE 'scope:bg:%';
-- two reducer rows per generation
INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, status, visible_at, created_at, updated_at, payload)
SELECT 'red:'||d.dom||':'||g.generation_id, g.scope_id, g.generation_id, 'reducer', d.dom,
       CASE g.status WHEN 'pending' THEN 'pending' WHEN 'failed' THEN 'dead_letter' ELSE 'succeeded' END,
       CASE WHEN g.status='pending' THEN g.ingested_at END,
       g.ingested_at, g.ingested_at + interval '5 minutes', '{"source_system":"git"}'::jsonb
FROM scope_generations g CROSS JOIN (VALUES ('workload_materialization'),('deployment_mapping')) d(dom)
WHERE g.scope_id LIKE 'scope:bg:%';
