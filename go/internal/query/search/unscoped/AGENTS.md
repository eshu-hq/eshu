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
- The `content_entities` shape (`entities.go`) has its own figures: probe
  1,000, step 5,000, phase one 0.20, tail cap 0.80, floor 150 ms (triple A of
  the #7009 S1 shim; the figures are in
  `docs/internal/evidence/7009-unscoped-entity-search-figures.md`).
  `TestEntityPolicyClasses` pins its classes. Never copy one shape's figures
  to another: a table's per-row cost and match density set them.
- The engine (`walk.go`, the generic `search` in `searcher.go`,
  `telemetry.go`) never names a table. Table-specific statement text, key
  binding, row scanning, span identity, error subject and figures belong in a
  shape (`files.go` for `content_files`, `entities.go` for
  `content_entities`), together with the span `search.content_kind`, the
  error and partial-log texts. A new table is a new shape file, not an engine
  edit; its figures need their own measurement.
- `TestFileRouteGolden` and `TestEntityRouteGolden` must stay byte-identical
  for any change that is not an intended contract change of that route. Never
  regenerate them to make a refactor pass.
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
(`content_reader_search_unscoped_live_test.go` for files,
`content_reader_entity_search_unscoped_live_test.go` for entities) and need a
disposable PostgreSQL with `pg_trgm`.
