# #5393 Phase 1 Storage Evidence: `content_file_ref_deltas` Overlay

Phase 1 storage slice for the branch-aware coverage epic (#5393): migration
`196_content_file_ref_deltas.sql` plus `RefDeltaStore`
(`go/internal/storage/postgres/content_ref_deltas.go`). New overlay tables
only. No existing statement, index, worker, lease, queue, or read path is
modified; the store has no callers yet, so no runtime behavior changes.
No performance improvement is claimed; this note is the no-regression
marker the perf-evidence gate requires for schema-DDL changes.

## How to reproduce

```bash
cd go
go test ./internal/storage/postgres/ -run 'TestRefDelta' -count=1
go test ./internal/storage/postgres/ ./internal/storage/postgres/migrations/ -count=1
```

Measured 2026-10-17 on branch `fix/5393-ref-deltas-table` over base
`f9f16412d` (which includes merged #8357).

No-Regression Evidence: #5393 adds the `content_file_ref_deltas` and
`content_file_ref_delta_bases` schema and store only. Baseline: full
`go/internal/storage/postgres` plus `migrations` suites green before the
slice (base commit). After: the same suites green with the slice --
`go test ./internal/storage/postgres/ ./internal/storage/postgres/migrations/
-count=1` passes (22 new `TestRefDelta*` tests, plus the updated migration
golden digest, checksum manifest, and ordered-names pins). Backend: local Go
unit tests against the package's `fakeExecQueryer` (no live Postgres; live
`*_live_test.go` lanes self-skip without a DSN). Input shape: diverged rows,
hash-converged rows, tombstones, blank-key rejections, whole-overlay
retracts, and stale/fresh/missing delta bases. The slice is safe because it
adds tables and an unconsumed store: `TestDDLMigrationParity` proves the Go
DDL declares nothing the numbered migration lacks, the migration holds no
foreign key, trigger, or concurrent index (no lock on `content_files`), and
`TestRefDeltaStatementsNeverWriteCanonical` plus its seeded-violation pair
prove no overlay statement can write canonical content.

No-Observability-Change: #5393 storage slice adds no metric instrument,
metric label, span name, log key, route, worker, lease, queue domain, or
runtime knob. The new statements run through the existing `db.ExecQueryer`
surface, so when a future caller wires the store, the standard
`InstrumentedDB` Postgres query spans and
`eshu_dp_postgres_query_duration_seconds` cover the SQL path with no new
telemetry to learn.
