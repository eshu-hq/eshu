# Generation-Prune Generation-ID Index Evidence (#7419)

`graph_projection_phase_state` carries `generation_id` only as the fourth
column of its primary key `(scope_id, acceptance_unit_id, source_run_id,
generation_id, keyspace, phase)`, so no lookup by `generation_id` can seek.
Migration `156_graph_projection_phase_state_generation_idx.sql` adds a plain
`(generation_id)` btree as a sole `CREATE INDEX CONCURRENTLY IF NOT EXISTS`
statement. No query text changes.

## Where the cost is

Every prune deletes scope generations with
`deleteScopeGenerationsForRetentionQuery`
(`DELETE FROM scope_generations WHERE generation_id = ANY($1)`). The
`generation_id REFERENCES scope_generations ... ON DELETE CASCADE` foreign key
on this table fires once per deleted generation and runs
`DELETE FROM ONLY graph_projection_phase_state WHERE generation_id = $1`. With
nothing to seek by, each firing scans the whole table, so a prune of N
generations scans it N times, and the retention transaction holds its scope row
locks (`FOR UPDATE` on `ingestion_scopes` and `scope_generations`) across all of
them.

The retention row-count statement is not per generation. It joins the batch's
candidate set to each table (`candidate LEFT JOIN table ... GROUP BY`), which
the planner runs as one hash join over one scan per batch. This change does
not claim to speed it up.

This table is the outlier among the foreign-key children of
`scope_generations`. On ops-qa the large children (`fact_records`,
`fact_work_items`, `eshu_search_index_documents`, `code_reachability_rows`,
`admission_decisions`) already seek through their `(scope_id, generation_id,
...)` indexes (PostgreSQL 18 skip scans or bitmap scans in the generic cascade
plan); only this table and the 1.5 MB `code_reachability_repository_watermarks`
plan a full scan.

Two sibling tables were considered and not indexed:
`graph_projection_phase_repair_queue` and `fact_replay_events` (see
"Tables not indexed").

## ops-qa census (read-only, 2026-10-02 16:44-16:52Z)

PostgreSQL 18.3 (`postgres` pod, plans on the read replica, counters on the
primary), session with `default_transaction_read_only=on`; no DDL, no writes.

| Item | `graph_projection_phase_state` |
| --- | --- |
| rows | 91,558 across 10,167 generations (p50 10, p95 13, max 31 rows per generation) |
| size | 139 MB total: 29 MB heap, 110 MB indexes (`updated_idx` 72 MB, `idx_scan` 0) |
| `pg_stat_user_tables` | n_tup_ins 23,315, n_tup_upd 737,846, n_tup_hot_upd 7,289, n_tup_del 0 |
| write rate | about 326,000 rows per day over the 56 h stats window, 454,000 per day over a 357 s window; 97% conflict updates |
| generation churn | 672 to 2,300 generations superseded per day, about 1,150 per day over the last week |

`pg_stat_database.stats_reset` is NULL, but `pg_stat_wal.stats_reset` is
2026-09-30 08:43:53Z, the postmaster start. The counters cover about 56
hours; `scope_generations` shows 2,863 inserts against about 1,150 created per
day, which agrees.

`generation_retention_events` has 0 rows: no prune has run on ops-qa.
`ESHU_GENERATION_RETENTION_MAX_SUPERSEDED_AGE` is `87600h` there (default
168h), and 21,489 superseded generations are waiting. Retention itself is on by
default and the Helm chart refuses to render it off, so a config change is the
only thing between ops-qa and draining that backlog. The saving below is
therefore projected from churn, not observed.

Plans on the replica (`EXPLAIN (ANALYZE, BUFFERS)`):

- `SELECT count(*) ... WHERE generation_id = $1`: `Seq Scan`, 91,545 rows
  removed by filter, 3,687 buffer hits, 14.7 to 15.3 ms.
