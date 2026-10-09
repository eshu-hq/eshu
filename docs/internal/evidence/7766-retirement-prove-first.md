# Evidence: #7766 prove-first results for repository retirement

Issue #7766. The design for operator-driven repository retirement rested on
three theories that could degrade a hot path, so each was measured before any
code was written: the phase 1 critical section is short (P1), the commit gate
is free (P2), and a shared-projection worker drops intents whose acceptance or
generation is gone (P9). P1 and P9 failed as designed, and P2 passed only after
the gate was folded into the scope upsert. The design was amended per the
arbiter ruling on these results. The design files are
`docs/internal/design/7766-repository-retirement*.md`; this note is the record
of the numbers and the material to re-run them.

Scope: Postgres only, no graph backend. The harness ran against a throwaway
container and never touched shared data. Everything under "Results" was
measured on 2026-10-08. Nothing here measures the final design: P1', P1q, P2',
and P9a to P9d are still owed (see the design's prove-first table).

## Method

### Environment

- PostgreSQL 18.3 (aarch64-musl), image
  `postgres:18-alpine@sha256:4da1a4828be12604092fa55311276f08f9224a74a62dcb4708bd7439e2a03911`,
  `shared_buffers` 2GB, fsync on, extension `pg_buffercache`. Go toolchain 1.26.9.
- Host: Apple M5, 10 cores, 32 GB, under other load. Absolute times are a
  local-dev figure, not a production one.
- Schema: the repository's own bootstrap (`postgres.ApplyBootstrap`), so every
  index and trigger the real queue carries was present.
- Percentiles are nearest-rank. At n=10 and n=20 every "p99" is the maximum.
- Cold means the touched relations and their indexes were evicted from
  `shared_buffers` with `pg_buffercache_evict_relation` (the OS cache stayed
  warm). Warm means prewarmed by a read pass over the target rows.

### Data

- Background: 12,000 scopes and 729,462 generations, lognormal per-scope counts
  (p50 27, p90 141, p99 522, max 3,280), heavier than the QA shape's p99 of 79.
  Status mix: 12,000 active, 941 pending, 325 failed, the rest superseded. Two
  reducer rows and one projector row per generation. The final database held
  2.2M generations and 8.0M work items, about 3x QA scale.
- Shapes: **R** has one pending, one active, and one failed generation and the
  rest superseded. **W** has every non-active generation pending or failed. **M**
  is a 25-repo request (one 5,000, one 3,280, and 23 of 79 generations,
  realistic statuses). Each target scope has four reducer rows per generation
  and one expired claimed reducer row.
- Family suffix: `c` cold commit-mode runs, `h` warm commit-mode runs, `w` a
  separate scope set for rollback-mode runs (measured cold or warm), `Bk` the
  blocking runs. `Rw5000` is the R shape with 5,000 generations in the `w` set.
  `Rc`, `Rh`, `Wc`, and `Wh` have 20 groups each (n=20). `Mc` and the `w` and
  `Bk` families have 10 (n=10).

### What was timed

The P1 driver replays the design's phase 1 on one connection: the advisory
locks and the scope-row lock outside the window, then
`LOCK TABLE fact_work_items IN EXCLUSIVE MODE`, the live-lease recheck, steps 7a
to 7g (the design-form 7f lease-horizon read is included), and `COMMIT` or
`ROLLBACK`. `lock_to_commit_ms` runs from the lock request to the end of the
commit.

- **Design form** binds 7c, 7d, and the recheck to every
  `(scope_id, generation_id)` pair. 7d is a DELETE.
- **Tuned** binds 7c to the non-superseded pairs and 7d and the recheck to
  `scope_id = ANY`. 7d is still a DELETE (the final design marks instead, which
  P1' will measure).
- Variant `ne7a` changes 7a's predicate to `status <> 'superseded'`. Variant
  `scope` binds 7c and 7d by scope. Both are exploratory.

Design-form figures are n=20 in commit mode, except the 25-repo group at n=10.
Tuned and exploratory figures are n=10 in rollback mode. The two modes are not
the same total and must not be compared as a speedup.

## Results

### P1: design form, commit mode (failed)

Milliseconds, lock request to commit. The last four columns are mean
per-statement times; `(r)` is rows affected.

| Shape | Cache | n | p50 | p90 | p99 (= max) | 7a | 7c | 7d | commit |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| R79 | cold | 20 | 7.1 | 8.9 | 22.3 | 1.0 (3r) | 1.7 | 0.8 (8r) | 1.9 |
| R79 | warm | 20 | 5.6 | 8.0 | 14.3 | 0.7 (3r) | 1.1 | 0.7 (8r) | 1.8 |
| R3280 | cold | 20 | 69.6 | 83.1 | 96.2 | 1.1 (3r) | 37.5 | 14.7 (8r) | 2.0 |
| R3280 | warm | 20 | 55.5 | 62.5 | 64.7 | 0.7 (3r) | 27.0 | 13.9 (8r) | 2.1 |
| R5000 | cold | 20 | 173.3 | 249.0 | **252.2** | 1.7 (3r) | 111.6 | 23.2 (8r) | 2.7 |
| R5000 | warm | 20 | 102.3 | 114.3 | 114.9 | 0.9 (3r) | 60.4 | 21.2 (8r) | 2.5 |
| M, 25 repos (10,097 generations) | cold | 10 | 171.8 | 180.8 | 208.2 | 6.4 (75r) | 90.4 | 43.0 (200r) | 4.0 |
| W3280 | cold | 20 | 204.2 | 214.8 | 234.7 | 64.7 (3,280r) | 54.6 | 62.7 (13,116r) | 8.1 |
| W5000 | cold | 20 | 346.9 | 452.0 | **885.9** | 135.9 (5,000r) | 101.6 | 100.7 (19,996r) | 19.0 |
| W5000 | warm | 20 | 301.4 | 349.6 | **411.9** | 84.6 (5,000r) | 97.2 | 100.3 (19,996r) | 12.7 |

Commit added 1.8 to 19.0 ms to the section. R5000 cold missed the 250 ms bar by
2.2 ms, and the stalled shape missed it by a wide margin. The pass text "7a
touches at most 3 rows" counted statuses on a current projector and was never
an invariant: the stalled shape updated 5,000 rows. An uncontrolled first pass
on R5000 (cache as found, n=20) gave p50 95.0, p90 264.4, and max 664.7 ms; it
is exploratory and not used.

### P1: tuned and exploratory forms, rollback mode, n=10

| Shape | Cache | Form | p50 | max | Notes |
| --- | --- | --- | --- | --- | --- |
| Rw5000 | cold | design | 152.9 | 194.2 | |
| Rw5000 | warm | design | 74.3 | 88.7 | |
| Rw5000 | cold | tuned | 27.9 | 88.5 | 7c 6.5 ms mean |
| Rw5000 | warm | tuned | 12.0 | 19.3 | |
| Rw3280 | cold | tuned | 15.5 | 28.7 | |
| Ww5000 | cold | tuned | 114.4 | 184.9 | 7c 32.3 ms mean |
| Ww5000 | warm | tuned | 87.3 | 140.1 | |
| Rw5000 | cold | `ne7a` | 123.0 | 321.2 | 7a 67.5 ms on 3 rows |
| Rw5000 | warm | `ne7a` | 77.0 | 91.9 | |
| Rw5000 | cold | `scope` | 46.9 | 180.2 | |
| Rw5000 | warm | `scope` | 27.5 | 41.8 | |

Caveat: the tuned Ww5000 files record 107 to 171 rows for 7a and 106 to 170 for
7c per run, not 5,000, while 7d records 19,996. So the 184.9 ms figure does not
prove the 5,000-row 7a and 7c leg. The cause was not investigated, and P1'
re-measures at the precheck limits in commit mode.

### P1: fleet pause

Claim-shaped statements ran against unrelated scopes while the section ran:
three that claim a pending row, two heartbeat-shaped, and one enqueue-shaped,
all design-form (the three shapes are in the SQL below). The real projector
claim did not finish in 150 s on this fixture, so the harness used proxies of
the same shape. n=10 per family, milliseconds.

| Shape | Section p50 | Section max | Blocked statement max | Baseline statement p50 |
| --- | --- | --- | --- | --- |
| R5000 | 47.9 | 99.4 | 100.5 | 1.1 |
| W5000 | 115.5 | 229.1 | 230.5 | 1.1 |
| R79, excluding run 1 | 4.0 | 4.8 | 5.4 | 0.8 |

R79 run 1 stalled: the lock request waited 451,585.3 ms (the section took
451,635.6 ms) behind a leftover real claim statement of about 7.5 minutes. That
is the convoy the design guards against. Claim-shaped statements started inside
the section: 60 for R5000, 58 for W5000, and 54 for R79 without run 1.

### P1: convoy

A `LOCK TABLE fact_work_items IN EXCLUSIVE MODE` waited 6,975.070 ms behind a
`ROW EXCLUSIVE` holder. An unrelated single-row `UPDATE` then waited 5,963.016
ms behind that waiter. The convoy came from a `SET LOCAL lock_timeout` inherited
by the table lock, not from the advisory wait.

### P2: the commit gate

- **Lookup.** At 0 rows the planner used a sequential scan, 0.006 to 0.016 ms.
  At 100 and 10,000 rows it used an index scan on
  `repository_retirements_open_repo_idx`: 0.011 to 0.046 ms in custom plans and
  0.011 to 0.021 ms in generic plans (prepared after five executions, as `pgx`
  runs it). `pgbench` over a unix socket, one client, prepared protocol, 8 s
  each: protocol floor 0.004 ms; miss 0.005 ms at 0 rows and 0.008 ms at
  10,000; hit 0.004 ms and 0.009 ms.
- **End to end.** A scratch benchmark committed one generation per arm per
  iteration (gate off, separate lookup, folded into the upsert), rotating the
  arm order through six permutations so each arm saw the same database state.
  Median delta against the gate off:

| Marker rows | Facts | Runs | Off (ms/commit) | Separate lookup | Folded into upsert (sd) |
| --- | --- | --- | --- | --- | --- |
| 0 | 1 | 6 | 5.64 | +3.04% (+183 us) | +0.46% (1.25) |
| 0 | 400 | 4 | 41.06 | +1.05% | +0.82% (0.92) |
| 10,000 | 1 | 6 | 7.81 | +2.25% (+195 us) | -0.09% (1.10) |
| 10,000 | 400 | 4 | 38.20 | +0.18% | -0.39% (1.12) |

The separate lookup fails the 1% bar at facts=1, and the folded form is inside
the noise everywhere. The harness measured its own copy of the upsert, not the
real commit code, and facts=400 had four runs.

### P9: shared worker and orphan intents

Real Postgres, the real shared worker.

| Case | Result |
| --- | --- |
| Control: acceptance present, generation active | processed 1, edges written, intent completed |
| Control: acceptance points at a newer generation | filtered as stale, no write, completed |
| Acceptance deleted before selection | processed 0 in each of 5 cycles, intent never completed |
| Generation deleted before selection (the cascade left 0 acceptance rows) | same |
| Acceptance, generation, or intent deleted mid-batch | worker still wrote edges |
| After phase 1 (generation superseded, acceptance and intent kept) | processed 1, edges written |

An intent without an acceptance row is neither filtered nor completed, and the
worker skips it on every cycle. The last row shows the shared worker does not
read generation status, so superseding the generation does not fence it.

Starvation: 10,100 orphan intents (20 distinct acceptance keys, no acceptance
rows) plus one healthy intent. One run took 2.123 s (selection 2.112 s),
processed 0, wrote 0, and left the healthy intent uncompleted. An earlier run
of the same test processed the healthy intent in 10 ms. The difference is
unexplained.

Horizon (P9H): a 4.025 s write cycle, the design's horizon taken at t=300 ms.

| t | `now() > horizon` | Lease expiry past horizon | Worker |
| --- | --- | --- | --- |
| 0.801 s | false | +0.50 s | inside `RetractEdges` |
| 1.501 s | true | +1.00 s | inside `RetractEdges` |
| 2.501 s | true | +2.00 s | inside `RetractEdges` |
| 3.501 s | true | +3.00 s | inside `RetractEdges` |

The horizon wait would have passed at 1.5 s with the writer holding a renewed
lease until 4.0 s. The design's wait was unsound, and the intent-delete barrier
replaced it.

## Reproducing

Order: start a disposable Postgres 18 with `shared_buffers=2GB` and
`pg_buffercache`; apply the bootstrap schema; run the SQL below in order; build
the drivers described at the end. Pass the DSN through an environment variable
(`PROOF_PG_DSN`); no credential belongs in a file.

### Background scopes and generations

```sql
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
```

### Background work items

```sql
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
```

### Leases and reindex requests

```sql
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
```

### Target scopes

Run once before the next block: `CREATE TABLE _targets (scope_id text primary
key, repo_id text, variant text, grp text, n int);`. The `w` and `Bk` families
(10 groups each) come from the same statement with
`('Rw79','R',79),('Rw3280','R',3280),('Rw5000','R',5000),('Ww5000','W',5000),
('Bk5000','R',5000),('Bk79','R',79),('BkW5000','W',5000)` in place of the `c`
and `h` list and `generate_series(1,10)` in place of `(1,20)`.

```sql
SET synchronous_commit = off;
DROP TABLE IF EXISTS _targets_new;
CREATE TABLE _targets_new (scope_id text primary key, repo_id text, variant text, grp text, n int);
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
```

### P1 timed statements

The design-form statements the driver ran, in order. `$1` is the scope id
array, and the pair form unnests `(scope_id, generation_id)` arrays.

```sql
-- step 4 read, step 5 lock (outside the timed window)
SELECT scope_id, generation_id FROM scope_generations WHERE scope_id = ANY($1) ORDER BY scope_id, generation_id;
SELECT scope_id FROM ingestion_scopes WHERE partition_key = ANY($1) ORDER BY scope_id FOR NO KEY UPDATE;
-- timed window
LOCK TABLE fact_work_items IN EXCLUSIVE MODE;
SELECT COUNT(*) FROM fact_work_items AS w WHERE w.stage = 'reducer' AND w.status IN ('claimed','running') AND w.claim_until > clock_timestamp() AND (w.scope_id, w.generation_id) IN (SELECT * FROM unnest($1::text[], $2::text[]) AS affected(scope_id, generation_id));  -- tuned: w.scope_id = ANY($1)
-- 7a
UPDATE scope_generations SET status='superseded', superseded_at=$2 WHERE scope_id = ANY($1) AND status IN ('pending','active','failed');
-- 7b
UPDATE ingestion_scopes SET active_generation_id = NULL WHERE scope_id = ANY($1);
-- 7c (pair form; tuned binds only the non-superseded pairs)
UPDATE fact_work_items SET status='superseded', failure_class='repository_retired', lease_owner=NULL, claim_until=NULL, visible_at=NULL, next_attempt_at=NULL, updated_at=$3 WHERE stage='projector' AND (scope_id, generation_id) IN (SELECT * FROM unnest($1::text[], $2::text[]) AS affected(scope_id, generation_id)) AND (status IN ('pending','retrying') OR (status IN ('claimed','running') AND claim_until <= clock_timestamp()));
-- 7d (pair form; tuned: scope_id = ANY($1)); a DELETE in the proven form
DELETE FROM fact_work_items WHERE stage='reducer' AND (scope_id, generation_id) IN (SELECT * FROM unnest($1::text[], $2::text[]) AS affected(scope_id, generation_id)) AND status IN ('pending','retrying','failed','dead_letter','claimed','running') AND NOT (status IN ('claimed','running') AND claim_until > clock_timestamp());
-- 7e, 7f (design form only), 7g
DELETE FROM repository_reindex_requests WHERE scope_id = ANY($1);
SELECT max(lease_expires_at) FROM shared_projection_partition_leases WHERE lease_owner IS NOT NULL AND lease_expires_at > clock_timestamp();
-- 7g: INSERT INTO repository_retirements (...) SELECT ... FROM unnest($1::text[], $2::text[]) ON CONFLICT (repo_id) WHERE readmitted_at IS NULL DO NOTHING
```

The three claim-shaped proxies of the fleet-pause runs:

```sql
UPDATE fact_work_items SET status='claimed', lease_owner='proof-claimer', claim_until=now()+interval '10 minutes', attempt_count=attempt_count+1, updated_at=now() WHERE work_item_id=$1 AND status='pending';
UPDATE fact_work_items SET claim_until = now()+interval '30 days', updated_at=now() WHERE work_item_id=$1;
INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, status, visible_at, created_at, updated_at) SELECT $1, g.scope_id, g.generation_id, 'projector', 'source_local', 'succeeded', NULL, now(), now() FROM scope_generations g WHERE g.generation_id=$2;
```

### P2 marker rows

The marker table was the first-draft design DDL. P2 relied on these columns
and indexes, which the current design keeps: `repo_id TEXT NOT NULL`,
`state`, `readmitted_at TIMESTAMPTZ NULL`, a unique partial index
`repository_retirements_open_repo_idx ON (repo_id) WHERE readmitted_at IS NULL`,
and the two secondary indexes. The separate lookup was `SELECT retirement_id,
state FROM repository_retirements WHERE repo_id = $1 AND readmitted_at IS NULL
LIMIT 1`; the folded form is in the concurrency contract. Row loader (psql
variable `n`, 90% open, 10% readmitted history):

```sql
TRUNCATE repository_retirements;
INSERT INTO repository_retirements (retirement_id, repo_id, scope_id, state, phase, reason_code, reason_hash, actor_class, idempotency_key_hash, scope_id_hash, generation_ids_hash, generations_fenced, next_attempt_at, requested_at, updated_at, retired_at, readmitted_at)
SELECT 'rr_'||lpad(i::text,14,'0'), 'repo:rr:'||lpad(i::text,6,'0'), 'scope:rr:'||i,
       (ARRAY['complete','complete','pending','running','blocked','failed'])[1 + i % 6], 'done', 'operator_retired', 'h','admin','k','s','g', 3,
       now(), now(), now(), now(), CASE WHEN i % 10 = 0 THEN now() END
FROM generate_series(1, :n) i;
```

## Harness not committed

The Go drivers are scratch code that built against the repository at the time
and are not reproduced here. What each did, so they can be rewritten:

- **P1 driver** (about 590 lines): modes `explain` (`EXPLAIN (ANALYZE, BUFFERS)`
  of each statement), `time` (the loop in "What was timed", one transaction per
  target group, cache eviction or prewarm before each run), and `block` (the
  same section while 3 claim-shaped, 2 heartbeat-shaped, and 1 enqueue-shaped
  worker ran against unrelated scopes, recording each statement's start and
  end). Flags: family, runs, variant, cache state, commit or rollback.
  Percentiles are nearest-rank. A small script summarised the CSVs.
- **P2 benchmark** (about 120 lines, plus an 87-line gate shim and a 30-line
  patch that hooked it into the commit path): a real-Postgres benchmark on
  `IngestionStore.CommitScopeGeneration` with 64 scopes and 1 or 400 facts per
  generation, three arms per iteration in rotating order.
- **P9 tests** (about 330 lines): three Go tests on the shared worker with real
  Postgres. One drove the control, orphan, mid-batch, and after-phase-1 cases
  through five cycles each. One ran a 4 s write cycle and polled the lease row
  at fixed offsets (P9H). One seeded 10,100 orphan intents plus one healthy
  intent and timed one cycle.
- **Schema loader and claim probe** (25 and 29 lines): apply the bootstrap
  schema; run the real projector and reducer claim once with a 150 s deadline.

## Performance Evidence

Performance Evidence: on the scratch fixture (12,000 scopes, 2.2M generations,
8.0M work items), the design-form phase 1 held `fact_work_items` in `EXCLUSIVE`
mode for a p99 of 252.2 ms on the realistic 5,000-generation scope (cold) and
885.9 ms on the stalled one, failing the 250 ms bar. The tuned bindings measured
88.5 ms and 184.9 ms in rollback mode, which is not a pass: the tuned stalled
runs touched about 170 generation rows, not 5,000. The separate commit-gate
lookup cost +3.04% median at facts=1, and the folded gate +0.46%.

## Observability Evidence

No-Observability-Change: this note records measurements of a design. It adds no
metric, span, log, status, or audit output.
