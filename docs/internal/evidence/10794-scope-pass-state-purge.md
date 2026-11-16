# #10794 scope pass-state purge: no-regression evidence

Repository retirement now deletes `supply_chain_impact_scope_pass_state` rows
(migration 284, keyed by `scope_id`, no foreign key) inside the purge
transaction. This note is the tracked record the perf-evidence gate requires.

## No-Regression Evidence:

The purge transaction gains one statement, issued once per repository after
the `ingestion_scopes` delete and before the tombstone completes:

```sql
DELETE FROM supply_chain_impact_scope_pass_state WHERE scope_id = ANY($1)
```

- Conflict domain: the pass-state row of each purged scope. The impact writer
  takes the `scope_generations` row `KEY SHARE` through its fact insert and
  upserts the pass-state row last; the purge's scope delete takes the
  generation row lock through the cascade, then this delete takes the
  pass-state row. Both sides acquire the generation row first and the
  pass-state row second, so no lock cycle can form. The statement sits after
  the cascade on purpose; placed before it, a writer holding `KEY SHARE` and
  waiting on the pass-state row would deadlock with the cascade.
- Plan shape on PostgreSQL 18.6 (`EXPLAIN (ANALYZE, BUFFERS)` inside a rolled
  back transaction, throwaway database, table built from migration 284's DDL
  with 100,000 rows, two scope ids): `Index Scan using
  supply_chain_impact_scope_pass_state_pkey`, `Index Cond: (scope_id = ANY
  (...))`, 2 rows, 9 shared buffer hits, execution 0.106 ms. One primary-key
  probe per purged scope; no secondary index is needed or added.
- End-to-end purge wall time, measured with `TestRetirementPurgeScaleMeasurementLive`
  (3,280 generations, 328,000 facts, one repository) against a disposable
  PostgreSQL 18.6 on a shared, loaded laptop. Nine interleaved pairs, main at
  5dc344a8e1 versus this branch, purge wall time in ms:
  main 551, 585, 640, 540, 531, 574, 2528, 529, 510 (median 551);
  branch 497, 1819, 1639, 513, 544, 496, 516, 497, 538 (median 516).
  Both trees show occasional 1.6 s to 2.5 s outliers from machine load, so the
  evidence is the equal medians and the single-statement plan above, not a
  speedup claim. The scale fixture seeds no pass-state rows, so the new
  delete matches zero rows there.
- Worker counts, leases, batching, retries, and the `NOWAIT` scope lock are
  unchanged. The purge stays one transaction per repository.
- Row counts: `TestRetirementPurgeLive` seeds one stuck partial row per scope
  for two scopes; after the purge the table holds 0 rows for them and the
  tombstone ledger records `supply_chain_impact_scope_pass_state: 2`.

Known residual: a straggler impact pass that commits after the purge with no
finding rows takes no `KEY SHARE` and can re-insert one pass-state row for a
gone scope. The same class exists for `repository_reindex_requests`. It is
bounded to one row per scope.

## No-Observability-Change:

No metric, span, or log field is added. The deleted count joins the existing
tombstone `rows_deleted` ledger under the key
`supply_chain_impact_scope_pass_state`, the same way every other purged table
reports, so an operator reads it from the tombstone row. A purge error
surfaces through the existing purge failure path with the message
`retirement purge delete impact pass state`.
