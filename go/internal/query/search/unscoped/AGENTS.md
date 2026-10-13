# Agent instructions: unscoped

Read `doc.go` and `README.md` first.

## Invariants

- Every statement after the readiness check runs inside a savepoint and under a
  statement timeout. A server cancel (SQLSTATE 57014) is handled by
  `ROLLBACK TO` plus `RELEASE`; any other error is returned unchanged.
- The cursor advances only on a completed window. Never advance it from a
  cancelled statement, and never read a range twice or skip one.
- A spent budget is a `Partial` result, never an error and never a page that
  looks complete. Do not add a fallback that runs the old single statement.
- No file in this package may put the search pattern into a span attribute,
  metric label, or log line.
- The figures (200, 500, 0.5, 150 ms floor) come from the #7730 V4c
  measurements; the 0.55 continuation cap comes from the #7730 continuation
  shim of 2026-10-11 (`7730-continuation-shim-20261011.md`, QA environment
  read of 2026-10-11) and `TestPolicyClasses` pins its classes. Changing a
  figure needs a new measurement, not a guess.
- The engine (`walk.go`, the generic `search` in `searcher.go`,
  `telemetry.go`) never names a table. Table-specific statement text, key
  binding, row scanning, span identity, error subject and figures belong in a
  shape (`files.go` for `content_files`). A new table is a new shape file,
  not an engine edit; its figures need their own measurement.
- `TestFileRouteGolden` must stay byte-identical for any change that is not an
  intended file-route contract change. Never regenerate it to make a refactor
  pass.
- The root `query` directory is dirgate-pinned: new non-test code for this
  feature belongs here, not in `go/internal/query/*.go`.

## Verification

From `go/`:

```
go test ./internal/query/search/unscoped -count=1
go test ./internal/query/contentread -count=1
go vet ./internal/query/...
```

The live plan and differential proofs are in `go/internal/query`
(`content_reader_search_unscoped_live_test.go`) and need a disposable
PostgreSQL with `pg_trgm`.
