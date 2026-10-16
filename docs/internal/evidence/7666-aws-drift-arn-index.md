# #7666: aws_cloud_runtime_drift ARN-keyed state join

## Problem

`listActiveStateResourcesForAWSARNsQuery`
(`go/internal/storage/postgres/aws_cloud_runtime_drift_evidence_sql.go`)
loads the active Terraform-state rows whose `attributes.arn` matches the ARNs
of one sealed AWS generation. In production one `aws_cloud_runtime_drift` run
took 14-20 minutes on this statement and held the Postgres writer at 12
cores.

## Root cause

Root-Cause Evidence: the ARN equality
`fact.payload->'attributes'->>'arn' = aws_arn.arn` had no index and no
statistics. The planner estimated the `aws_generation_arns` CTE at 1 row
(actual 379), put it on the outer side of a nested loop, and re-ran the
active-state join (`ingestion_scopes` x `fact_records_scope_generation_idx`,
342 scopes) once per ARN. On the 1/20-scale shim (below), with the index
present and no statistics:

| Plan | Rows | Execution | Shared buffers | Hottest node |
| --- | ---: | ---: | ---: | --- |
| shipped statement, custom plan | 150 | 36,010.990 ms | 11,164,992 | `fact_records_scope_generation_idx` Index Scan, 129,618 loops |
| shipped statement, generic plan | 150 | 35,820.629 ms | 11,164,992 | same, 129,618 loops |

A single session reproduces it with zero lock waits (117 relation locks per
execution), so lock-manager contention is not the cause.

## Shim

PostgreSQL 18.6 (aarch64), production schema from the bootstrap migrations,
`max_parallel_workers_per_gather = 0`. 300 state scopes x 3 generations (third
active) with 500 to 1,999 `terraform_state_resource` rows per generation,
every fourth row carrying an ARN (1,105,650 state rows, 276,075 with an ARN);
90,000 `terraform_state_output` and 480,000 `parsed_entity` bystanders; one AWS
scope with 459 `aws_resource` rows (379 distinct requested ARNs, 150 matching
an active state row). 1,676,109 rows, 938 MB heap. Each figure is the second
of two back-to-back `EXPLAIN (ANALYZE, BUFFERS)` runs. Custom and generic plans
were forced with `plan_cache_mode`, because pgx's statement cache can switch
to the generic plan after five executions.

## Forms measured

- A: the shipped statement, unchanged.
- B0: the state join in a plain CTE (no fence).
- B1: the state join in a `MATERIALIZED` CTE, `ingestion_scopes` joined after.
- B2: B1 that carries only keys, then refetches the payload by primary key.
- B3 (shipped): B1 whose state side is an `OFFSET 0` LATERAL probe per ARN.

Each line below lists rows, execution time, and shared buffers.

Partial index plus `CREATE STATISTICS` on the expression, `ANALYZE`d:

- A: 150 rows, 3.714 ms, 2,668 buffers; generic 3.468 ms, 2,668.
- B1: 3.680 ms, 2,668; B2: 4.230 ms, 3,268.
- B3: 3.731 ms, 2,668; generic 3.743 ms, 2,668.

Partial index, no statistics object:

- A: 36,010.990 ms, 11,164,992 (the per-ARN loop).
- B1: 3.385 ms, 1,625; B2: 3.927 ms, 2,225.
- B3: 3.753 ms, 1,625; generic 3.388 ms, 1,625.

Stale statistics: the table was `ANALYZE`d with no `terraform_state_resource`
rows, then 1,105,650 were inserted with autovacuum off. This is the state a
large state ingest leaves until autovacuum re-analyzes; at the default scale
factor a 36.9M-row table waits for about 3.7M changed rows.

- A with the stale expression statistics object: 8,758.664 ms, 921,168. It
  walked the whole ARN index once and probed `ingestion_scopes` 276,075 times.
- B1 with the stale object: 142.956 ms, 278,044 (the same full index walk).
- B2 with the stale object: 148.996 ms, 278,644.
- Without the object, the table statistics still stale:
  - A: 8,764.736 ms, 921,169.
  - B0: 50,393.006 ms, 104,970,058.
  - B1: 131.418 ms, 278,044.
  - B3: 3.743 ms, 2,668; generic 3.809 ms, 2,668.

No ARN index, no statistics, as on main:

