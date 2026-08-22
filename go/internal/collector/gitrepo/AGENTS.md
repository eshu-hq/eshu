# AGENTS.md — go/internal/collector/gitrepo

## Read first

1. `go/internal/collector/gitrepo/README.md` — layout, the import direction, and
   why the core is one package
2. `go/internal/collector/gitrepo/doc.go` — the package contract
3. `go/internal/collector/README.md` — the collector seam this package plugs into
4. `go/internal/collector/gitrepo/gitmodel/README.md` — the shared types

## Import direction is the invariant

`gitrepo -> leaf -> gitmodel`, never the reverse. A leaf that needs something
from `gitrepo` is telling you one of two things:

- the thing is shared, and belongs in `gitmodel`; or
- the thing is snapshot/selection logic that ended up in the wrong file, and
  belongs here.

Both have happened already. `commitSHAByRelativePath` was living in the
observability emitter and reads `RepositorySnapshot`, so it moved here.
`documentationFileMetasForPaths` was living in a snapshot file and is pure
documentation logic, so it moved into `gitdocs`. Prefer moving the misfiled
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
