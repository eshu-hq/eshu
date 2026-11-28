# JavaScript/TypeScript Project Resolution

## Purpose

`project` resolves a JavaScript or TypeScript source file's project context
from the surrounding repository layout: the nearest `tsconfig.json` and its
`baseUrl`/`paths` import-alias mappings, the nearest `package.json` and the
entry points it declares (`main`, `module`, `bin`, `scripts`, `exports`,
`types`), repo-relative path normalization, and the stat-keyed cache that
keeps those filesystem lookups off the parser's hot path (issue #4515 P2a).
It was split out of the `javascript` package's root as part of the
directory-size reduction in issue #6771 (`golangci-lint`'s 40-file `dirgate`
cap).

## Ownership boundary

This package owns project-context *resolution*: finding and parsing the
nearest config file for a given source path, and answering path-normalization
and path-containment questions against a repository root. It does NOT own
AST parsing, dead-code root-kind classification, or TypeScript public-surface
BFS walks — those stay in the parent `javascript` package and call into this
one. It is a leaf package: nothing here may import `javascript`.

## Exported surface

- `NewTSConfigImportResolver(repoRoot, path) TSConfigImportResolver` and
  `TSConfigImportResolver.ResolveSource(source) string` — resolve a TypeScript
  or JavaScript import specifier against the nearest tsconfig.json or
  jsconfig.json `baseUrl`/`paths` (tsconfig.json wins when one directory
  carries both).
- `TSConfigSourceCandidates(basePath) []string` — deterministic extension/
  index-file candidates for resolving an extensionless import.
- `PackageFileRootKinds(repoRoot, path) []string` — package-level dead-code
  root evidence (entrypoint, bin, script, export) for one source file.
- `NearestPackageRoot(repoRoot, path) (string, bool)` — closest owning
  `package.json` directory for a source path.
- `NearestPackageName(repoRoot, path) string` — the trimmed `name` of the
  nearest `package.json`, or `""` when it has none. It is package placement
  (`node_package_name`) and stays ungated: a file under a fixture manifest keeps
  its owning package name.
- `ProducerPackageName(repoRoot, path) string` — the same name, except `""`
  when the nearest manifest sits under a test, tests, fixtures, testdata, or
  examples directory (`shared.FixtureDirPattern`, whole segments, any ASCII
  letter case; #8244). It never falls through to an outer manifest. It feeds
  `package_id` (#7601). `PackageNames` returns both from one walk.
- `DeclaredDependencies(repoRoot, path) map[string]struct{}` — union of the
  dependency, dev, peer, and optional dependency names of every `package.json`
  from the file up to the repo root (#7601 consumer keys).
- `NpmAliasTargets(repoRoot, path) map[string]string` — npm-alias dependencies
  (`"alias": "npm:target@range"`) mapped to the target package the producer
  publishes; an unparseable target maps to `""` and stays unkeyed (#7613).
- `PackagePublicSourcePaths(repoRoot, path) []string` — absolute source paths
  exposed through the nearest `package.json`'s `exports`/`types` fields.
- `RelativeSlashPath(repoRoot, path) (string, bool)`, `CleanPath(path) string`,
  `PathWithin(root, path) bool` — path normalization and containment used by
  every resolver above and by the parent package's own path handling.
- `ScopeCache[V]`, `NewScopeCache[V]()`, `Stat`, `StatFor(path)` — the
  bounded, single-flight-coalesced, generation-safe cache backing the
  tsconfig.json/package.json memoization below, reused by the `javascript`
  package's own TypeScript public-surface cache instead of a second
  hand-rolled implementation.
- `SetConfigScopeComputeHooksForTest` — test-only hook installer used by
  regression tests proving a shared config is read+parsed exactly once
  across many files (issue #4515 P2a).

See `doc.go` for the full godoc contract.

## Dependencies

- `internal/parser/shared` (`AppendUniqueString`, plus node/text helpers used
  transitively by callers) — the only intra-repo dependency; no tree-sitter
  import here, since this package resolves filesystem/config context, not AST.
- Callers: `internal/parser/javascript` (the TypeScript/JavaScript adapter).

## Telemetry

None. Resolution runs inline on the parser's hot path; a caller's own span
covers the file parse this resolution is part of.

## Gotchas / invariants

- **The scope cache is keyed by resolved config path, not repo root.** A
  monorepo with nested packages has several distinct `tsconfig.json`/
  `package.json` files, each owning a different subtree; keying by repo root
  would collapse them and leak one package's config into a sibling's files.
  See `scope_cache.go`'s package-level doc comment for the full concurrency
  and eviction argument.
- **Cache entries are invalidated by (mtime, size)**, not by content hash — a
  repo re-scanned after a config file changes on disk recomputes rather than
  serving a stale generation.
- **Single-flight coalescing keys on the full (path, stat) tuple**, never on
  path alone — an earlier design that keyed on path alone let a second
  goroutine's newer generation overwrite a first goroutine's still-in-flight
  slot; see the "generation-overwrite hazard" note in `scope_cache.go`.
- **`CleanPath`/`PathWithin` are what stop a baseUrl or paths mapping from
  resolving outside the repository root.** Never bypass them when adding a
  new resolution path.
- **Leaf-package constraint**: do not add an import of `internal/parser/javascript`
  here, even transitively — that would create an import cycle with every
  caller.

## Related docs

- `docs/internal/design/reducer-target-tree.md` ("Restack rule (the dirgate
  ledger trap)") — why package moves like this one exist.
- Sibling package `internal/parser/javascript/jsdataflow` — another leaf
  package extracted from the same root for the same file-count reason.
