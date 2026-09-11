# python — agent instructions (issue #6061)

Read `code/call/shared/AGENTS.md` first for the `PythonClassBasesByRepo`
accessor `resolver.go` walks.

## Invariants

- Never import `code/call` or a sibling language leaf.
- `resolvePythonDirectClassMethod`/`resolvePythonInheritedClassMethod` must
  return "ambiguous" (no resolution) when more than one candidate resolves,
  not pick one arbitrarily — an inherited-method walk over an ambiguous base
  class must never invent a false edge.
- `ExtractMetaclassRows` and `ExtractMetaclassRowsWithIndex` are the renamed,
  still-exported successors of `ExtractPythonMetaclassRows`/
  `extractPythonMetaclassRowsWithIndex`; `code/call/extract.go` calls
  `ExtractMetaclassRowsWithIndex` directly to share one `BuildEntityIndex`
  pass with code-call extraction — do not reintroduce a second index build.
- Prove changes with `go test ./internal/reducer/code/call/... -count=1`
  and this package's own `go test ./internal/reducer/code/call/python/...`,
  plus the Python import-order/imports/metaclass-isolation tests and the
  Python resolution goldens in `code/call`.
