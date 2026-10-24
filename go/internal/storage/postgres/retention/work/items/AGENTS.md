# AGENTS.md — storage/postgres/retention/work/items

## Read first

1. `README.md` and `doc.go` in this directory.
2. `sql.go`: `pruneBatchQuery` and its doc comment, which states every
   eligibility rule and the lock reasoning.
3. `../../AGENTS.md` for Postgres storage conventions.
4. `../../workflow_control_sql.go` and `../../workflow_control_release_sql.go`
   for the lifecycle statements this package must never contend with.

## Invariants

- Delete only `completed` and `failed_terminal` rows. Never widen the status
  filter: `claimed`, `pending`, `failed_retryable`, and `expired` rows are
  live queue state.
- Keep the closed-run check. Run reconciliation derives an open run's status
  from its item counts; deleting from an open run changes that status.
- Keep the newest row, the newest `completed` row and the newest
  `failed_terminal` row per `(collector_kind, collector_instance_id,
  scope_id)`, and the active generation's row. Latest-row reads, the status
  registry `last_completed_at`, failure reporting and collector readiness
  read them. One same-status newer-row probe enforces the first three.
- Keep `FOR UPDATE OF item SKIP LOCKED` and the row predicates on the locked
  relation (EvalPlanQual rechecks them).
- Keep the `OFFSET 0` fence. Without it the planner turns the newer-row
  probe into a hash semi-join over a full sequential scan.
- One window per transaction, under the local lock and statement timeouts.
  Never fold several windows into one transaction.
- Never import the parent `postgres` package from non-test code; the
  coordinator wiring passes a `Database` in.

## Verification

A change to `pruneBatchQuery` is hot-path SQL: re-run `EXPLAIN (ANALYZE,
BUFFERS)` on a seeded table (see the evidence note in the README) and the
live proofs. From `go/`:

```bash
go test ./internal/storage/postgres/retention/work/items -count=1
ESHU_POSTGRES_DSN=... go test ./internal/storage/postgres/retention/work/items -run Live -count=1 -v
```

Run `scripts/verify-package-docs.sh` from the repository root.
