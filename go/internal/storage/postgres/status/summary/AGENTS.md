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
  limit. A live test pins the read to this table and a handful of buffers, not
  the plan node: the planner seq-scans a one-page table.
- `computed_at` is the database clock; `Upsert` ignores `Row.ComputedAt`.
- Never edit `migrations/161_status_summary_snapshots.sql` after it merges: the
  migration ledger pins its checksum. Change storage parameters with a new
  migration, and update the checksum manifest and embed invariant tests in the
  same change as any new migration.
- Never import the parent `postgres` package from here; the status store
  imports this package. Live tests live in the external `summary_test` package
  so they may import the parent for its adapters.
- `WriterLockKey` must stay distinct from every other advisory key constant;
  `lock_key_test.go` scans the module for collisions. Add a new advisory key
  elsewhere only with a name that contains `lock` or `advisory`, so the scan
  sees it.
- Do not add a `.go` file directly in `go/internal/storage/postgres/status/`: it
  has none today, so dirgate does not treat it as a package, and a file there
  would make every legacy root `status_*.go` file trip the naming gate.
- The reader side owns `Observe` (`observe.go`). Keep the `reason` values a
  closed set that matches `eshu_dp_status_summary_read_total` in the telemetry
  reference and both the `ActiveWorkSource` and `TerraformStateSource` OpenAPI
  enums; a new reason needs all of them.
  The writer loop's telemetry stays in `reducer/status/summary`.
- Age is the reader's database clock minus `as_of`, never a caller clock. Keep
  the clock read in `Select` on the same transaction as the row read.
- Keep the fence order in `Select` (version, row count, stale, decode). `Read`
  decodes before the caller can judge the version columns, so `Select` uses
  `readRaw`; do not switch it back to `Read`.
- A fallback is whole. `Select` returns no entries on a fallback, so a caller
  cannot mix a stored row with the live statement. A database error is an error,
  not a fallback.
- `ageKeys` must name exactly the durations the production decoder reads;
  `TestAgeKeysCoverTheDecoderDurations` and the parent package's
  `TestAgeAdvanceReachesEveryDecodedDuration` pin both sides.
- The scrape path (`ReadScrape`, `scrape.go`) has no live step by design: a
  scrape from every process must not become a herd of live statements when the
  writer stops. Never add a live hook or a `Flight` to it. It keeps one row per
  `ModelReader` unaged (`last.go`) and derives the age at each read from the
  database clock and the row's `as_of`; storing the aged entries would serve a
  stale row as fresh. `Select` sets `Now` and `Stored` for it, and, only under `DecodeStale`, decodes a stale row that passed the other fences so a restarted process serves the newest decodable row; a status route must never set `DecodeStale`. Its source and
  reason values are closed sets matching `eshu_dp_status_summary_scrape_total`
  in the telemetry reference.
