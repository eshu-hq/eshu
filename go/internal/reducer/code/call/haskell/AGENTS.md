# haskell — agent instructions (issue #6061)

Read `code/call/shared/AGENTS.md` first for the `UniqueNameByPath` lookup
this package depends on.

## Invariants

- Never import `code/call` or a sibling language leaf.
- A qualified call resolves only when it maps to exactly one module name
  (`haskellQualifiedImportTargets` requires `len(moduleNames) == 1`) — an
  ambiguous qualifier (two imports sharing an alias) must resolve to
  nothing, not the first match.
- `QualifiedImportTargetExists` (the repo-fallback-blocking hook named
  distinctly from the `BlocksRepoFallback` convention the other JVM/Dart/
  Elixir/Kotlin leaves use) reports existence, not success — it returns
  true even when the resolver itself failed to uniquely resolve, so the
  dispatch still refuses to fall back to a repo-unique-name guess.
- Prove changes with `go test ./internal/reducer/code/call/... -count=1`
  and the Haskell resolver tests in `code/call`.
