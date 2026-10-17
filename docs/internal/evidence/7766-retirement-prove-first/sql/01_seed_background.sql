\timing on
SET synchronous_commit = off;
SELECT setseed(0.7766);
DROP TABLE IF EXISTS _bg_counts;
CREATE TABLE _bg_counts AS
WITH z AS (SELECT i, sqrt(-2*ln(1-random()))*cos(2*pi()*random()) AS z FROM generate_series(1,12000) i)
SELECT i, least(3280, greatest(2, round(27*exp(1.27*z))))::int AS n,
       (random() < 0.08) AS has_pending, (random() < 0.03) AS has_failed
FROM z;

INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, parent_scope_id, collector_kind,
                              partition_key, observed_at, ingested_at, status, active_generation_id, payload)
SELECT 'scope:bg:'||lpad(i::text,5,'0'), 'repository', 'git', 'src:bg:'||i, NULL, 'git',
       'repo:bg:'||lpad(i::text,5,'0'), now(), now(), 'active', NULL,
       jsonb_build_object('repo_slug','org/bg-'||i)
FROM _bg_counts;

-- generations: history rows k=1..n (k=n active, rest superseded); extra pending/failed rows.
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, superseded_at, payload)
SELECT 'gen:bg:'||lpad(c.i::text,5,'0')||':'||g.k,
       'scope:bg:'||lpad(c.i::text,5,'0'), 'snapshot',
       ts, ts + interval '1 minute',
       CASE WHEN g.k = c.n THEN 'active' WHEN g.k = 0 THEN 'pending' WHEN g.k = -1 THEN 'failed' ELSE 'superseded' END,
       CASE WHEN g.k >= 1 THEN ts + interval '2 minutes' END,
       CASE WHEN g.k BETWEEN 1 AND c.n-1 THEN ts + interval '1 day' END,
       '{}'::jsonb
FROM _bg_counts c
CROSS JOIN LATERAL (
   SELECT k FROM generate_series(1, c.n) k
   UNION ALL SELECT 0 WHERE c.has_pending
   UNION ALL SELECT -1 WHERE c.has_failed
) g
CROSS JOIN LATERAL (
   SELECT now() - interval '200 days' + (CASE WHEN g.k <= 0 THEN c.n+1 ELSE g.k END) * (interval '199 days' / (c.n+1))
          + (c.i % 977) * interval '1 second' AS ts
) t
ORDER BY ts, c.i;

UPDATE ingestion_scopes s SET active_generation_id = 'gen:bg:'||substr(s.scope_id,10)||':'||c.n
FROM _bg_counts c WHERE s.scope_id = 'scope:bg:'||lpad(c.i::text,5,'0');
