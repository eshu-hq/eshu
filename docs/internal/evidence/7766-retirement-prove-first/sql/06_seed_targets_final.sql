\timing on
SET synchronous_commit = off;
DROP TABLE IF EXISTS _targets_new;
CREATE TABLE _targets_new (scope_id text primary key, repo_id text, variant text, grp text, n int);
-- cold extras (run 21; M:11) replace the runs pre-warmed by the EXPLAIN pass
INSERT INTO _targets_new
SELECT 'scope:t:'||fam||':'||lpad(r::text,2,'0'), 'repo:t:'||fam||':'||lpad(r::text,2,'0'), v, fam||':'||lpad(r::text,2,'0'), sz
FROM (VALUES ('Rc79','R',79),('Rc3280','R',3280),('Rc5000','R',5000),
             ('Rh79','R',79),('Rh3280','R',3280),('Rh5000','R',5000),
             ('Wc5000','W',5000),('Wc3280','W',3280),('Wh5000','W',5000)) f(fam,v,sz),
     generate_series(1,20) r;
-- 25-repo requests (1x5000, 1x3280, 23x79), realistic statuses, cold
INSERT INTO _targets_new
SELECT 'scope:t:Mc:'||lpad(r::text,2,'0')||':'||lpad(j::text,2,'0'), 'repo:t:Mc:'||lpad(r::text,2,'0')||':'||lpad(j::text,2,'0'), 'R', 'Mc:'||lpad(r::text,2,'0'),
       CASE j WHEN 1 THEN 5000 WHEN 2 THEN 3280 ELSE 79 END
FROM generate_series(1,10) r, generate_series(1,25) j;
INSERT INTO _targets SELECT * FROM _targets_new;
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, parent_scope_id, collector_kind,
                              partition_key, observed_at, ingested_at, status, active_generation_id, payload)
SELECT scope_id, 'repository', 'git', 'src:'||scope_id, NULL, 'git', repo_id, now(), now(), 'active', NULL,
       jsonb_build_object('repo_slug','org/'||repo_id)
FROM _targets_new;

INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, superseded_at, payload)
SELECT t.scope_id||':g'||k, t.scope_id, 'snapshot', ts, ts + interval '1 minute',
       CASE
         WHEN t.variant='R' AND k = t.n THEN 'pending'
         WHEN t.variant='R' AND k = t.n-1 THEN 'active'
         WHEN t.variant='R' AND k = t.n-2 THEN 'failed'
         WHEN t.variant='W' AND k = t.n-1 THEN 'active'
         WHEN t.variant='W' AND k % 2 = 0 THEN 'pending'
         WHEN t.variant='W' THEN 'failed'
         ELSE 'superseded' END,
       CASE WHEN k <= t.n-1 THEN ts + interval '2 minutes' END,
       CASE WHEN t.variant='R' AND k <= t.n-3 THEN ts + interval '1 day' END,
       '{}'::jsonb
FROM _targets_new t
CROSS JOIN LATERAL generate_series(1, t.n) k
CROSS JOIN LATERAL (SELECT now() - interval '200 days' + k * (interval '199 days' / (t.n+1)) + (hashtext(t.scope_id) % 977 + 977) * interval '1 second' AS ts) x
ORDER BY ts, t.scope_id;

UPDATE ingestion_scopes s SET active_generation_id = s.scope_id||':g'||(t.n-1)
FROM _targets_new t WHERE s.scope_id = t.scope_id;

-- projector rows
INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, status, visible_at, created_at, updated_at, failure_class, lease_owner, claim_until)
SELECT 'proj:'||g.generation_id, g.scope_id, g.generation_id, 'projector', 'source_local',
       CASE WHEN g.status='pending' AND (hashtext(g.generation_id) % 20)=0 THEN 'claimed'
            WHEN g.status='pending' THEN 'pending' WHEN g.status='failed' THEN 'failed' ELSE 'succeeded' END,
       CASE WHEN g.status='pending' THEN g.ingested_at END,
       g.ingested_at, g.ingested_at + interval '3 minutes',
       CASE WHEN g.status='failed' THEN 'projection_error' END,
       CASE WHEN g.status='pending' AND (hashtext(g.generation_id) % 20)=0 THEN 'dead-proj' END,
       CASE WHEN g.status='pending' AND (hashtext(g.generation_id) % 20)=0 THEN now() - interval '1 hour' END
FROM scope_generations g WHERE g.scope_id IN (SELECT scope_id FROM _targets_new);
-- reducer rows, 4 per generation
INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, status, visible_at, created_at, updated_at, payload)
SELECT 'red:'||d.dom||':'||g.generation_id, g.scope_id, g.generation_id, 'reducer', d.dom,
       CASE g.status WHEN 'pending' THEN 'pending' WHEN 'failed' THEN (CASE WHEN d.dom IN ('workload_materialization','deployment_mapping') THEN 'dead_letter' ELSE 'failed' END) ELSE 'succeeded' END,
       CASE WHEN g.status='pending' THEN g.ingested_at END,
       g.ingested_at, g.ingested_at + interval '5 minutes', '{"source_system":"git"}'::jsonb
FROM scope_generations g CROSS JOIN (VALUES ('workload_materialization'),('deployment_mapping'),('code_calls_x'),('repo_dep_x')) d(dom)
WHERE g.scope_id IN (SELECT scope_id FROM _targets_new);
-- exactly one expired claimed reducer row per target scope (live-lease unique index allows one per conflict key)
UPDATE fact_work_items w SET status='claimed', lease_owner='dead-red', claim_until = now() - interval '1 hour', visible_at = NULL
FROM _targets_new t
WHERE w.work_item_id = 'red:workload_materialization:'||t.scope_id||':g'||t.n AND t.variant='R'
   OR w.work_item_id = 'red:workload_materialization:'||t.scope_id||':g2' AND t.variant='W';
-- one reindex request per target scope
INSERT INTO repository_reindex_requests (scope_id, requested_at) SELECT scope_id, now() FROM _targets_new;
