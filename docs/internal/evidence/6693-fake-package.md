# #6693 D6: shared storage/postgres fake package

Baseline: `origin/main` `41d0def95`. Change: add the non-test package
`go/internal/storage/postgres/fake`, a scripted `db.ExecQueryer` with
exported call records, FIFO query responses and injectable `Routes`, for test
packages that move out of the storage/postgres root. Root's own test-only
`fakeExecQueryer` and every existing test are unchanged.

No-Regression Evidence: nothing in production imports the new package, and no
existing file changes, so no statement, query, lock, lease, batch size, worker
count or graph write changes. `go test ./internal/storage/postgres/fake/...
-race -count=1` passes, covering FIFO order, route precedence, the
unexpected-query error, exec error and result injection, concurrent use under
the race detector, and row scanning.

No-Observability-Change: the package is test support only; no metric, span,
log key or status field is added, removed or renamed.