- B1: 462.187 ms, 1,092 hit + 119,027 read (hash join over a sequential scan).
- B3: cancelled by the 120 s `statement_timeout`. Each ARN probe scans
  `terraform_state_resource` rows, so B3 depends on migration 180.

Scale: 20,000 extra `aws_resource` rows (14,211 distinct requested ARNs),
index, no statistics:

- B3: 648 rows, 86.706 ms, 54,893 buffers (about 3.9 buffers per ARN).
- B1: 86.133 ms, 54,649.

## Decision

Performance Evidence: ship B3 plus the partial expression index (migration
180). B3 is the only form whose plan stays a per-ARN index probe with fresh,
absent, or stale table statistics, under custom and generic plans. B3 was not
measured with a stale expression statistics object, because none ships. Its cost is
1,625 to 2,668 buffers and about 4 ms at 379 ARNs. It grows with the ARN
count times the retained generations per matched ARN, because each probe reads
every retained generation of that ARN from the heap before the active
generation join drops the superseded ones. An independent review measured 300
scopes with 25 generations each at 379 ARNs: 5,277 buffers and 7.0 ms, against
989,195 buffers and 3,882 ms for the old statement, with identical rows.
That is 4,185x to 6,871x fewer buffers than the 11,164,992 baseline. Both
fences are load-bearing. Without `MATERIALIZED`, B0 loops the scope join (50 s
on stale statistics). Without the LATERAL fence, B1 walks the whole index on
stale statistics (278,044 buffers).

Deployment prerequisite: the statement depends on migration 180 being built
and valid. With the index missing, or left INVALID by a failed concurrent
build (the planner ignores an invalid index), the new statement runs one
sequential scan of `fact_records` per ARN: an independent review measured
139,267 ms and 45.5M buffers on the shim, worse than the old statement's
33,805 ms. The default Helm `schema-bootstrap` Job is a pre-install and
pre-upgrade hook, and Compose waits for it with `service_completed_successfully`,
so the reducer does not start against a missing index. An operator who disables
the schema hooks must check `indisvalid` on
`fact_records_terraform_state_resource_arn_idx` before rolling the reducer.

The statistics object does not ship. (Migrations 178 and 179 belong to other PRs,
so this index took 180.) B3 does not
need it under any measured condition. With fresh statistics it moves B3 from
1,625 to 2,668 buffers. When stale, it fixes nothing that B3's fence does not
already fix. The unchanged statement A is good only with fresh expression
statistics. It would also need an `ANALYZE fact_records` in the migration, and
it degrades to 921,168 buffers between autovacuum runs after a large state
ingest. Precedents 119 and 165 use statistics objects where no plan-shape
fence was available. Here a fence was available.

Exactness: on the shim, A and B3 return the same 150 rows in the same order
(`cmp` of the ordered unaligned output, md5
`365b29d1e8bcc9187a0eb1760bc46b1c` for both). `EXCEPT ALL` gives 0 rows in
each direction.
`TestAWSCloudRuntimeDriftStateARNJoinMatchesReferenceLive` repeats this on
every live run. It compares against a reference derived from the shipped
constant, which keeps the shipped CTE prefix and the pre-#7666 join tail. The
fixture covers ambiguous ownership, a superseded generation, an older AWS
generation, a stale caller allowlist, blank and padded ARNs, an inactive
scope, and duplicate addresses whose `fact_id` order disagrees with address
order.

## Index cost

- Size: 6,096 kB for 276,075 entries on the shim (one entry per ARN-bearing
  state row in every retained generation).
- Build: `CREATE INDEX CONCURRENTLY` took 1,018.951 ms over 1,676,109 rows
  (938 MB heap) on an idle host. Plan a minute-scale build on the production
  primary and watch `pg_stat_progress_create_index`.
- Write tax: 50,000 `terraform_state_resource` rows inserted in one statement
  and rolled back, 25% carrying an ARN, three runs per arm alternating between
  two copies of the migrated shim with and without the index, `CHECKPOINT`
  before each run, per-statement `wal_records` from `pg_stat_statements`
  (PostgreSQL 18.6, shared host). Without the index: 254,616 to 255,205
  records. With it: 267,272 to 268,262 records. The difference is about 12.5k
  records, +4.9%, which is one index record per ARN-bearing row (12,500).
  Insert execution time was 1.2 to 2.5 s in both arms and overlapped. WAL
  bytes are not compared: `wal_fpi` varied from 3,309 to 13,048 per run, so
  full-page images dominate the byte count. The per-row index WAL record size
  was not isolated and is NOT_CHECKED. The numbers above are the run ranges;
  the probe that produced them, to reproduce with, is
  `docs/internal/evidence/7666-write-tax.sql`.
