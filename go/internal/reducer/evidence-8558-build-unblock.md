# Evidence: reducer Result straggler repoint (unblocks #8558)

The batch8-result refactor (base 01d6e117dd) deleted the `type Result =
reducercontract.Result` alias from `runtime.go`, leaving two bare `Result`
uses behind, so `go build` of the merge tree fails with `undefined: Result`
(`documentation_edge_materialization.go:78`,
`workload_cloud_relationship_materialization.go:161`). The fix repoints
those two error-path returns to the canonical `reducercontract.Result`
spelling every sibling file already uses. (The Intent/Domain stragglers
from the older base are main's #8568 now, not this branch.)

## Why no regression

No-Regression Evidence: the retired alias denoted the identical type
(`type Result = reducercontract.Result`), so the repoint is spelling-only
with no signature, envelope, row, or ordering change. Baseline
origin/main abb5cae718 fails `go build ./internal/reducer/` with
`undefined: Result` at the two lines above. After the repoint:
`go build ./internal/reducer/` exit 0, `go vet ./internal/reducer/`
exit 0, and `go test ./internal/reducer/ -count=1` ok.

No-Observability-Change: identifier spelling only; no span, metric, log,
status, queue, or lease behavior is touched.
