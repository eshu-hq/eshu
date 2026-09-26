# #6809: retention content prunes without planner statistics

Generation retention deletes `content_entities`, `content_files`, and
`content_file_references` rows whose only live facts sit in the generations
being pruned. The three prune statements used a `candidate NOT EXISTS retained`
shape over `fact_records`. This note records the plan cliff that shape has when
the planner has no statistics, the rewrite that removes it, and how the rewrite
was proven equivalent.

**Every timing here is laptop-local** (Postgres 18.6 in Docker on a developer
machine, default `work_mem`, host load average 50-85 while measuring). Remote
numbers are pending; the owner requires final performance numbers from the
remote host. Read the ratios and plan shapes as the evidence and the absolute
milliseconds as indicative.

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
cliff and matches the old warm plans. Laptop-local, Postgres 18.6, real
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

Warm 5x is within noise of the old shape or up to about 35% slower (content_files:
595 / 669 ms against 444 / 509 ms); that is the cost
of always reading every live fact of the kind once. One stale-statistics shape (5x,
`ANALYZE` at 138k rows, then about 500k rows added) showed no cliff for either
shape (old 1,112 / 782 / 791 ms, grouped 624 / 686 / 642 ms); that is one shape,
and it is not a claim that stale statistics are safe in general.

Classification: correctness-neutral handler win on the cold path; warm path is
not faster. It does not change any end-to-end time claim. Next long pole: a
production-scale warm run, which needs the remote host.

Regression test, Go on the migrated schema (laptop-local): 10 pruned generations
of 6,000 keys, no `ANALYZE`, `SET LOCAL statement_timeout = '10s'`. The old
entities statement was cancelled at the timeout (SQLSTATE 57014) and, on the
same run, the old references statement took 9.0s; the rewrite
finished the references prune (7,200 rows) in 74.6ms and the entities and files
prunes (2,400 rows each) in 66.6ms and 66.2ms.

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

## Limits

- Synthetic data, one main scope, laptop-local, no production-scale run. The
  rewrite reads every live fact of the kind once; that cost is unmeasured at 10M+
  facts. After `ANALYZE` the delete join to the content table can be a hash join
  with a sequential scan of the content table, also unmeasured at that scale.
- No concurrency proof. These are not claim or lease paths, and each statement is
  one snapshot as before, but a retention delete racing an ingester re-upsert of the
  same entity was not exercised.
- The row-count statement carries the same prunable-key predicate and was not
  rewritten. On the cold test shape (10 generations of 6,000 keys, no `ANALYZE`)
  it returned all 13 table counts in 316-340ms, and a whole
  `PruneSupersededGenerations` batch through the production store finished in
  1.16-1.22s; `TestGenerationRetentionContentPrunesFinishWithoutPlannerStatisticsLive`
  runs both under a 20s deadline. No cliff reproduced, so that phase guards
  against a regression rather than showing a RED. One cold shape on a laptop
  is not a proof for other shapes.
- The row count attributes a content row to every candidate generation whose facts
  name it, so on this shape each generation reports 2,400 doomed entities and the
  batch total is 24,000 against the 2,400 rows the batch deletes. `RowsPruned`
  reports the deleted rows, but the batch row limit and the per-generation event
  `row_counts` see the larger figure. That behavior predates this change and is
  unchanged here; the test pins the per-generation counts and that the total never
  falls below the rows deleted.

No-Observability-Change: the three statements return the same rows-affected
counts into the same `RowsPruned` entries as before, so the
`eshu_dp_generation_retention_rows_pruned_total` counter, the retention event
`row_counts` JSON, and the retention duration reporting are unchanged, and no
metric, span, label, log key, queue, or runtime setting is added.
