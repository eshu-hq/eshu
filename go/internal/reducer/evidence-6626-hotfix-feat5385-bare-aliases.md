# Evidence: main repair — feat/5385 bare Intent/Result/Domain (#6626 follow-up)

## What changed

`origin/main` at `01d6e117dd` did not compile `go/internal/reducer`:
`#8523` (feat/5385-workload-rekey, merged 07:10 EDT) landed four files
written against the pre-retirement tree, using bare `Intent`, `Result`,
and `Domain` after buckets 6 (#8560, merged 05:22 EDT), 7, and 8 deleted
those aliases. Proven on a pristine detached worktree at `01d6e117dd`:
`go build -buildvcs=false ./...` exit 1 with exactly four `undefined`
errors and nothing else module-wide.

Fix (spelling-only, identical types — 5 Go files, 7 repoint sites plus imports):

- `cloud_relationship_resolution.go:20`: `intent Intent` →
  `intent reducercontract.Intent` (+ contract import).
- `documentation_workload_resolution.go:21`: same (+ contract import).
- `documentation_edge_materialization.go:78`: `return Result{}, err` →
  `return reducercontract.Result{}, err` (import already present).
- `workload_cloud_relationship_materialization.go:161`: same (import
  already present).
- `defaults_workload_handle_wiring_test.go:56,70,133`: `domain Domain`,
  `map[Domain]bool`, `[]Domain{...}` → `reducercontract.Domain` (+ contract
  import). The `DomainWorkload*` const elements are live forwarders kept in
  `intent.go` and stay.

A `// Result.SubSignals` prose comment is untouched. No behavior change is
possible: every edit names the identical canonical type.

## Why no regression

Proof on this head: `go build -buildvcs=false ./...` exit 0 (was exit 1
with the four errors pre-fix); `go vet -buildvcs=false ./...` exit 0;
gofmt clean over all five files; `go test -buildvcs=false -count=1
./internal/reducer/` green (covers the fixed test file and the bucket
6/7/8 retirement guards); `git diff --check` clean.

No-Regression Evidence: baseline `origin/main 01d6e117dd` fails the
module build (exit 1); after this change the identical build exits 0,
vet exits 0, and the reducer package suite is green. No backend/version,
input shape, or row counts apply: the edit renames only static type
spellings to the identical canonical type (same identity, methods,
layout, and generated call code), so no runtime, allocation, or hot-path
delta is possible; the compiler verifies every call site, and the
touched-tree tests confirm behavior is unchanged.

Local toolchain note: pinned go1.26.9 via the go1.26.6 binary,
`CC=clang`, `GOROOT`/`GOBIN` unset, go1.26.6 first on `PATH`;
`-buildvcs=false` only for the detached-scratch build (VCS stamping
noise), not needed in the worktree.

## Observability

No-Observability-Change: no new metric, span, log field, knob, worker,
lease, or queue-domain touch. Build repair only; the existing reducer
spans and counters report exactly as before.