- The cascade shape, `DELETE FROM ONLY ... WHERE generation_id = $1`, plans as
  a `Seq Scan` (generic plan cost 4,832). The primary key cannot serve it:
  `pg_stats` `n_distinct` is 813 for `scope_id` and -0.12 and -0.11 (a
  negative value is a fraction of the row count, so about 12% distinct each)
  for `acceptance_unit_id` and `source_run_id`. Together the three leading
  columns have 73,106 distinct values in 91,829 rows (about 80%), so a
  PostgreSQL 18 skip scan has almost nothing to skip.
- The real retention count join at 1, 50 and 500 candidates is a
  `Hash Right Join` over a single `Seq Scan` of this table (31 to 45 ms in
  total with the candidate selection).

The cascade `DELETE` itself cannot run on ops-qa read-only. Its cost is
established by the plan above and the fixture below.

## Fixture at the ops-qa shape (PostgreSQL 18.6)

`postgres:18.6` in a throwaway container, real child DDL from migrations
012 with stub parents and the foreign keys, 800 scopes, 22,309 generations, and
91,503 `graph_projection_phase_state` rows over 10,167 generations (9 rows each),
`VACUUM ANALYZE` after seeding. A full scan reads only the heap, and the
fixture heap is smaller than ops-qa's 29 MB (3,687 pages), so ops-qa's
per-generation scan cost is higher than the fixture's: 14.7 ms measured there
against 5.2 ms here.

### Prune cascade

`DELETE FROM scope_generations WHERE generation_id IN (<500 generations>)` inside
`BEGIN ... ROLLBACK`, 8 runs per arm, every run in milliseconds:

- without the index: 2,602.6, 2,673.8, 2,607.4, 2,629.4, 2,608.3, 2,604.8,
  2,613.8, 2,622.9 (median 2,611.1)
- with the index: 16.5, 15.3, 15.6, 15.1, 15.2, 15.3, 16.1, 15.2 (median 15.3)

That is 5.22 ms per generation before and 0.031 ms after, about 170 times
faster on the fixture. Draining the 21,489 ops-qa backlog would cost about 112 s
of cascade scans without the index on the fixture's smaller table (about 316 s
at ops-qa's 14.7 ms scan), against well under 1 s with it; in steady state
(about 1,150 generations per day) that is 6 to 17 s of database time per day. A
500-generation batch's cascade through this table adds about 2.6 s (up to 7 s
at ops-qa's table size) to the time the retention transaction holds its scope
row locks without the index, and about 15 ms with it. The transaction holds
those locks through every other step too (candidate selection, the row-count
join, content prunes, and the cascades into the other children), so this is
this table's share of the hold, not the whole of it.

### Write cost

2,000-row batches shaped like the production writer (8-column upsert), each
inside `BEGIN ... ROLLBACK`. `VACUUM` before every run so earlier rolled-back
batches do not leave dead rows for the next one, two copies of the table (one
with the index, one without), arms interleaved, 16 runs per arm, every run in
milliseconds.

Conflict update (`ON CONFLICT ... DO UPDATE`, indexed `updated_at`, so
non-HOT):

- without: 41.7, 41.2, 40.6, 41.3, 43.3, 42.0, 40.2, 42.4, 41.4, 40.9, 40.7,
  41.8, 40.8, 42.7, 40.5, 41.0 (median 41.26)
- with: 53.3, 49.9, 49.6, 50.3, 49.4, 50.3, 49.3, 48.9, 49.2, 49.8, 48.4,
  48.3, 68.7, 49.0, 48.9, 49.1 (median 49.37; the 68.7 is one outlier)

Fresh insert:

- without: 51.1, 52.0, 51.8, 51.0, 51.7, 51.6, 51.7, 52.2, 52.7, 51.9, 51.9,
  51.8, 52.7, 51.4, 52.9, 51.9 (median 51.86)
- with: 57.3, 60.1, 58.1, 56.5, 56.0, 57.0, 57.6, 56.5, 57.3, 56.8, 56.3,
  56.2, 56.7, 56.5, 58.9, 56.0 (median 56.78)

