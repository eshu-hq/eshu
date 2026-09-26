# #6809: retention content prunes without planner statistics

Generation retention deletes `content_entities`, `content_files`, and
`content_file_references` rows whose only live facts sit in the generations
being pruned. The three prune statements used a `candidate NOT EXISTS retained`
shape over `fact_records`. This note records the plan cliff that shape has when
the planner has no statistics, the rewrite that removes it, and how the rewrite
was proven equivalent.

Two sets of timings appear here. The first pass was laptop-local (Postgres 18.6
in Docker on a developer machine, host load average 50-85). The reference set is
a remote measurement on an AWS r7a.4xlarge (AMD EPYC 9R14, 16 vCPU, 128 GiB RAM)
running `postgres:18-alpine` (PostgreSQL 18.6) with container defaults
(`shared_buffers` 128MB, `work_mem` 4MB, `hash_mem_multiplier` 2), one client, a
near-idle host. Where they disagree, the remote figures win; the laptop
"warm 5x is within noise" reading was wrong and is corrected below.

## The cliff

On a freshly bulk-loaded database, before autovacuum has analyzed
`fact_records` and the content tables, the planner estimated a handful of
candidate keys and chose a Nested Loop Anti Join for
`pruneContentEntitiesForGenerationsQuery`. Its inner side is a Bitmap Heap Scan
of the retained `fact_records` facts with no Materialize, so it is rescanned once
per candidate key.

Cold entities prune, 10 superseded generations of 5,000 keys, autovacuum off:

- Estimated 1 to 3 candidate rows; actual 5,000 outer rows against an inner scan
  of about 4,060 rows each, 20.3M rows removed by the join filter, 30.7M buffer
  hits, 48.3s.
- The same statement after `ANALYZE fact_records, content_entities`: hash join
  with a Materialize over the retained scan, 98-203ms at that size (429ms in an
  earlier 50,000-row probe).

At 5x (25,000 keys per generation) the cold statement did not finish inside a 60s
timeout. The files and references prunes chose a hash anti join cold only because
of a cost coincidence: forcing a nested loop on them (hash and merge join
disabled, an artificial demonstration) took 6.4s and 11.3s against 232ms and 74ms.
A nested loop on that predicate is therefore reachable for all three statements.

## The rewrite

One pass over the live facts of the kind, grouped by key, keeping the keys that
a pruned generation holds and no other generation holds:

```sql
WITH doomed AS (
    SELECT payload->>'repo_id' AS repo_id, payload->>'entity_id' AS entity_id
    FROM fact_records
    WHERE fact_kind = 'content_entity' AND is_tombstone = FALSE
      AND payload->>'repo_id' <> '' AND payload->>'entity_id' <> ''
    GROUP BY 1, 2
    HAVING bool_or(generation_id = ANY($1::text[]))
       AND NOT bool_or(generation_id <> ALL($1::text[]))
)
DELETE FROM content_entities AS t
USING doomed AS d
WHERE t.repo_id = d.repo_id AND t.entity_id = d.entity_id
```

The files and references statements are the same with `fact_kind = 'file'` and
`relative_path`. There is no join between two scans of `fact_records`, so no
plan can rescan the retained facts per candidate key. The aggregate spills to
disk instead of growing memory, so an estimate error costs time, not memory. It
adds no index and does not depend on `ANALYZE`.

| candidate | expected saving | old | new | accuracy | concurrency | disposition |
| --- | --- | --- | --- | --- | --- | --- |
| grouped single pass | removes the cold cliff | 48.3 s cold (1x) | 71 ms cold (1x) | identical deleted sets | not a claim or lease path | proven |
| targeted `ANALYZE` in retention | fixes estimates only | - | - | - | takes `SHARE UPDATE EXCLUSIVE` in the retention path | rejected: 1.1-1.4 s for four tables, misses stale-but-present statistics |
| partial expression indexes on payload keys | avoids the rescan | 557 ms warm 5x | 1.4-1.7 s baseline vs 3.1-5.7 s with index | - | one more index on a table with about 100 | rejected: 2.5x slower warm, needs a migration |
| materialized candidates variant | avoids the rescan | - | 570-645 ms cold 5x | identical | - | rejected: join method stays a planner choice, slower cold |
| `EXCEPT` variant | avoids the rescan | - | 307-414 ms cold 5x | identical | - | rejected: hash set has no spill path, one 2.7 s outlier |

## Measurements

Performance Evidence: the grouped rewrite removes the statistics-dependent plan
cliff, and under the retention transaction's `work_mem` (below) it is also faster
than the old statements warm. First pass, laptop-local, Postgres 18.6, real
bootstrapped schema, `EXPLAIN (ANALYZE, BUFFERS)` of each statement rolled back,
60s `statement_timeout`. Cold means freshly migrated and seeded with autovacuum off
and no `ANALYZE`; warm means the same database after `ANALYZE`. 1x is 10
superseded generations of 5,000 keys; 5x is 25,000 keys per generation.

