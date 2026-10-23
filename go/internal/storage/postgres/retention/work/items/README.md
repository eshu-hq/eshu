# storage/postgres/retention/work/items

Bounded retention for `workflow_work_items` (#8829). Without it the queue
table grows by one terminal row per collector target per cycle, forever.

## Pipeline position

The workflow coordinator (`go/internal/coordinator`) calls
`Sweeper.PruneWorkItems` on its retention ticker in active mode, after run
reconciliation, when `ESHU_WORKFLOW_COORDINATOR_WORK_ITEM_RETENTION_AGE`
is set. Nothing downstream consumes the deletes; status reads simply see
fewer historical rows.

## Exported surface

| Name | What it does |
| --- | --- |
| `Database` | `db.Beginner` plus `db.Queryer`; satisfied by `postgres.SQLDB` and `*postgres.InstrumentedDB`. |
| `Sweeper`, `NewSweeper` | Holds the in-memory primary-key cursor between passes. Safe for concurrent use. |
| `Sweeper.PruneWorkItems` | One pass: up to `MaxBatches` batch transactions of `BatchSize` rows. Returns `workflow.WorkItemRetentionResult`, including partial results when a batch fails. |
| `Sweeper.WorkItemTableSize` | `pg_total_relation_size` and `pg_class.reltuples` for `workflow_work_items`. |

## What a batch deletes

A row is deleted only when it is `completed` or `failed_terminal`, older than
the cutoff, in a closed run (`complete` or `failed`), not its scope's active
generation, not the newest row of its target, and, for `failed_terminal`, not
the newest terminal failure of its target. The full reasoning is on
`pruneBatchQuery` in `sql.go`.

## Concurrency

- Each window is its own transaction under `SET LOCAL`-scoped `lock_timeout`
  (policy) and `statement_timeout` (30 s).
- Rows are taken `FOR UPDATE OF item SKIP LOCKED`. Lifecycle statements lock
  only `claimed` or `pending` rows, so the row sets are disjoint.
- The cascade into `workflow_claims` locks only finished claims of deleted
  items.
- A failed batch rolls back and leaves the cursor before its window, so the
  next pass retries it. A skipped (locked) row is retried on the next walk.

## Evidence

`EXPLAIN (ANALYZE, BUFFERS)` on 300,000 rows (50 scopes of 6,000 cycles): 54 ms
and 38.8k shared buffers per 1,000-row batch; a full walk deleted 156,050
rows in 301 batches; a second walk deleted 0. Without the `OFFSET 0` fences
the planner chose a hash semi-join over a full sequential scan (457 ms with a
temp spill). Details: `docs/internal/evidence/8829-workflow-work-item-retention.md`.

## Tests

- `sweeper_test.go`: hermetic batch loop, cursor, transaction, timeout,
  partial-failure and statement-shape tests.
- `sweeper_live_test.go`, `concurrency_live_test.go`: real-Postgres proofs,
  each in its own database, gated on `ESHU_POSTGRES_DSN` (role needs
  `CREATE DATABASE`).

```bash
cd go
go test ./internal/storage/postgres/retention/work/items -count=1
ESHU_POSTGRES_DSN='postgres://eshu:change-me@localhost:15432/eshu?sslmode=disable' \
  go test ./internal/storage/postgres/retention/work/items -run Live -count=1 -v
```
