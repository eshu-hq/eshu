# AGENTS.md - internal/parser/javascript/project guidance

## Read first

1. `README.md` - package boundary, exported surface, and invariants
2. `doc.go` - godoc contract: what this package resolves and why it is a leaf
3. `scope_cache.go` - `ScopeCache[V]`, the bounded single-flight LRU cache
   both memoizers sit on
4. `tsconfig.go` - `TSConfigImportResolver`, JSONC parsing, `baseUrl`/`paths`
   resolution
5. `package_json.go` - `PackageFileRootKinds`, `NearestPackageRoot`,
   `PackagePublicSourcePaths`
6. `paths.go` - `RelativeSlashPath`, `CleanPath`, `PathWithin`
7. `scope_cache_test.go` - single-flight, eviction, and cross-generation
   proofs (run with `-race`)

## Invariants this package enforces

- This package is a leaf: it MUST NOT import `internal/parser/javascript`
  (or anything that imports it), even transitively.
- The scope cache is keyed by the resolved CONFIG FILE PATH, not the
  repository root, so nested monorepo packages never collapse into one
  shared config value.
- Cache entries are invalidated by `(mtime, size)`; a stat mismatch is
  treated as a miss.
- Single-flight coalescing keys on the full `(path, stat)` tuple. Never
  reintroduce path-only keying — see the "generation-overwrite hazard" note
  in `scope_cache.go` for the defect that guards against.
- `ScopeCache` is process-global and bounded (`scopeCacheCapacity`); a new
  memoizer built on it must accept that eviction can happen at any time and
  must be safe to recompute.
- Every `baseUrl`/`paths`/export resolution MUST stay bounded by `PathWithin`
  against the repository root — never resolve or return a path outside it.

## Common changes and how to scope them

- Add a new config file kind to memoize: add a `ScopeCache[V]` instance next
  to `tsConfigCache`/`packageManifestCache` in `scope_cache.go`, following the
  `StatFor` + `Get(path, stat, compute)` pattern already there. Add a
  regression test proving one compute per shared config (mirror
  `TestNearestTSConfigOptionsComputedOnceForSharedConfig`).
- Add a new `tsconfig.json`/`package.json` field to resolve: extend the
  relevant struct in `tsconfig.go`/`package_json.go` and add a fixture test
  using `writeFile`/`writePackageJSONTestFile`.
- Change path-resolution or containment logic: edit `paths.go`'s
  `CleanPath`/`PathWithin` only — every other resolver in this package
  depends on their exact semantics, so a behavior change here is repo-wide.

## Failure modes and how to debug

- A resolved import escapes the repository root: check that the new
  resolution path calls `PathWithin` before returning a candidate, mirroring
  `resolveBaseRelativeSource`.
- Two files sharing one config recompute more than once: confirm the caller
  used `StatFor` + `ScopeCache.Get` rather than reading the file directly,
  and that the (path, stat) key is stable across calls for the same file.
- A `-race` failure in `scope_cache_test.go`: the single-flight key almost
  certainly collapsed two generations onto one slot; re-read the
  "generation-overwrite hazard" doc comment in `scope_cache.go` before
  changing `Get`.

## Do not change without review

- The `(path, stat)` single-flight key shape in `ScopeCache.Get` — keying by
  path alone reintroduces the overwrite-while-in-flight defect the type was
  built to fix.
- The leaf-package constraint (no import of `internal/parser/javascript`).
- `CleanPath`/`PathWithin`'s absolute-path-and-containment semantics; several
  callers rely on them to keep resolution inside the repository root.
