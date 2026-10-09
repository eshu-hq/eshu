# AGENTS.md — go/internal/storage/postgres/membership

## Read first

1. `go/internal/storage/postgres/membership/README.md` — invariants and evidence
2. `go/internal/storage/postgres/membership/doc.go` — the package contract
3. `go/internal/collector/repo/git/membership/README.md` — the evaluation this store serves
4. `go/internal/storage/postgres/migrations/166_repository_selection_observations.sql` — the shipped DDL

## Rules

- `schemaSQL` must stay byte-identical to the DDL after migration 166's
  leading comment block (`TestObservationSchemaEqualsEmbeddedMigration`
  compares them for equality). Never edit a shipped
  migration; add a new guarded one and update the checksum manifest.
- The upsert's counter math must match `project` in the collector
  `membership` package. Change both together; the live test compares them.
- Keep the upsert one statement, ordered by `scope_id`, and advance-only.
  Do not split it per row or add a retry loop that hides a conflict.
- Do not read `ingestion_scopes` with `FOR UPDATE` or write it from here: the
  projector locks those rows first.
- The only deletion is `DeleteExpiredObservations` (#7774): rows strictly past
  their own liveness window plus the grace, in bounded `SKIP LOCKED` batches.
  Never delete a `not_listed` row (the mass-miss guard reads them), a live
  row, or an in-grace row, and never delete or hide a scope. Keep
  `TestObservationStoreSweepLive` green: it fails a `not_listed`, window,
  boundary, grace, or lock-wait mutant.
- Keep the sweep batch unordered. An `ORDER BY` forces a full primary-key scan
  on every steady-state probe; the README has the measurement.
- This package must not import the parent `postgres` package (tests may).
- Use `eshu-postgres-rigor` for any SQL or index change, with an
  `EXPLAIN (ANALYZE, BUFFERS)` before and after.

## Verification

```bash
cd go && go test ./internal/storage/postgres/membership -count=1
cd go && go test ./internal/storage/postgres/migrations -count=1
```

Run the live test against a disposable PostgreSQL 18 (see the README); it is
enrolled in the live-postgres-readiness runner.
