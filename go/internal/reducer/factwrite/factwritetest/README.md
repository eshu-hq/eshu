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

## Exported surface

| symbol | what it is |
|---|---|
| `FakeExecer` / `ExecCall` | a `factwrite.Execer` that records every `ExecContext` call (query + positional args) instead of executing it |
| `BatchedFactRow` | one row recovered from a decoded `factwrite.BatchInsertQuery` call |
| `DecodeBatchedFactCalls` | flattens every recorded `FakeExecer` call into `BatchedFactRow`s, failing the test if any call did not use `factwrite.BatchInsertQuery` |
| `ExpectedBatchedExecCount` | `ceil(rowCount/factwrite.BatchSize)`, the number of `ExecContext` calls a batched writer must issue for `rowCount` rows |

## Dependencies

`internal/reducer/factwrite` (the query/row/batch-size constants this
package decodes against) and the standard library (`context`,
`database/sql`, `testing`, `time`). No dependency on the reducer root or any
family subpackage.

## Telemetry

None. Test-only support code with no production call path — see
No-Observability-Change below.

## Gotchas / invariants

- **`decodeBatchedFactCall`'s 16-column decode is positional, not
  field-name-bound, and it must track `factwrite.ChunkArgs`'s column order
  exactly.** `DecodeBatchedFactCalls` reads `call.Args[0]` through
  `call.Args[15]` by index (`fact_id`, `scope_id`, `generation_id`,
  `fact_kind`, `stable_fact_key`, `collector_kind`, `source_confidence`,
  `source_system`, `source_fact_key`, `source_uri`, `source_record_id`,
  `observed_at`, `ingested_at`, `is_tombstone`, `payload`, `fencing_token`,
  in that order) and assembles each `BatchedFactRow` from those slices by
  common index `i`. If `factwrite.ChunkArgs` adds, removes, or reorders a
  column without a matching change here, this package does not fail loudly:
  it silently misattributes one column's values onto the wrong
  `BatchedFactRow` field (a `[]string` decodes fine into any other
  string-typed column), so a caller's assertions can pass against
  corrupted data. Only a type mismatch (e.g. a column becoming `[]int64`
  where a `[]string` was expected) trips the `t.Fatalf` in `stringArg`
  et al.; a same-type reorder does not.
- `DecodeBatchedFactCalls` fails the test (via `testing.TB.Fatalf`) if any
  recorded call did not use `factwrite.BatchInsertQuery` — a regression to a
  per-row insert statement is caught here, not silently accepted.
- This package is test support, not production code, and has no production
  call path — import it only from a `_test.go` file.
- The reducer root's own equivalent
  (`reducer_fact_batch_insert_test_helpers_test.go`,
  `fakeWorkloadIdentityExecer`/`decodeBatchedFactCalls`) is a SEPARATE,
  untouched copy, not a forwarder to this package. Do not consolidate the two
  without first confirming every one of the root copy's ~30 callers compiles
  against the change — that migration was deliberately left out of scope for
  issue #6061's `cicdrun` split.

## Usage

```go
db := &factwritetest.FakeExecer{}
writer := SomeWriter{DB: db}
if _, err := writer.WriteSomething(ctx, someWrite); err != nil {
    t.Fatal(err)
}
rows := factwritetest.DecodeBatchedFactCalls(t, db.Execs)
```

See Gotchas / invariants above for `DecodeBatchedFactCalls`'s failure
behavior and the positional-decode invariant it depends on.

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
