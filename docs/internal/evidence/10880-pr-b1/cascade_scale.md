# Supports generation-FK cascade at scale (#10880 PR-B1)

Refs #10880. Arbiter ruling `arbiter-b1-cascade`, acceptance check 2. Records
what the generation foreign key's cascade reads, with and without the
`(generation_id)` index that migration 338 now carries.

Harness: [cascade_scale.sh](cascade_scale.sh) with
[cascade_scale_load.sql](cascade_scale_load.sql) and
[cascade_scale_probe.sql](cascade_scale_probe.sql). Raw output:
[cascade_scale_raw.txt](cascade_scale_raw.txt). `postgres:18.6` (disposable
container, `shared_buffers=1GB`, `fsync=off`), host load about 25 during the
run (so times are noisy; buffer counts are the stable figure). The shim has the
columns and the five other indexes of migration 338 and a parent table with the
same `ON DELETE CASCADE`; 60 supports per generation, one generation per scope.

The cascade trigger runs `DELETE FROM ONLY supports WHERE generation_id = $1`
once per deleted generation, as a prepared statement that reuses a generic
plan. The probe runs that statement under `plan_cache_mode =
force_generic_plan` inside `BEGIN ... ROLLBACK`. The comparison figure is the
buffers of the supports scan node (the index and heap pages it touches), taken
on a warm cache (the last two of three probes; the first probe of each variant
reads cold pages).

| Supports | Generations | Index | Plan of the probe | Index Searches | Scan-node buffers (warm) | Probe time |
| --- | --- | --- | --- | --- | --- | --- |
| 300,000 | 5,000 | `(scope_id, generation_id)` only | Index Scan on the scope index, `Index Cond: (generation_id = $1)` | 1 | 326 | 0.30 to 0.38 ms |
| 1,200,000 | 20,000 | `(scope_id, generation_id)` only | same | 1 | 1,115 | 0.93 to 1.14 ms |
| 300,000 | 5,000 | plus `(generation_id)` | Bitmap Index Scan on the generation index | 1 | about 63 (1 to 3 index pages, 60 heap pages) | 0.11 to 0.17 ms |
| 1,200,000 | 20,000 | plus `(generation_id)` | same | 1 | about 63 (1 to 3 index pages, 60 heap pages) | 0.10 to 0.18 ms |

Without the new index the probe reads the whole scope index: 326 scan-node
buffers at 300,000 supports (about 266 index pages) and 1,115 at 1,200,000
(about 1,055 index pages; 3.4 times for 4 times the rows), one index search over
every leaf page. That is a full index pass, not a skip scan: a
skip scan would show one search per distinct scope. The cost grows with the
table. With the new index the probe stays at about 63 scan-node buffers at both sizes:
1 to 3 index pages plus the 60 heap pages of the deleted rows, which are the same
in both variants. On the index-page basis alone the figures are about 266 and
1,055 without the index against 1 to 3 with it. **Pass:** the after-index probe
buffers do not grow with table size (about 63 at 300,000 and at 1,200,000).

The real cascade, one `DELETE FROM generations` of 50 generations (the trigger
time line of `EXPLAIN (ANALYZE, BUFFERS)`; the trigger's own buffers are not in
the statement total):

| Supports | Without the index | With the index |
| --- | --- | --- |
| 300,000 | 10.8 ms (trigger), 11.7 ms (statement) | 2.1 ms (trigger), 3.0 ms (statement) |
| 1,200,000 | 39.3 ms (trigger), 39.9 ms (statement) | 2.6 ms (trigger), 3.1 ms (statement) |

The index-less time grows with the table (3.6 times for 4 times the rows); the
indexed time stays flat. The same delete runs once per retention batch and per
repository retirement, inside the scope-locked transaction, so the cost lands on
the lock hold.

PostgreSQL version: only 18.6 is measured here. PostgreSQL 17 is NOT_CHECKED.
The index stays on every version regardless: a plain `(generation_id)` index
keeps the probe at a few buffers whichever planner release runs it, and nothing
here shows that another release avoids the full index pass.