| statement | shape | stats | old shape | grouped rewrite |
|---|---|---|---|---|
| content_entities | 1x | cold | 48,265 ms | 71 ms |
| content_entities | 1x | warm | 98 / 203 ms | 81 / 84 ms |
| content_entities | 5x | cold | timed out at 60 s | 225 ms |
| content_entities | 5x | warm | 557 / 554 ms | 621 / 612 ms |
| content_files | 1x | cold | 232 ms | 47 ms |
| content_files | 1x | warm | 54 / 76 ms | 41 / 38 ms |
| content_files | 5x | cold | 818 ms | 211 ms |
| content_files | 5x | warm | 444 / 509 ms | 595 / 669 ms |
| content_file_references | 1x | cold | 74 ms | 38 ms |
| content_file_references | 1x | warm | 59 / 52 ms | 52 / 32 ms |
| content_file_references | 5x | cold | 663 ms | 196 ms |
| content_file_references | 5x | warm | 569 / 521 ms | 522 / 576 ms |

At 5x the laptop warm rewrite was 10-35% slower than the old shape (content_files:
595 / 669 ms against 444 / 509 ms). The remote run, below, shows the cause: a
sort that spills at the default 4MB `work_mem`. One stale-statistics shape (5x,
`ANALYZE` at 138k rows, then about 500k rows added) showed no cliff for either
shape (old 1,112 / 782 / 791 ms, grouped 624 / 686 / 642 ms); that is one shape,
and it is not a claim that stale statistics are safe in general.

Classification: cold-path correctness-neutral handler win, and with the
transaction-local `work_mem` a warm-path win at 5x as well (remote, below). It
changes no end-to-end time claim. Next long pole: the row-count statement, which
runs first in every batch (see Limits), and the 20x shape, which was not measured.

Regression test, Go on the migrated schema (laptop-local): 10 pruned generations
of 6,000 keys, no `ANALYZE`, `SET LOCAL statement_timeout = '10s'`. The old
entities statement was cancelled at the timeout (SQLSTATE 57014) and, on the
same run, the old references statement took 9.0s; the rewrite
finished the references prune (7,200 rows) in 74.6ms and the entities and files
prunes (2,400 rows each) in 66.6ms and 66.2ms.

## Remote measurement

Performance Evidence: remote, AWS r7a.4xlarge, PostgreSQL 18.6 (`postgres:18-alpine`,
amd64), the branch built at the rewrite commit, 142 of 142 migrations applied by the
product bootstrap, the same seed shapes as above. Two independent fresh seeds per
scale; cold means no statistics (autovacuum off, hint bits primed, checkpoint taken)
and warm means after `ANALYZE`, six timed executions per warm cell. Each statement
ran as `EXPLAIN (ANALYZE, BUFFERS)` inside a transaction rolled back, with a literal
array in place of `$1`. No foreign benchmark ran during the 1x and 5x rounds (a
per-run process guard aborted a round otherwise); whole-machine CPU stayed at 13-27%.

Cold, both variants at the default `work_mem`, milliseconds (two seeds):

| statement | 1x old | 1x rewrite | 5x old | 5x rewrite |
|---|---|---|---|---|
| content_entities | 42,439 / 43,990 | 58 / 63 | cancelled at 120 s twice | 302 / 307 |
| content_files | 116 / 102 | 54 / 55 | 755 / 792 | 281 / 279 |
| content_file_references | 112 / 110 | 57 / 56 | 766 / 785 | 273 / 298 |

The old entities statement plans a Nested Loop Anti Join estimated at one row (the
candidate CTE estimated at 92 rows against 25,000 actual) whose inner Bitmap Heap Scan
of `fact_records` runs once per candidate key; at 1x it did 30.5M buffer hits. The
rewrite plans a Nested Loop over one HashAggregate at every setting tested and
never spills at 5x.

Warm at 5x, the default 4MB `work_mem` against a larger one, median with range in
milliseconds. "Old" is the previous statements; "rewrite" is the grouped pass:

| variant | content_entities | content_files | content_file_references | aggregate |
|---|---|---|---|---|
| old, 4MB | 351 [333-364] | 327 [308-390] | 432 [383-467] | HashAggregate |
| old, 16MB | 348 [342-406] | 312 [301-320] | 386 [360-427] | HashAggregate |
| rewrite, 4MB | 626 [615-667] | 624 [615-661] | 642 [622-666] | GroupAggregate, sort spills |
| rewrite, 16MB | 257 [249-299] | 239 [232-252] | 260 [254-297] | HashAggregate |
| rewrite, 32MB | 260 [250-306] | 242 [233-253] | 257 [245-281] | HashAggregate |
| rewrite, 64MB | 265 [249-302] | 241 [238-279] | 256 [251-295] | HashAggregate |
| rewrite, 128MB | 269 [250-303] | 239 [230-260] | 250 [245-294] | HashAggregate |
| rewrite, 256MB | 254 [252-263] | 240 [232-251] | 254 [252-257] | HashAggregate |

