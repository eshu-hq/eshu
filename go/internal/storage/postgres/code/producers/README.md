# Postgres code-call producers

## Purpose

This package finds the repository scopes that can define a code-call symbol key,
from the key text and the manifests stored in `content_files`. The definition
loader in the parent `postgres` package scans only those scopes' active file
facts instead of every file fact in the corpus (#7601, #7623).

## Ownership boundary

This package owns the key classification (`Split`), the Go key and module
rules, the two manifest statements (`PackageManifestsQuery`,
`GoModuleManifestsQuery`), and the `Store` that runs them. The parent `postgres`
package owns `LoadActiveCodeCallSymbolDefinitionFacts`, the definition scan and
its keyset paging, and the load log line. The Go parser owns the key shape and
the import-path rule this package mirrors: `internal/parser/golang/scip_symbols.go`
and `internal/parser/go_package_module_import_path.go`.

## Exported surface

- `Split` sorts keys into package keys, `scip-go gomod` keys, and the rest.
- `GoImportPath` and `GoModuleCandidates` turn a Go key into the module paths
  that could declare it: the import path and each of its `/`-prefixes.
- `GoModuleName` reads a go.mod `module` line exactly as the parser does.
- `PackageName` and `PackageManifestName` do the same for `package:` keys and
  package.json manifests.
- `New(db.Queryer)` returns a `Store` with `PackageScopeIDs` and
  `GoModuleScopeIDs`. Both reads bind stored content to the writing
  generation through the `content_files` generation tag (#7760): a row whose
  tag names no activated generation returns as a NULL row, so the producer
  set never drops a candidate; scopes with no stored manifest but a
  never-activated generation resolve dirty through a manifest-less leg. See
  `docs/internal/evidence/7760-generation-tag.md` (and its predecessor,
  `docs/internal/evidence/7609-manifest-generation.md`).
- `WithInstruments` counts every row each read sees into
  `eshu_dp_producer_manifest_tag_outcomes_total` by `kind` and `outcome`.

## Why the Go rule is exact

The parser builds a definition's import path as its nearest go.mod module path
plus the package directory, and emits no key without a go.mod. The scope that
defines a key therefore stores a go.mod whose module path is the key's import
path or one of its prefixes. The candidate list is a superset on purpose: an
extra scope only adds files the exact key match then rejects. The ways the
anchor can still miss a definition (go.mod dropped from the content store, a
store out of step with the active generation) are in
`docs/internal/evidence/7623-go-module-anchored-loader.md`.

See `doc.go` for the godoc contract.
