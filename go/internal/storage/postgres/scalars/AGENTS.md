# AGENTS.md — internal/storage/postgres/scalars guidance

## Read first

1. `README.md` in this directory -- why the helpers left the `db` leaf.
2. `scalars.go` -- `Blank`, `NullTime`, `NullTimePtr`, `TimePtrFromNull`.
3. `../db/AGENTS.md` -- the interfaces-only invariant this package protects.

## Invariants

- Keep this package stdlib-only and free of SQL text, I/O, and telemetry.
- Do not import the parent `postgres` package: the dependency runs one way,
  `postgres` and its leaves import `scalars`.
- Do not move these helpers back into `db`: that leaf holds interfaces only.
- Keep the zero/nil/UTC mappings frozen; validators across families depend
  on them.

## Verification

```bash
cd go && go test ./internal/storage/postgres/scalars -count=1
cd go && go test ./internal/storage/postgres/... -count=1
```