At 4MB the rewrite is 1.5-1.9x slower warm at 5x, and the cause is measured: after
`ANALYZE` it plans a Sort feeding a GroupAggregate over all 291,750 live facts of the
kind, and the sort spills to an 11MB external merge. From 16MB up the plan is a
HashAggregate in one batch (about 6MB) and the rewrite is 23-40% faster than the old
statements at either memory setting. Memory above 16MB buys nothing at 5x, and cold
times are the same at every setting (241-302 ms), so raising it never worsens the cold
plan. At 1x the rewrite is 13-19% faster warm at every setting, with no spill.

A dedup-first shape (candidate keys deduplicated before the retained probe) was measured
and rejected: it stays hash-based at 4MB, but at 5x it is 9% slower than the old warm
statement (382 against 351 ms for entities) and 2.5x slower cold than the rewrite
(699 / 689 against 265 / 267 ms), because it scans `fact_records` twice.

Bound `$1`: the same statements run as a server-side prepared statement on the compose
planner profile (`random_page_cost` 1.1, `work_mem` 16MB, seed A at 5x), seven
executions plus one forced generic plan. The rewrite stayed stable across all seven
(cold 267-298 ms, warm 301-390 ms) and the forced generic plan cost about 20% more,
not a cliff. This exercises the server-side plan cache; a Go driver call was not
timed separately.

Equivalence on the remote matched the laptop: the old statements, the rewrite and the
arithmetic oracle deleted identical row sets at 1x cold and warm (entities 1,750, files
1,750, references 5,250, same md5s) and at 5x (8,750 / 8,750 / 26,250).

## The transaction-local work_mem

Nothing in the reducer sets `work_mem`, and a Helm deployment uses whatever the
operator's Postgres uses (commonly 4MB). Compose already sets 16MB. So the retention
transaction now runs `SET LOCAL work_mem = '64MB'` as its first statement, before the
candidate selection, the row count, and every prune. `SET LOCAL` ends with the
transaction, so the pooled session and the server setting are untouched. 64MB is four
times the smallest value proven at 5x (16MB); the 20x cold aggregate already needs about
18-20MB of hash memory, so 16MB is not a safe bound there.

`work_mem` is a per-plan-node allowance, not a per-statement budget: each sort or hash
node may use up to 64MB, a hash node up to twice that (`hash_mem_multiplier`), and one
statement can hold several such nodes. The three content prunes are proven hash-based at
16MB and above at 5x (remote measurement above). The row-count statement is proven only on
the laptop cold shape (see Row-count attribution): at 64MB its two grouping sorts stay in
memory (4.4MB each) and at 4MB they spill (2.5MB each to disk). The earlier shape spilled
at 4MB warm at 5x on the remote (about 4.9 s); the new shape's remote warm plan is
unmeasured. Concurrent retention transactions were not measured, so no total-memory
figure is claimed.

`TestGenerationRetentionSetsTransactionLocalWorkMemFirst` fails if that statement is
not the first the transaction issues, and the live cold test reads `work_mem` from inside
the transaction (64MB after the setting and again before commit) and checks the pooled
session value is unchanged afterward.

## Equivalence

For each statement the set of rows actually deleted (count and md5 of the sorted
keys) was captured from the database and compared with an oracle computed
arithmetically from the seed layout, not from any statement variant. The old
statement and the rewrite deleted identical sets at 1x (entities 1,750, files
1,750, references 5,250) and at 5x (8,750 / 8,750 / 26,250). Covered: a key kept
by a live retained fact, a key whose retained fact is a tombstone (deleted), a key
kept by another scope's retained generation, a key whose retained fact names a
different repository (deleted), empty-key candidate facts (ignored), and rows with
no candidate fact (untouched). Removing `is_tombstone = FALSE` from the rewrite
deleted 1,500 rows instead of 1,750 and was flagged.

`TestGenerationRetentionContentPrunesDeleteExactRowsLive` keeps that check in the
tree at small scale: it writes out the expected surviving rows per case and
compares the exact remaining sets of all three tables. It passes on both the old
and the new statements, which is what pins the semantics.

## Row-count attribution

