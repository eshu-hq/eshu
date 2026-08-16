# 6154 — fact_records keyset pagination

Paging one generation's facts was quadratic in generation size. This records
what was measured, on what, and why the change is safe.

## What was wrong

`ListFactsByKind` and `ListFactsByKindAndPayloadValue` page with the keyset
cursor `(observed_at, fact_id) > ($4, $5)` under
`ORDER BY observed_at ASC, fact_id ASC`.

Every fact in a generation is stamped with one collection timestamp. On the
largest repository in the reference corpus that is 241,726 facts sharing a
single `observed_at` (`count(DISTINCT observed_at) = 1`). The cursor's leading
column therefore never advances, and `fact_id` was in no index, so no page
could skip the rows earlier pages had already returned. Each of the 484 pages
bitmap-scanned all 241,750 matching rows and top-N sorted them.

Four reducer domains walk this path once per generation:
`semantic_entity_materialization`, `sql_relationship`, `inheritance`, and
`rationale_materialization`.

## Environment

- Remote validation host: 16 vCPU, 123 GiB, Linux x86_64.
- Postgres 18.4 (`postgres:18-alpine`), the corpus store from the 896-repository
  bootstrap: 3,642,630 `fact_records` rows, 896 scopes, 896 generations,
  2,497,428 `content_entity` facts.
- Driver: pgx v5 stdlib through `database/sql`, the production path.
- Worst-case input shape: scope `…r_de3355a0`, one generation, 241,726
  `[repository, content_entity]` envelopes, 484 pages at `factBatchSize` 500.
- Corpus distribution of that load per scope: p50 285, p90 4,268, p99 34,407,
  max 241,726, total 2,498,324.

## Performance Evidence:

Built binary, warm, three runs per configuration, loading the worst-case
generation through `ListFactsByKind`. Handler row is
`RationaleEdgeMaterializationHandler.Handle` over the same generation.

| configuration | load | handler |
| --- | --- | --- |
| baseline: no index, folded statement | 84.68 / 83.78 / 84.12 s | 84.34 s |
| new index only, statement still folded | 232.94 / 232.25 / 235.32 s | 233.29 s |
| new index plus the statement split | 3.53 / 3.23 / 3.34 s | 3.46 s |

Baseline 84.34 s, after 3.46 s, a 24.4x reduction on the worst-case generation.

The middle row is the point of this document. The index alone is a 2.8x
**regression**. Under a generic plan — where these statements land, because the
services prepare them through `database/sql` — the folded predicate
`$4::timestamptz IS NULL OR (observed_at, fact_id) > (...)` cannot be pushed
down as an index qual, so the planner takes the new index and then discards
nearly every row it reads:

```
-- generic plan, folded statement, real mid-scan cursor
Index Cond: ((scope_id = $1) AND (generation_id = $2))
Filter: ((($4 IS NULL) OR (ROW(observed_at, fact_id) > ROW($4, $5))) AND (fact_kind = ANY ($3)))
Rows Removed by Filter: 385703
Execution Time: 580.611 ms

-- generic plan, cursor split onto its own statement
Index Cond: ((scope_id = $1) AND (generation_id = $2) AND (ROW(observed_at, fact_id) > ROW($4, $5)))
Filter: (fact_kind = ANY ($3))
Rows Removed by Filter: 1123
Execution Time: 1.749 ms
```

An `EXPLAIN` with literal parameters reports 4.7 ms for the folded shape and
hides this completely. A literal `EXPLAIN` is not sufficient evidence for a
change to this query; probe with `PREPARE` plus
`SET plan_cache_mode = force_generic_plan`.

Scaling was confirmed on two independent points before the fix: 241,726
envelopes in 84.12 s against 38,822 envelopes in 2.97 s — rows x6.2, time
x28.7, roughly N^1.84.

## Exactness

Bidirectional, against the worst-case generation, folded predicate versus split
predicate:

- first page: 0 rows only in folded, 0 only in split, 0 ordered mismatches
- whole-generation walk from a real mid-scan cursor: 241,226 rows on both
  sides, 0 only in folded, 0 only in split

## DDL behaviour

Measured on the corpus store:

- first apply: index built, 10.3 s to 13.2 s across three builds
- reapply: clean no-op in 0.06 s (`already exists, skipping`)
- build losing its `lock_timeout` race against a held ROW EXCLUSIVE lock:
  fails with `canceling statement due to lock timeout` and leaves **zero**
  matching indexes behind, so the next start retries cleanly
- retry once the blocker clears: succeeds
- no `ANALYZE` needed: after a drop and rebuild with no `ANALYZE`, the cursor
  query took the seek under both a custom plan (15.964 ms) and a forced generic
  plan (1.078 ms). This is a plain-column btree; the expression indexes in this
  repo do need a post-build `ANALYZE`, this one does not.

Index size 844 MB against a 45 MB `fact_records_scope_generation_idx` and a
4240 MB heap. `fact_id` is unique and wide, so btree deduplication cannot
compress it; dropping `observed_at` from the key saves 3-4% and would force an
ordering-contract change, so the key stays as it is.

`CREATE INDEX` rather than `CREATE INDEX CONCURRENTLY`: a failed CONCURRENTLY
build leaves an INVALID index that `IF NOT EXISTS` then skips on every later
run, which is a permanent silent regression wearing this fix's name. The
blocking build is bounded and visible, and its failure mode leaves nothing
behind. `docs/public/deployment/service-runtimes-bootstrap.md` carries the
operator note, because the pause grows with table size.

## No-Observability-Change:

No metric, span, structured log, status field, or dashboard changes. The two
statements keep the same names, the same call sites, and the same error
wrapping (`list facts by kind`, `list facts by kind and payload value`), so
existing logs and traces read identically. The change is a query shape and an
index; nothing new is worth an operator alert, and nothing existing stops being
emitted.

## Why this is safe

- The split is behaviour-preserving: the first-page statement is the folded
  statement with `$4` NULL, and the cursor statement is the folded statement
  with `$4` non-NULL. The exactness diffs above confirm it on real data, and
  unit tests assert both statement shapes so the guard cannot creep back.
- Ordering is unchanged: both statements keep
  `ORDER BY observed_at ASC, fact_id ASC`, so the documented stable ordering
  shared with `ListFacts` still holds and no caller sees a different sequence.
- The existing `fact_records_scope_generation_idx` is kept, so descending and
  per-kind readers are unaffected.
- The regression risk this change carries is write amplification from a fourth
  index on an insert-heavy table. That is measured separately against the
  batched `UpsertFacts` path; this document is not complete without it.
