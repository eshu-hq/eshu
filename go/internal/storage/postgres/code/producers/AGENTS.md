# AGENTS.md — Postgres code-call producers guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../../AGENTS.md` for Postgres storage conventions.
3. `../../facts_active_code_call_symbols.go`, the loader that calls this package.
4. `docs/internal/evidence/7623-go-module-anchored-loader.md` for the proof that
   the Go rule matches the corpus-wide scan and for its named limits.

## Invariants

- `GoModuleName` must read a go.mod exactly as `goModulePath` in
  `internal/parser/go_package_module_import_path.go` does, including its
  line-ending normalization. `TestGoModuleNameMatchesTheImportPathTheParserStamps`
  drives the parser's public pre-scan with the same inputs; if the parser's rule changes,
  change this function in the same pull request or the anchor silently drops
  producers.
- `GoModuleCandidates` returns a superset on purpose. Do not narrow it to the
  longest module.
- A key shape this package does not recognize must fall into "other" and keep
  the corpus-wide scan, never be dropped. A `scip-go gomod` key needs a symbol
  after the import path to count as a Go key.
- The package, Go, and other keys run as separate scans in the loader. Do not
  merge them into one scan over the union of producer scopes: a manifest that
  matches one kind would widen another.
- Keep the two manifest statements MATERIALIZED and unfiltered by path for
  go.mod (discovery already prunes `vendor/`).
- Never import the parent `postgres` package from non-test code.

## Common changes

- A new indexer whose keys name their producer in a manifest gets its own group
  in `Split`, a key parser, a statement here, a leg in the loader, and a
  differential test against the corpus-wide statement on real Postgres.

## Failure modes

- Changing the parser's key or go.mod rule without changing this package makes
  the anchor miss definitions. The key is usually then unresolved; if the missed
  repository was one of several declaring the same module, the key can resolve
  to the remaining one.
- Importing the parent package from here couples the leaf back to root.

## Verification

From `go/`, run:

```bash
go test ./internal/storage/postgres/code/producers/... -count=1
go test ./internal/storage/postgres -run 'ActiveCodeCallSymbol|LoadActiveCodeCall' -count=1
```

Run `scripts/verify-package-docs.sh` from the repository root.
