# #10800 write-admission purge: no-regression evidence

Repository retirement now deletes `supply_chain_impact_write_admission` rows
(migration 154, keyed by `(scope_id, generation_id)`, no foreign key) inside
the purge transaction. The census already carried a `2e` row for the table,
but no purge statement existed. This note is the tracked record the
perf-evidence gate requires.

## No-Regression Evidence:

The purge transaction gains one statement, issued once per repository in the
leftover sweep, before the `ingestion_scopes` delete:

```sql
DELETE FROM supply_chain_impact_write_admission WHERE scope_id = ANY($1)
```

- Conflict domain: the admission rows of each purged scope. The position is
  the opposite of the #10794 pass-state delete, and the reason is the writer's
  own order. The impact writer takes its conflict-domain advisory lock, then
  upserts the admission row, and only then runs the fact insert whose foreign
  key takes `FOR KEY SHARE` on the `scope_generations` row
  (`writer_retract.go`: admission, then `BatchInsertVersionedFacts`, with the
  pass-state upsert last). The writer therefore holds the admission row first
  and the generation row second. The purge's scope delete takes generation row
  locks through the cascade. Deleting admission before the cascade makes the
  purge take the same order (admission row, then generation rows), so no cycle
  can form. Deleting it after the cascade, as #10794 does for pass state,
  would hold the generation row while waiting on an admission row an in-flight
  writer holds, while that writer waits on the generation row.
- Measured on a disposable PostgreSQL 18.6 with a minimal replica of the
  schema (`ingestion_scopes`, `scope_generations` with `ON DELETE CASCADE`,
  `fact_records` with a generation foreign key, migration 154's admission
  table), one writer session (admission upsert, `pg_sleep(2)`, fact insert)
  and one purge session started 0.5 s later:
  - Admission deleted after the scope delete: the writer failed with
    `ERROR: deadlock detected` on `SELECT 1 FROM ONLY "public"."scope_generations" x WHERE "generation_id" OPERATOR(pg_catalog.=) $1 FOR KEY SHARE OF x`.
  - Admission deleted before the scope delete: no error on either side; the
    purge waited for the writer's commit and then removed the scope.
- The same proof is pinned in the real schema by
  `TestRetirementPurgeAdmissionLockOrderLive`: a writer transaction holds the
  admission row, the purge starts and a `pg_blocking_pids` poll waits until
  the purge is blocked behind that writer's backend, the writer then takes the
  generation `KEY SHARE` and commits, and the purge must complete. Moving the
  delete after the cascade turns it red with `PurgeRetired: ERROR: deadlock
  detected (SQLSTATE 40P01)`, and `TestPurgeRetiredRunsDesignedStepOrder` also
  fails on the position.
- Plan shape on PostgreSQL 18.6 (`EXPLAIN (ANALYZE, BUFFERS)` inside a rolled
  back transaction, throwaway database, table built from migration 154's DDL
  with 100,000 rows spread over 30 scopes plus two target scopes of 3,280
  generations each, the 3,280-generation scale of the #8659 measurement):
  `Bitmap Index Scan using supply_chain_impact_write_admission_pkey`,
  `Index Cond: (scope_id = ANY (...))`, 6,560 rows deleted, 6,693 shared
  buffer hits for the whole delete (133 for the scan, the rest are the row
  deletions and their index maintenance), execution 2.1 ms. The primary key leads with `scope_id`, so no
  new index is needed or added. The row count is bounded by the generations of
  one repository, deleted in one statement inside the existing transaction.
- End-to-end purge wall time, measured with
  `TestRetirementPurgeScaleMeasurementLive` (3,280 generations, 328,000 facts,
  one repository) against the same disposable PostgreSQL 18.6, `large` purge
  wall in ms, interleaved pairs, base `01304ce074` (the #10794 head) versus
  this branch, five interleaved pairs on a shared laptop at load average
  about 50: base 1013, 609, 1118, 1780, 1059 (median 1059); branch 653, 678,
  1384, 2208, 1123 (median 1123). Both trees swing by a factor of three from
  machine load, so the evidence is the single-statement plan above and the
  unchanged shape of the transaction, not a speed claim; no regression can be
  read out of or into these medians. The scale fixture seeds no admission
  rows, so the new delete matches zero rows there; the plan above is the cost
  of the populated case.
- Worker counts, leases, batching, retries, and the `NOWAIT` scope lock are
  unchanged. The purge stays one transaction per repository.
- Row counts: `TestRetirementPurgeLive` seeds one admission row per scope for
  two scopes; after the purge the table holds 0 rows for them and the
  tombstone ledger records `supply_chain_impact_write_admission: 2`.
- Reader check: the only reader of the table besides the writer is the
  suppression-diff candidate query (`suppression_fanout_wire.go`), which joins
  `scope_generations` of the same scope. A purged scope has no generations, so
  deleting its admission rows cannot change another scope's baseline.

Known residual: an impact pass that inserts a first-time admission row for a
purged scope after the purge's delete statement ran and before the cascade
takes the generation lock keeps that one row (its `KEY SHARE` makes the
cascade wait, then the writer commits). The window is the few statements
between the delete and the cascade, the row is inert (global fencing-token
sequence, generation-id keys), and it is bounded to one row per in-flight
(scope, generation). The same class exists for `repository_reindex_requests`
and, for the pass-state table, in #10794.

## No-Observability-Change:

No metric, span, or log field is added. The deleted count joins the existing
tombstone `rows_deleted` ledger under the key
`supply_chain_impact_write_admission`, the same way every other purged table
reports, so an operator reads it from the tombstone row. A purge error
surfaces through the existing purge failure path with the message
`retirement purge delete supply_chain_impact_write_admission`.
