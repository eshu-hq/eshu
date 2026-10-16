# Package Manifest Backfill Page Timing

The reducer's package-consumption sidecar backfill rebuilds one scope as a start
transaction followed by one transaction per 500-fact page (#7663). Each
transaction takes the scope's `ingestion_scopes` row with `FOR NO KEY UPDATE`
under a 5 s `lock_timeout`, so foreign-key inserts for the scope never wait
behind the rebuild. Use these series to see where a rebuild spends time and why a
scope failed. The pass-level gauges stay in
[Reducer And Storage Metrics](metrics-reducer-storage.md).

| Metric | Type | Meaning |
|---|---|---|
| `eshu_dp_package_manifest_backfill_page_duration_seconds`, `eshu_dp_package_manifest_backfill_scope_lock_wait_seconds` | histogram | One observation per scope-rebuild transaction (#7663): the start transaction and every 500-fact page. The first is begin-to-end time by closed `outcome` (`committed`, `lock_timeout`, `failed`); the second is the wait for the `ingestion_scopes` row lock (`FOR NO KEY UPDATE`). Healthy transactions take milliseconds. A tail toward the 5 s `lock_timeout` means the ingester was holding the scope row. A `lock_timeout` outcome with a low scope lock wait means the 55P03 came from the dirty-scope row or a sidecar key row instead: the `scope_failed` log line's error text names which. |
| `eshu_dp_package_manifest_backfill_scope_rebuilds_total` | counter | Scope rebuilds by `phase` (`initial`, `dirty_repair`) and `outcome`: `cleared` (dirty row unchanged and removed), `left_dirty` (an old writer or a concurrent rebuild rewrote the dirty row; a later rebuild repairs it), `skipped`, `disappeared`, `lock_timeout` (SQLSTATE 55P03; the pass fails and retries the scope), `failed`. |

## Reading them

- A healthy page or start transaction takes milliseconds. A tail toward the
  5 s `lock_timeout` means the ingester held the scope row.
- A `lock_timeout` outcome with a low scope lock wait means the 55P03 came from
  the dirty-scope row or a sidecar key row. The `scope_failed` log line's error
  text names which.
- `left_dirty` is not a failure: another writer or a concurrent rebuild rewrote
  the dirty row, and a later rebuild repairs the scope.