Before this change the row count charged a content row to every candidate
generation whose facts named it. On the cold test shape each of the 10 generations
reported 2,400 doomed entities, a batch total of 24,000 against the 2,400 rows the
batch deletes, so `BatchRowLimit` could trip up to 10 times early and the events'
`row_counts` could not be summed. The count now attributes each doomed content row
(content_entities, content_files, content_file_references and the
infra_resource_entities mirror) to exactly one candidate: the newest one naming its
key, the last position in `$1`. The store sorts candidates oldest superseded first,
generation id breaking ties, in Go, because the locking candidate `SELECT` has no
`ORDER BY` of its own. The `doomed_*` CTEs group every live fact of a kind once,
hash-joined to the candidate list built `WITH ORDINALITY`, with a `HAVING` equivalent
to the prunes' predicate, so per table the counts sum to the prunes' deletes. After a
row-limit skip the store recounts over the selected generations, since a skipped
generation stays on disk and protects keys the first count charged to a selected one.

Tests (laptop-local, migrated schema): the cold-batch phase now requires gen-cold-10
to hold every doomed content row, gen-cold-1..9 to hold none, and each table's sum to
equal `RowsPruned`; on the previous statement it failed with sums of 24,000 / 24,000 /
72,000 against 2,400 / 2,400 / 7,200 pruned.
`TestGenerationRetentionRowCountsAttributeSharedRowsOnceLive` seeds a key shared by
gen-1 and gen-3, a key only in gen-2 and a key protected by the active generation, and
checks the per-generation counts in store order and reversed order plus the sums
against the prunes and the infra orphan delete run in the same transaction; the
previous statement charged the shared key to both. The fake-DB tests
`TestGenerationRetentionStoreRecountsAfterRowLimitSkip` and
`TestGenerationRetentionStoreCountsCandidatesOldestFirst` pin the recount and the order.

Performance Evidence: laptop-local (Apple Silicon, PostgreSQL 18.6 container
`postgres:18-alpine`, shared host, load average 3.5-7.7), the cold test shape
(10 generations of 6,000 keys, no `ANALYZE`; `pg_stats` held no fact_records rows
before or after the runs). `EXPLAIN` of the new statement shows no `SubPlan` and no
`Nested Loop` over fact_records: each `doomed_*` CTE is a Bitmap Heap Scan of
fact_records, a Hash Left Join to the candidate list, a Sort and a GroupAggregate; the
content tables are then probed by index per doomed key, as before. `EXPLAIN ANALYZE`
execution time, 7 alternating-order runs per statement, median (range):

| `work_mem` | previous statement | attribution rewrite |
| --- | --- | --- |
| 64MB | 335.6 ms (332.0-344.3) | 150.4 ms (149.0-151.8) |
| 4MB | 342.7 ms (335.6-346.5) | 156.3 ms (154.5-158.9) |

The same run of the live cold test counted in 151.0 ms and finished the whole batch
in 949.6 ms inside its 20 s deadline. This is one cold shape on a laptop; the remote
5x warm and 20x cold measurements are pending (see Limits).

## Limits

- Synthetic data, one main scope, no production-scale run. The rewrite reads every
  live fact of the kind once; that cost is unmeasured at 10M+ facts, and no clean
  20x shape (10 generations of 100,000 keys) was measured: a peer benchmark shared
  the host, so those timings are discarded. Only the 20x cold plan shapes are
  kept: the aggregate needs 18-20MB and spills in 5 batches at 4MB without failing. After `ANALYZE` the delete join to the content table can be a hash join
  with a sequential scan of the content table, also unmeasured at that scale.
- The row-count statement's remote timing is unmeasured for the attribution rewrite.
  Its earlier shape spilled its sorts at 4MB (about 4.9 s warm at 5x on the remote,
  roughly 7x one warm prune). It runs first in every batch, so once the prunes are
  stable it is the larger cost at these scales. Pending: the 5x warm count at 4MB and
  64MB and the 20x cold plan on the remote, appended here when run.
- No concurrency proof. These are not claim or lease paths, and each statement is
  one snapshot as before, but a retention delete racing an ingester re-upsert of the
  same entity was not exercised.
- The cold row-count phase of
  `TestGenerationRetentionContentPrunesFinishWithoutPlannerStatisticsLive` never
  reproduced a cliff for either row-count shape, so it guards against a regression
  rather than showing a RED for the plan. One cold shape is not a proof for other shapes.
- The infra_resource_entities delete removes every orphan in the touched
  repositories, so an orphan that existed before the batch makes that table's
  deletes exceed its count. That behavior is unchanged.

No-Observability-Change: the three statements return the same rows-affected
counts into the same `RowsPruned` entries as before, so the
`eshu_dp_generation_retention_rows_pruned_total` counter and the retention duration
reporting are unchanged, and no metric, span, label, log key, queue, or runtime
setting is added. The retention event `row_counts` JSON keeps its keys; its content
values now follow the single attribution above.
