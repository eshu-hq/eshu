# AGENTS.md — status summary snapshots guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../../AGENTS.md` for Postgres storage conventions.
3. `sql.go` for the three statements, `row.go` for the codec, `store.go` for
   the upsert and read, `lock.go` for the writer lock.
4. `docs/internal/naming.md`: nest plain-English directories, never repeat the
   directory name in a file name, and keep this package's tests in this
   directory.

## Invariants

- Keep the upsert guard strict (`existing.as_of < EXCLUDED.as_of`). A `<=`
  guard makes a replay rewrite the row and breaks the "equal as_of rewrites
  nothing" contract; removing the guard lets an older pass overwrite a newer
  one. `store_live_test.go` proves both by running a guard-stripped copy.
- Keep the write one statement on one row. A multi-row model exposes a torn
  answer after a partial write.
- Keep the read a primary-key lookup with no `as_of` filter, ordering, or
  limit. A live test pins the plan.
- `computed_at` is the database clock; `Upsert` ignores `Row.ComputedAt`.
- Never edit `migrations/161_status_summary_snapshots.sql` after it merges: the
  migration ledger pins its checksum. Change storage parameters with a new
  migration, and update the checksum manifest and embed invariant tests in the
  same change as any new migration.
- Never import the parent `postgres` package from here; the status store will
  import this package. Live tests live in the external `summary_test` package
  so they may import the parent for its adapters.
- `WriterLockKey` must stay distinct from every other advisory key constant;
  `lock_key_test.go` scans the module for collisions. Add a new advisory key
  elsewhere only with a name that contains `lock` or `advisory`, so the scan
  sees it.
- This package adds no telemetry: the writer and reader callers own the
  metrics, spans, and logs the ruling lists.
