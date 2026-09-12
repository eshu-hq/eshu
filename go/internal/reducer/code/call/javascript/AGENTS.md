# javascript — agent instructions (issue #6061)

Read `code/call/shared/AGENTS.md` first, especially the `JavaScriptAliasesByPath`
and `JavaScriptAliasSet` accessors this package's `dynamic.go` depends on.

## Invariants

- Never import `code/call` or a sibling language leaf.
- `ResolveDynamicCallee` must try the index-cached alias set
  (`shared.EntityIndex.JavaScriptAliasesByPath`) before falling back to
  re-scanning the containing function's source — the fallback exists only
  for direct helper tests that bypass index construction; do not make it the
  primary path (it would silently drop the caching win `BuildEntityIndex`
  pays for once per file).
- `roots.go`'s three caller-identity helpers gate on the JS/JSX/TS/TSX
  language set independently of each other; keep that switch identical
  across all three if the supported language list ever changes.
- Prove changes with `go test ./internal/reducer/code/call/... -count=1`
  and this package's own `go test ./internal/reducer/code/call/javascript/...`
  (the `parsed_file_data`-typed byte-identity test for `FileRootCallerID`
  lives here as `roots_test.go`).
