# internal/reducer/factwrite/factwritetest

## Purpose

Test support for `internal/reducer/factwrite`'s batched-insert contract
(issue #6061): a fake `factwrite.Execer` that records every `ExecContext`
call instead of executing it, and a decoder that turns those recorded calls
back into per-row records, so a reducer family's writer test can assert on
what a batched insert wrote without a live database.

It exists as a regular (non-`_test.go`) package because Go forbids importing
another package's `_test.go` files. Before this package, the fake `Execer`
and its decoder lived only in the reducer root's
`reducer_fact_batch_insert_test_helpers_test.go`, usable by the ~30 root
writer test files in the same package but unreachable from any family
subpackage below the root (`cicdrun`'s own writer test is the first such
caller). The root's copy was left in place rather than migrated, so every
existing root caller is unchanged; new callers, in the root or any family
subpackage, should prefer this package instead of writing another local
fake.

## Ownership boundary

**Owns:** `FakeExecer` / `ExecCall` (the fake `Execer` and its recorded-call
shape), `BatchedFactRow` and `DecodeBatchedFactCalls` (the decoder for
`factwrite.BatchInsertQuery` calls), and `ExpectedBatchedExecCount` (the
`ceil(rowCount/factwrite.BatchSize)` helper for asserting a batched writer
issued the expected number of round-trips).

**Does not own:** the versioned batched-insert decode
(`factwrite.BatchInsertVersionedQuery` /
`factwrite.BatchInsertVersionedFacts`) — no current caller of this package
needs it, so it was not ported from the root's equivalent
(`decodeBatchedVersionedFactCalls` in
`reducer_fact_batch_insert_test_helpers_test.go`). Add it here, mirroring
`DecodeBatchedFactCalls`'s shape, if a future family subpackage needs to
assert on a versioned batched write.

## Dependencies

`internal/reducer/factwrite` (the query/row/batch-size constants this
package decodes against) and the standard library (`context`,
`database/sql`, `testing`, `time`). No dependency on the reducer root or any
family subpackage.

## Usage

```go
db := &factwritetest.FakeExecer{}
writer := SomeWriter{DB: db}
if _, err := writer.WriteSomething(ctx, someWrite); err != nil {
    t.Fatal(err)
}
rows := factwritetest.DecodeBatchedFactCalls(t, db.Execs)
```

`DecodeBatchedFactCalls` fails the test (via `testing.TB.Fatalf`) if any
recorded call did not use `factwrite.BatchInsertQuery` — a regression to a
per-row insert statement is caught here, not silently accepted.

## Related docs

- `go/internal/reducer/factwrite/README.md` — the production batched-insert path this package's decoder mirrors
- `go/internal/reducer/cicdrun/README.md` — the first caller of this package, and the root-side test doubles its move needed for fixtures this package could not cover
- `docs/internal/design/package-restructure.md` — the #6061 restructure

No-Regression Evidence: this package is new test-support code, ported
verbatim (same field names, same positional-argument decode, same 16-column
order) from the reducer root's pre-existing
`reducer_fact_batch_insert_test_helpers_test.go`, restricted to the
non-versioned path `cicdrun` needs. The root's own copy is untouched, so
every existing root caller is unaffected. Measured from `go/`, with `GOROOT`
unset and `GOCACHE` pointed at this worktree: `go build ./...` exited 0; `go
vet ./...` exited 0; `go test ./internal/reducer/... -count=1` exited 0,
including `cicdrun`'s two callers of `FakeExecer`/`DecodeBatchedFactCalls`/
`ExpectedBatchedExecCount` (`ci_cd_run_correlation_test.go`,
`ci_cd_run_correlation_writer_batch_test.go`). `gofumpt -l` on every file in
this package reported nothing to format.

No-Observability-Change: this package adds no queue domain, worker, lease,
graph, or Postgres operation, runtime setting, metric instrument, metric
label, span, or status surface. It is test-only support code with no
production call path.