- Lock shape: one concurrent statement in its own file.
  `scripts/lib/migration_lock_shape.py check` exits 0 on it. A seeded
  CREATE INDEX plus UPDATE file exits 1.

## Regression proof

`TestAWSCloudRuntimeDriftStateARNJoinProbesARNIndexLive`
(`go/internal/storage/postgres/aws_cloud_runtime_drift_state_arn_plan_live_test.go`)
seeds 320 state scopes x 3 generations and 120 requested ARNs into an
isolated schema. It runs fresh and stale statistics, each with the custom and
the generic plan, and asserts four things:

- the plan probes `fact_records_terraform_state_resource_arn_idx`;
- no `fact_records` node loops more than once per requested ARN;
- the index returns at most generations x ARNs entries, so it is probed, not
  walked;
- root shared buffers stay at or below 40 x ARNs + 2,000.

| Code under test | Result |
| --- | --- |
| origin/main (old statement, no migration 180) | FAIL: fresh, `fact_records_scope_generation_idx` looped 38,640 times, 154,806 buffers, index unused; stale, index unused |
| old statement plus migration 180 | FAIL on stale statistics: the index produced 5,760 entries (walked) and 6,812 buffers |
| B1 (`MATERIALIZED` only) plus migration 180 | FAIL on stale statistics: 5,760 entries walked, 6,810 buffers |
| shipped (B3 plus migration 180) | PASS: fresh 433 buffers, stale 1,390 buffers, 180 index entries, 60 rows, custom and generic |

## Telemetry

Observability Evidence: before this change, the existing signals showed that
the drift load was slow. They did not show which statement was slow.

- `eshu_dp_reducer_run_duration_seconds` showed slow `aws_cloud_runtime_drift`
  runs.
- The `reducer.aws_runtime_drift_evidence_load` span covers both loader reads
  and the config resolution.
- `eshu_dp_postgres_query_duration_seconds{store="reducer",operation="read"}`
  records each read, but has no per-statement label.

The two reads were unlabeled `postgres.query` children of the load span. The
loader now labels them through the existing `db.WithQuerySummary` mechanism
(#6794): `db.query.summary=aws_runtime_drift_resources` and
`db.query.summary=aws_runtime_drift_active_state_by_arn`. A slow trace then
names the statement. The values are a closed set on a span attribute only, so
no metric cardinality changes.
`TestPostgresAWSCloudRuntimeDriftEvidenceLoaderLabelsDriftReads` failed before
the change (`query 0 summary = ""`) and passes after it.

## Rejected hypotheses

- Lock-manager contention: one session reproduces the 35 s plan with zero lock
  waits, 117 relation locks per execution.
- Index alone (statement unchanged): no change without fresh statistics
  (11,164,992 buffers). On stale statistics the index is walked whole (921,168
  buffers).
- Statistics alone (no index): no change, per the read-only investigation.
- A flattened LATERAL rewrite, and an `OFFSET 0` LATERAL that held the scope
  join inside the fence: both still looped over scopes without statistics,
  per the read-only investigation. B3 keeps the scope join outside the fence.
- A statistics object as the fix: it is good only while fresh, it needs a
  migration-time `ANALYZE` of a 36.9M-row table, and a stale object left the
  unchanged statement 2,358x slower than fresh (8,758.664 ms against
  3.714 ms).

## Sibling queries

The index does not serve the other `attributes->>'arn'` readers, and they are
not changed:

- `producerOwedDriftARNsQuery`
  (`ingestion_producer_activation_sql.go`) filters by `scope_id` and
  `generation_id` first, so it uses `fact_records_scope_generation_idx`.
- `producerEvidenceCoreSQL` only tests the predicate per row.
- `multi_cloud_runtime_drift_evidence_sql.go` matches on
  `COALESCE(arn, id, self_link)` behind a `CASE`, which is a different
  expression.

## Not checked

- Production `EXPLAIN` on the 36.9M-row table and production build time for
  migration 180.
- A wall-clock drift run on the production corpus after deploy. Judge it from
  `eshu_dp_reducer_run_duration_seconds` and from the labeled
  `postgres.query` spans.