Conflict update +8.1 ms per 2,000 rows (+19.7%, about 4.1 microseconds per
row); insert +4.9 ms (+9.5%, about 2.5 microseconds per row). Every run of the
with arm is above every run of the without arm. Each written row also adds WAL. Measured as the
`pg_current_wal_insert_lsn()` difference over a 2,000-row conflict update in
one transaction, the batch writes 540.5 bytes per row without the index and
670.9 with it (3 runs each, identical within 0.1), so about 131 extra bytes per
row (an independent reviewer's repro measured about 140). A fresh index on the
91,503 fixture rows is 1,608 kB.

Production translation at ops-qa's 326,000 to 454,000 written rows per day,
97% conflict updates: 1.3 to 1.8 s of extra write CPU per day, and 43 to 64 MB
of extra WAL per day (the primary wrote about 3,200 GB of WAL in the same 56
hours, about 1.4 TB per day, so this is about 0.004%). At the production writer's batch of 250 rows
the extra cost is about 1 ms per batch (extrapolated from the 2,000-row run, not
measured at 250).

The saving is 3 to 13 times the write cost once retention runs, and zero until
it does. An earlier PostgreSQL 16.15 fixture at 50,000 rows (+9.8% on conflict
update, ~1.9 microseconds per row, vacuum-paired) agrees on sign and order of
magnitude; PostgreSQL 18.6 is the primary evidence because production runs
18.x.

## Tables not indexed

Both fail the index doctrine (query hot enough, plan can use it).

- `graph_projection_phase_repair_queue`: 0 rows, no inserts, updates or deletes
  in the 56 h window. On PostgreSQL 18 the planner already uses an
  `Index Only Scan` on the primary key with `generation_id` as the index
  condition (0.05 ms). Nothing to save, nothing to pay.
- `fact_replay_events`: 262 rows in 25 pages, last written 2026-09-25, written
  only by the admin replay path
  (`go/internal/query/admin/store/replay.go`). The probe is a 25-page scan,
  about 2 ms cold and 0.1 ms warm. An earlier draft of this change indexed it
  from a 200,000-row fixture that has no production basis.

Revisit either table with a census showing rows and write volume that
justify an index.

## Migration build strategy

A concurrent build takes `ShareUpdateExclusiveLock`, which does not block
`INSERT`/`UPDATE`/`DELETE`, so writers keep running during the build. The file
holds exactly one statement so the migration coordinator runs it in autocommit
without the bootstrap `lock_timeout` (#7004) and drops an invalid same-name
index before a retry (`coordination.IsSoleConcurrentIndexStatement`, pinned by
`TestGenerationPruneGenerationIndexMigration`). `IF NOT EXISTS` makes a re-run a
no-op. The shipped file was applied verbatim to the fixture.

## Not measured

- The cascade `DELETE` time on ops-qa itself (read-only session; the proxy is
  the generic plan plus the fixture).
- The 250-row batch write delta (extrapolated from 2,000 rows).
- Prune timings with retention running, because no prune has run on ops-qa;
  after the age knob is reset, `generation_retention_events` timings and
  `pg_stat_user_indexes.idx_scan` on the new index confirm the saving.
- Which other deployments run the default 168h age.
- `graph_projection_phase_state_updated_idx` (72 MB, `idx_scan` 0 on ops-qa) is
  a separate question and is not part of this change.

## No-Regression Evidence

No-Regression Evidence (#7419): the change is one new index and no query text
changes, so no reader gets a different result and the cascade deletes exactly
the same rows, only reaching them by a seek. The cost is on the write side:
+4.1 microseconds per conflict-updated row and +2.5 per inserted row on the
PostgreSQL 18.6 fixture (+19.7% and +9.5% at 2,000 rows), 1.3 to 1.8 s of CPU
and 43 to 64 MB of WAL per day at ops-qa's write rate. No lease, claim, queue,
or transaction path changes.

## Observability Evidence

No-Observability-Change: no metric, span, log, or status signal is added. The
index is observable through `pg_stat_user_indexes` (`idx_scan` on
`graph_projection_phase_state_generation_idx`) and the existing
generation-retention timing and `generation_retention_events`.
