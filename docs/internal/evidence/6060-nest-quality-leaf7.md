# 6060 nesting leaf 7: quality leaf (code-quality inspection)

## What moved

`go/internal/query/codequery/quality.go` split into the `quality/`
leaf (request + bounds in `request.go`, the bounded scan in
`inspect.go`, shaping in `results.go`, with the doc trio and request
contract tests) and the thin `codequery/inspection.go` handler with
the capability alias named by contract tests. `symbolSourceHandle`
moved with its only caller.

## Pin handling

- One `query-source-coverage.yaml` pin repathed `quality.go` →
  `quality/inspect.go` with a recomputed `source_sha256` from the
  repo go/parser extraction (`Inspect`, count 1, `label_inventory`
  class, label Function, max_results 101 unchanged). Verified by
  `go test ./internal/queryplan/`.
- No `grandfathered_non_hot.go` entries name this family.

## No-Regression Evidence

- `go test ./internal/query/codequery/quality/ -count=1` — ok.
- `go test ./internal/query/... -count=1` — 35 packages ok, no FAIL.
- `go vet ./internal/query/codequery/...` — clean; scoped
  `precommit-go.sh lint` — 0 issues.
- `verify-dirgate.sh --all`, `verify-package-docs.sh` — green
  (see commit verification).

## No-Observability-Change

No telemetry, span, metric, or log line changed; the leaf adds no
instrumentation of its own.
