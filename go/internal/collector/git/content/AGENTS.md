# AGENTS.md — go/internal/collector/git/content

## Read first

1. `go/internal/collector/git/content/README.md` — purpose, exported surface, and the notes below
2. `go/internal/collector/git/content/doc.go` — the package contract
3. `go/internal/collector/gitrepo/gitmodel/README.md` — the shared types and the fact-stream writer
4. `go/internal/collector/gitrepo/README.md` — how the snapshot and fact stream drive this package

## The rule that shapes this package

This package is a leaf. `gitrepo` imports it; it must never import `gitrepo`.
If you find yourself needing something from `gitrepo`, the choice is:

- the thing is genuinely shared -> move it down into `gitmodel`, or
- the thing is snapshot logic that landed in the wrong file -> move it into
  `gitrepo` next to the type it reads.

Adding an import of `gitrepo` here does not compile, and working around that by
duplicating a type is worse than either fix above. That is why
`RepositoryFactEnvelope` takes the precomputed default branch and ref payload
instead of `[]GitRef`.

## Facts are a contract

Every envelope here writes through `gitmodel.FactEnvelope`. Changing what is
emitted — new fact kind, changed payload shape, different counts — changes
projected truth, so it needs the cassettes and the B-12 snapshot updated in the
same change. Load `eshu-golden-corpus-rigor` and `eshu-contract-rigor` before
touching emission.

## Verification

```bash
cd go && go test ./internal/collector/git/content/... ./internal/collector/gitrepo/... -count=1
cd go && go vet ./internal/collector/...
```
