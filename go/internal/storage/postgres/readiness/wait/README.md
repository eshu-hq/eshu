# readiness/wait

Postgres implementation of `crossscope.ReadinessWaitLedger` for the #6785
cross-scope edge handlers (`iam_can_perform_materialization` and
`workload_cloud_relationship_materialization`).

## Why it exists

Both handlers commit the edges they can resolve first, then wait, bounded, for
missing endpoints (an unregistered or uncommitted target scope, or a
WorkloadInstance that has not materialized). The bound used to be measured
from the reducer queue row's own `created_at`. A newer scope generation
supersedes a deferred row, and its new row started a new bound, so a scope
whose generation cadence is shorter than the bound never committed. The ledger
row is keyed by `(scope_id, domain)`, so it outlives each queue row.

## What a row holds

| Column | Meaning |
| --- | --- |
| `first_deferred_at` | The bound's anchor. Kept across generations; reset only when a settled or cleared wait sees a new missing set. |
| `missing_keys`, `missing_count` | The sorted missing set, capped at `crossscope.ReadinessWaitMaxKeys` (500), and its full size. |
| `missing_fingerprint` | SHA-256 over the full sorted set. |
| `committed_generation_id`, `committed_cycle_started_at`, `committed_fingerprint` | The last partial commit. A poll that matches all three writes nothing. |
| `settled_at` | Set when the bound expired for this missing set. |
| `anchor_epoch` | Fence against lease-expired stragglers. An anchor reset, a settle, or a clear moves the row to the next epoch. |
| `cleared_at` | Set when the missing set emptied. The row stays as a tombstone with no missing set so its epoch keeps fencing. |

The handler decides what to do with `crossscope.DecideWait`; this package only
reads and writes rows.

## Statements

- `GetReadinessWait`: one primary-key `SELECT`.
- `UpsertReadinessWait`: one `INSERT ... ON CONFLICT DO UPDATE ... WHERE
  EXCLUDED.anchor_epoch >= wait.anchor_epoch`. A write from a lower epoch is
  dropped. A higher epoch replaces `first_deferred_at`. An equal epoch keeps
  `LEAST(stored, new)`, so two racing first-defer writers keep the earlier
  anchor and a replayed write changes nothing.
- `ClearReadinessWait`: one primary-key `UPDATE ... WHERE anchor_epoch = $read`
  that empties the row and moves it to the next epoch. An absent row, or one a
  newer writer already advanced, is left alone.

A write the fence drops changes no row and logs
`readiness wait write dropped by the anchor epoch fence` at info, with
`scope_id`, `domain`, `readiness_wait_operation` (`upsert` or `clear`), and the
writer's `anchor_epoch`. It means a lease-expired straggler lost to a newer
evaluation; it is not an error.

## Why stragglers are fenced

The `(scope, domain)` claim fence allows one live worker per key, but a worker
whose lease expired can still finish its statements after the next worker has
claimed. Without the epoch, a straggler that read the row before a settled wait
reset its anchor would restore the older anchor through `LEAST`, and a new
missing set would settle at once instead of after the bound (review P3-1). A
straggler that read the row before a clear would re-insert its stale wait. A
straggler that read the row before it settled would write `settled_at` back to
NULL, and the next evaluation would settle the same missing set again and count
`abandoned` twice (review P3-a). The epoch drops all three writes. Graph truth never depended on this: ready edges commit
before any ledger write.

## Row growth

Rows are never deleted. A clear keeps a tombstone, because deleting the row
would drop its epoch and let a straggler re-insert a stale wait. Growth is
bounded by the key: at most one row per `(scope_id, domain)`, and only two
domains write this table, so at most two rows per scope that ever waited. A
row holds at most 500 missing keys (`crossscope.ReadinessWaitMaxKeys`); a
tombstone holds none. Scope rows elsewhere in Postgres (`ingestion_scopes`,
`scope_generations`) grow the same way, so the ledger adds no new growth
class. A retention sweep would need to prove that no straggler can still
write, which a lease timeout alone does not guarantee, so there is none.

No statement touches `fact_work_items` or holds a lock past its own statement.
The live proofs are `store_live_test.go` and `store_fence_live_test.go` (set
`ESHU_POSTGRES_DSN`).
