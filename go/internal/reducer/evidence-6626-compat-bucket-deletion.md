# Evidence: delete retired compat buckets projection/decode/correlation/cloud (#6626)

## What changed

Four files deleted from `go/internal/reducer/`, 380 lines removed, 0 added:
`compat_projection.go` (127), `compat_decode.go` (129),
`compat_correlation.go` (72), and `compat_cloud.go` (52).

The first three hold zero live declarations (package clause plus retired-ledger
comments only; verified by declaration sweep with a base positive control). The
cloud bucket holds zero aliases: only the bidirectional `GraphQueryRunner` pin
plus retired-stanza notes. Every caller names the owning leaf package directly
(#8719 value-flow lane and the prior bucket retirements).

The pin asserted method-set identity between `reducer.GraphQueryRunner` (alias
of `reducercontract.GraphQueryRunner`) and the structural local redeclaration in
`containerimage`. Four sibling seams (`value`, `taint`, `crossplane`,
`workloadinstance`) ship the identical single-method interface with no pin,
documented as safe via structural typing. The seam stays in its owning files;
only the cross-package assertion goes. All 15
`compat_alias_retirement_*_test.go` files survive untouched.

## Why no regression

No-Regression Evidence: pure compile-time deletion of unreferenced symbols, so
every repoint is vacuous; no signature, envelope, row, or ordering change.
Baseline: `origin/main` 741a8e1e (merge #8719); after: this head. Backend: n/a
(no query or driver path touched). Input shape: n/a (no fixture or row-shape
change). Proof on this head: `go build ./...` exit 0; `go vet` on
`internal/reducer`, `internal/reducer/containerimage`, and `cmd/reducer` exit 0;
all 15 retirement tests PASS; `go test -count=1` green over `internal/reducer`,
`internal/reducer/containerimage`, and `cmd/reducer`; `git diff --check` clean.

## Observability

No-Observability-Change: the deleted files contain no metric, span,
structured-log, status, or pprof emitter (verified by sweep over all four base
blobs); nothing is added, removed, or renamed on any operator surface.

Refs #6626.
