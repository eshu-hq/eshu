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
| `first_deferred_at` | The bound's anchor. Kept across generations; reset only when a settled wait sees a different missing set. |
| `missing_keys`, `missing_count` | The sorted missing set, capped at `crossscope.ReadinessWaitMaxKeys` (500), and its full size. |
| `missing_fingerprint` | SHA-256 over the full sorted set. |
| `committed_generation_id`, `committed_cycle_started_at`, `committed_fingerprint` | The last partial commit. A poll that matches all three writes nothing. |
| `settled_at` | Set when the bound expired for this missing set. |

The handler decides what to do with `crossscope.DecideWait`; this package only
reads and writes rows.

## Statements

- `GetReadinessWait`: one primary-key `SELECT`.
- `UpsertReadinessWait`: one `INSERT ... ON CONFLICT DO UPDATE`. It keeps
  `LEAST(stored, new)` for `first_deferred_at` unless the caller resets the
  anchor, so two racing writers keep the earlier anchor and a replayed write
  changes nothing.
- `ClearReadinessWait`: one primary-key `DELETE`; an absent row is fine.

No statement touches `fact_work_items` or holds a lock past its own statement.
The live proof is `store_live_test.go` (set `ESHU_POSTGRES_DSN`).
