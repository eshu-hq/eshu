# AGENTS.md — go/internal/collector/git/content

## Read first

1. `go/internal/collector/git/content/README.md` — purpose, exported surface, and the notes below
2. `go/internal/collector/git/content/doc.go` — the package contract
3. `go/internal/collector/repo/git/model/README.md` — the shared types and the fact-stream writer
4. `go/internal/collector/repo/git/README.md` — how the snapshot and fact stream drive this package

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

## Content-store policy (#8812)

`ApplyContentStore` is the only place a policy decision turns into a stored
body. Rules that keep it safe:

- Never return the source body on any failure path. A redactor error, a panic,
  non-UTF-8 input, and an unknown policy all return an empty body.
- Keep the marker `REDACTED_<finding_kind>`. `[redacted]`, `***`, and
  `REDACTED_value` are rejected (the detector cannot see them; `REDACTED_value`
  is reserved for #8833's skeleton mode).
- The redactor binds to `go/internal/secret/detect`; do not copy its pattern.
  `TestRedactMarkerPerKindResultUnderMigration138` pins the measured result per
  finding kind and must be updated, not loosened, when #8833's marker arm lands.
- Keep `redactMarker` fail-closed for YAML value shapes. A new key shape needs a
  seeded-violation test in `marker_redactor_test.go`.

## Verification

```bash
cd go && go test ./internal/collector/git/content/... ./internal/collector/repo/git/... -count=1
cd go && go vet ./internal/collector/...
```
