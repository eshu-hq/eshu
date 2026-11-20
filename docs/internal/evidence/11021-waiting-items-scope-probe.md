# #11021 waiting-items scope probe: index plan evidence

Refs #11021. The 2e purge sweep deletes parked waits for retired scopes
(`purgeDeleteWaitingItemsQuery`, `DELETE ... WHERE scope_id = ANY($1)`); the
waiting table's wake and reason indexes both lead on other columns, so without
a scope probe the delete would seq-scan every parked wait on each purge.

## Measurement

Performance Evidence: plan shape on a scratch Postgres 17 with 20,000 parked
waits across 200 scopes, after `VACUUM ANALYZE`, `EXPLAIN (COSTS OFF)` of the
shipped statement with two scope binds:

| Shape | Plan |
|---|---|
| Without the index | `Delete` → `Seq Scan`, filter on `scope_id = ANY (...)` |
| With `reducer_waiting_items_scope_idx` | `Delete` → `Bitmap Heap Scan`, `Bitmap Index Scan` on the new index |

No-Regression Evidence: the index is one narrow btree on `scope_id` (no
`INCLUDE`, no predicate), so writers pay one index insert per parked wait;
the purge delete probes instead of scanning. Migration 317 is a single
`CREATE INDEX CONCURRENTLY IF NOT EXISTS`, so bootstrap never blocks writers.

Observability Evidence: no new metrics, spans, or logs; the purge ledger
already counts per-table deletes and the new table joins that count via the
extended `TestPurgeRetiredLedgerReportsTheCensus2eTables` pin.
