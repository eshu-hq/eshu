# AGENTS.md — go/internal/collector/repo/git/runtime/declaration

## Read first

1. `README.md` — purpose and exported surface.
2. `go/internal/collector/repo/runtime/declaration/README.md` — the parser and
   the contract rules.
3. `go/internal/collector/repo/git/model/README.md` — the shared types and the
   fact-stream writer.
4. `go/internal/collector/repo/git/README.md` — how the snapshot and fact
   stream drive this package.

## The rule that shapes this package

This package is a leaf. `git` imports it; it must never import `git`. If you
need something from `git`, move the shared piece down into `model`, or move the
snapshot logic into `git` next to the type it reads.

## Facts are a contract

Every emitter here writes through `model.FactStreamWriter`. Changing what is
emitted changes projected truth, so it needs the cassettes and the B-12
snapshot updated in the same change. Load `eshu-golden-corpus-rigor` and
`eshu-contract-rigor` before touching emission.

Emit in sorted path order so the stream is deterministic across runs.

## Verification

```bash
cd go && go test ./internal/collector/repo/git/... -count=1
cd go && go vet ./...
```
