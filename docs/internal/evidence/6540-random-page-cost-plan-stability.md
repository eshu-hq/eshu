# 6540 — migration 107 plan stability across `random_page_cost` 4.0 and 1.1

Companion to [6540-content-entities-language-type-index.md](6540-content-entities-language-type-index.md),
which carries the candidate comparison, the cost accounting and the correctness
differential. This file answers one question that note left open, and is
separate only because that note is at its 500-line cap.

## The question

The candidate comparison in the main note was measured at
`random_page_cost = 4.0`, the Postgres default. This repo does not run that
value: `docs/public/reference/postgres-tuning.md:95` recommends **1.1** for SSD
and `:101` records the pipeline running against Compose Postgres at 1.1.

That gap is load-bearing rather than pedantic, because the repo has committed
proof that an index choice can invert with this knob — `postgres-tuning.md:116`,
the #5490 `K8sResource` case, where 4.0 keeps the old index plus an explicit
`Sort` and 1.1 naturally picks the ordered `Index Scan`, same query, same data,
byte-identical result set.

So: **is migration 107 adopted at 1.1, and does the plan shape hold there?**

## Environment

- PostgreSQL **16.15** (Debian, aarch64), disposable container, dropped after.
- 2,000,000 rows / 600 repositories, seeded with the same md5-scattering
  generator `seedZeroMatchCorpus` uses, so `relative_path` is uncorrelated with
  physical order.
- `content_entities` from migration 004, with **104 and 107 applied by hand from
  the migration text**. That bound is stated because it is the same limitation
  the main note discloses for its own re-measurement.
- `VACUUM ANALYZE`d; `jit = off`; `max_parallel_workers_per_gather = 0`; five
  settling executions before each measured `EXPLAIN (ANALYZE, BUFFERS)`.
- The statement under test is the `EXISTS`-gated form the reader actually sends,
  not the pre-gate statement the original ladder profiled.

## Result

| schema | `random_page_cost` | zero-match `(hcl,Function)` | matching `(go,Function)` | index chosen for the read | `Sort` node |
| --- | --- | ---: | ---: | --- | --- |
| main only (104) | 4.0 | 0.040 ms | 0.459 ms | `content_entities_path_idx` | present |
| main only (104) | 1.1 | 0.046 ms | 0.464 ms | `content_entities_path_idx` | present |
| main + **107** | 4.0 | 0.029 ms | 0.154 ms | `content_entities_language_type_path_idx` | **absent** |
| main + **107** | 1.1 | 0.031 ms | 0.156 ms | `content_entities_language_type_path_idx` | **absent** |

1. **107 is adopted at 1.1, not only at 4.0.** Same index, same plan shape.
2. **`random_page_cost` flips nothing here.** Neither the index chosen nor the
   presence of the `Sort` changes between 4.0 and 1.1 in either schema. The
   #5490 inversion does not reproduce for this statement.
3. **Without 107 the ordering is never served, at either setting.** Main's 104
   supplies the access path but not the sort order, so the plan carries an
   `Incremental Sort` regardless of the knob.

## The contrast pair at 1.1, matching arm

Without 107:

```
Limit  (actual time=0.290..0.476 rows=50 loops=1)
  Buffers: shared hit=287
  InitPlan 1 (returns $0)
    -> Seq Scan on content_entities content_entities_1 (actual rows=1)
         Filter: language = 'go' AND entity_type = 'Function'
  -> Incremental Sort  (actual time=0.289..0.473 rows=50 loops=1)
       Sort Key: relative_path, start_line, entity_name
       Presorted Key: relative_path
       -> Result
            One-Time Filter: $0
            -> Index Scan using content_entities_path_idx
                 Filter: language = 'go' AND entity_type = 'Function'
                 Rows Removed by Filter: 225
Execution Time: 0.493 ms
```

With 107:

```
Limit  (actual time=0.026..0.122 rows=50 loops=1)
  Buffers: shared hit=55
  InitPlan 1 (returns $0)
    -> Seq Scan on content_entities content_entities_1 (actual rows=1)
         Filter: language = 'go' AND entity_type = 'Function'
  -> Result
       One-Time Filter: $0
       -> Index Scan using content_entities_language_type_path_idx
            Index Cond: language = 'go' AND entity_type = 'Function'
Execution Time: 0.139 ms
```

At 1.1 that is **3.5x on execution time and 5.2x on buffers**, with the
post-scan `Filter` becoming an `Index Cond` and the `Incremental Sort` gone.

## What this does not claim

These are warm, single-sample `EXPLAIN ANALYZE` runs on a hand-built schema and
a synthetic seed, on a shared laptop. They are directional for the **plan
choice**, which is what was in question, and are not offered as a repo-scale
latency contract. The zero-match arm is short-circuited by the `EXISTS` gate in
every cell, which is why its four numbers are nearly flat and why the matching
arm is the informative one.

**No-Regression Evidence:** this change adds one index and alters no query
builder, so the only runtime deltas are the index build at migration time and
the planner's choice on the ordered page read. Both are measured above at both
planner settings: strict improvement on every arm timed, regression on none, and
no plan inversion between 4.0 and 1.1.

**No-Observability-Change:** no metric, span, log, or status surface is added,
removed, or renamed by this diff.
