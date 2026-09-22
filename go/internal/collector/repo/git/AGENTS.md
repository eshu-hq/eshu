# AGENTS.md — go/internal/collector/repo/git

## Read first

1. `go/internal/collector/repo/git/README.md` — layout, the import direction, and
   why the core is one package
2. `go/internal/collector/repo/git/doc.go` — the package contract
3. `go/internal/collector/README.md` — the collector seam this package plugs into
4. `go/internal/collector/repo/git/model/README.md` — the shared types

## Import direction is the invariant

`git -> leaf -> model`, never the reverse. A leaf that needs something
from `git` is telling you one of two things:

- the thing is shared, and belongs in `model`; or
- the thing is snapshot/selection logic that ended up in the wrong file, and
  belongs here.

Both have happened already. `commitSHAByRelativePath` was living in the
observability emitter and reads `RepositorySnapshot`, so it moved here.
`documentationFileMetasForPaths` was living in a snapshot file and is pure
documentation logic, so it moved into `docs`. Prefer moving the misfiled
function over exporting a new symbol.

## Do not split the core on prefix alone

`git_snapshot_*`, `git_selection_*` and `git_source*` look like three families
and are one. The three-way production import cycle between them is measured, not
assumed. Before proposing a split, prove the cycle is gone — a filename prefix
is not evidence.

## Facts are a contract

Emission changes projected truth. A new fact kind, a changed payload shape, or a
different fact count needs the cassettes and the B-12 snapshot updated in the
same change, and the golden-corpus gate re-run. Load `eshu-golden-corpus-rigor`
and `eshu-contract-rigor` first. A restructure that changes projected truth is a
bug, not a new baseline.

The generation estimate is assembled from per-family pre-count functions. If you
change what an emitter sends, change its pre-count in the same edit.

## Directory size

This directory is grandfathered over the 40-file cap. The ledger row is a
ratchet in both directions: moving files out without re-pinning fails the gate.
Re-pin in the same commit as the move.

## Verification

```bash
cd go && go test ./internal/collector/... -count=1
cd go && go build ./... && go vet ./...
bash scripts/verify-dirgate.sh --all
```

## SCIP consumer: parser/scip package split (#6772)

`snapshot_scip.go` and `snapshot_scip_groups.go` consume the SCIP index
parser and the external `scip-*` indexer runner. Those moved out of
`internal/parser` into `internal/parser/scip`, so the symbols are now
`scip.IndexParser`, `scip.ParseResult`, `scip.Indexer`,
`scip.LanguageFileGroup`, and `scip.DetectProjectLanguageGroups`. The
unexported `scipResultParser` / `scipProjectIndexer` interfaces in this
package are satisfied structurally, with no compile-time assertion binding
them, so a signature change in `internal/parser/scip` surfaces as a build
failure here rather than in that package's own tests. Build both after
touching `Parse`, `Run`, or `IsAvailable`.

Local identifiers here (`recordingSCIPIndexer`, `concurrentSCIPIndexer`,
`delayedSCIPIndexer`, `fakeSCIPParser`, `rootSCIPParser`,
`scipLanguageSubtrees`) belong to package `git`, not to `scip`, and keep
their names.

No-Regression Evidence: #6772 changed these two files by import and type
repoint only — no logic, control flow, signatures, or error wrapping.
Baseline origin/main 788edd169, same tree and toolchain. Normalizing all seven
moved files through the rename map leaves four residual hunks, every one
semantically null. Two are in production: locals renamed to avoid shadowing the
functions that took their names -- `occurrenceLine` -> `defLine` in parser.go,
and inside `filesByLanguage` the local became `byLanguage` (the `grouped` locals
in that function's two callers are pre-existing and untouched). The other two
are in moved tests: `parser_test.go` inlines two unexported parent-package
helpers that could not move, and `regex_hoist_bench_test.go` de-stutters a
benchmark helper whose body is byte-identical. So there is no measurable path
to regress.
`go test ./internal/collector/repo/git/... -count=1` passes (ok 4.684s,
unchanged set), and `go test -list` shows the SCIP test inventory moved
intact at 10 of 10 with the parent package retaining 127 tests and none
matching SCIP. No benchmark is quoted because no hot-path statement
changed; a timing comparison here would measure host noise, not the diff.

No-Observability-Change: no metric instrument, metric label, span, status
field, log key, queue table, worker, lease, or runtime knob is added,
removed, or renamed. The SCIP snapshot path keeps emitting
`eshu_dp_scip_snapshot_attempts_total` and `eshu_dp_scip_process_wait_seconds`
through the same `recordSCIPSnapshotAttempt` call sites with the same
`telemetry.FailureClassAttr("scip_"+reason)` values, so existing dashboards
and the telemetry-coverage row for this path stay valid.
