# #7926 statusCode cleanup: no-regression evidence

Scope: branch `fix/7926-statuscode-cleanup`. `ProviderFailure`
(`go/internal/collector/sdk/failure.go`) loses the write-never
`statusCode` field and its branch in `FailureStatusCode`. The gate
flagged `failure.go` as a hot file by path; this note records why the
change cannot regress runtime cost.

No-Regression Evidence (dead-field deletion): the old code ran one
integer range comparison before the `HTTPError` cause-unwrap; the new
code runs only the unwrap. Both are O(1) with no allocation, I/O,
lock, or retry change, and the status resolution runs once per
failure construction, never per row, scope, or generation, so there
is no corpus scaling term. Baseline: `go test
./internal/collector/sdk/ -count=1` wall 0.206s at HEAD `e5337e4c7`
(GOTOOLCHAIN=go1.26.9, darwin/arm64 dev harness, not a production
backend). After measurement equals the baseline shape: the same suite
with the dead branch removed, all tests passing unmodified. Input
shape: provider failures with and without a wrapped `HTTPError`
across the 100-599 boundary. Queue/row counts: not applicable, the
struct is built in memory before any enqueue and touches no rows.
Change is safe because the removed branch was unreachable (no setter
and no constructor sets the field, proven by repo-wide grep), so
every reachable input takes the identical unwrap path before and
after.

No-Observability-Change: the failure path emits no new spans,
metrics, or logs. `FailureStatusCode` returns the same value for
every constructible input. No operator dashboard or alert changes.
