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
generation through `ListFactsByKind`. The load is the comparable metric across
all rows: `ListFactsByKind` is byte-identical at baseline on both branches
involved, and the same scope, generation and page size are used throughout.

| configuration | load |
| --- | --- |
| baseline: no index, folded statement | 84.68 / 83.78 / 84.12 s |
| new index only, statement still folded | 232.94 / 232.25 / 235.32 s |
| new index plus the statement split (shim) | 3.53 / 3.23 / 3.34 s |
| new index plus the statement split (this commit) | 3.99 / 3.30 / 3.37 s |

Baseline 84.12 s, after 3.37 s: a 25x reduction on the worst-case generation,
reproduced independently by the throwaway shim and by the committed code.

Handler totals are reported separately rather than folded into that table,
because they are not like-for-like across branches. On the branch carrying the
rationale follow-up, `RationaleEdgeMaterializationHandler.Handle` over this
generation took 84.34 s at baseline and was entirely load-bound. On this branch
the follow-up does not exist, so the same handler returns "no repositories
available for rationale materialization" in 3.49 s. Comparing 84.34 s to 3.49 s
would be comparing two different amounts of work; comparing the loads is not.

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

Measured on the corpus store, with the migration in its shipped
`CREATE INDEX CONCURRENTLY` form:

- first apply: index built valid in 10.84 s
- reapply: clean no-op in 0.067 s (`already exists, skipping`)
- writes are not blocked by the build: five real inserts issued during an
  11.07 s build completed in 0.098, 0.089, 0.083, 0.078 and 0.077 s. Each was
  rolled back, and the corpus store ended with zero probe rows.
- a cancelled build leaves an invalid index, and it is reclaimable by name: after
  cancelling a build mid-flight, `pg_indexes` showed the index present with
  `indisvalid = false`; dropping it by name — which is exactly what
  `SQLDB.dropInvalidConcurrentIndexes` does before executing each definition —
  returned 0 present / 0 invalid, and the next build produced 1 present /
  0 invalid.
- no `ANALYZE` needed: after a drop and rebuild with no `ANALYZE`, the cursor
  query took the seek under both a custom plan (15.964 ms) and a forced generic
  plan (1.078 ms). This is a plain-column btree; the expression indexes in this
  repo do need a post-build `ANALYZE`, this one does not.

An earlier draft of this change used a blocking `CREATE INDEX` and justified it
by claiming a failed CONCURRENTLY build leaves an invalid index that
`IF NOT EXISTS` would skip forever. That claim was wrong, and the measurements
above are the correction: this repo already drops invalid concurrent indexes by
name before every schema definition (`db.go`, `schema_bootstrap_lock.go`), and
migrations 069 and 075 already build CONCURRENTLY on this same table. The
blocking form would have imposed an avoidable multi-second write stall on
`fact_records` at every upgrade.

Index size 844 MB against a 45 MB `fact_records_scope_generation_idx` and a
4240 MB heap. `fact_id` is unique and wide, so btree deduplication cannot
compress it; dropping `observed_at` from the key saves 3-4% and would force an
ordering-contract change, so the key stays as it is.

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
- The regression risk this change carries is write amplification from another
  index on an insert-heavy table. It is measured below, and the read saving
  exceeds it by a wide margin.

## No-Regression Evidence:

Write amplification on the batched `UpsertFacts` path, measured against a
scratch database on the same host as the corpus store, so the hardware matches
the read numbers. 60,000 `content_entity` envelopes per arm, all sharing one
`observed_at` as production does, seven rounds, arms alternating each round so
cache warmth does not settle on one side. Foreign-key parents are seeded
outside the measured window.

| round | without index | with index |
| --- | --- | --- |
| 1 | 4.791 s | 4.830 s |
| 2 | 4.744 s | 4.821 s |
| 3 | 4.719 s | 4.835 s |
| 4 | 4.721 s | 4.825 s |
| 5 | 4.715 s | 4.824 s |
| 6 | 4.719 s | 4.829 s |
| 7 | 4.736 s | 4.849 s |

Average without 4.735 s (12,672 rows/s), with 4.831 s (12,421 rows/s):
**+2.0%** on the insert path. All seven rounds move the same direction, and the
arms are separated by 0.096 s against a within-arm spread of 0.028 s on the
with-index arm and 0.029 s on the without-index arm across rounds 2-7. Round 1
without-index (4.791 s) sits 0.05 s above the rest of its arm and looks like a
warm-up; including it the without-index spread is 0.076 s, still smaller than
the gap between arms. That separation is why this number is trustworthy where
the earlier ones were not.

Two earlier three-round runs of the same harness reported +5.0% and +12.3%.
Neither is quoted here, and neither should be. The +5.0% run contained an
impossible datapoint: both arms of round 1 identical to the millisecond across
two independent 60,000-row inserts. The +12.3% run spanned +1.7% to +16% across
its three rounds, and that +16% round is a large outlier against the 0.03 s
within-arm spread the seven-round run shows — I do not know what caused it
(checkpoint, autovacuum, and other load on the host are all candidates), and
"unexplained" is the honest description rather than "noise". Either way three
rounds could not separate the index from it, and the seven-round run replaces
both.

Worth stating plainly, since the discarded runs are the ones that made this
change look more expensive: the run kept is the one most favourable to shipping.
That is why both discarded numbers are named here rather than dropped.

Net effect over the reference corpus. Ingesting all 3,642,630 fact rows costs
287.4 s at 12,672 rows/s and 293.3 s at 12,421 rows/s, so the index adds about
5.9 s across a full corpus ingest. Against that, the measured load saving on
the single worst-case generation is 84.12 s to 3.37 s, or 80.8 s — more than
thirteen times the entire corpus-wide write cost, from one generation. Three further
per-generation domains (`semantic_entity_materialization`, `sql_relationship`,
`inheritance`) read through the same path on `main` and gain the same
improvement without paying anything additional.

The write cost is real and worth restating plainly rather than rounding away:
every fact insert on this table now maintains one more index, forever, and that
2% does not disappear as the corpus grows. It is accepted because the read side
it buys is quadratic and this is linear.
