# elixir — agent instructions (issue #6061)

Read `code/call/shared/AGENTS.md` first for the `RepositoryImports`/
`EntityFileByID` lookups this package depends on.

## Invariants

- Never import `code/call` or a sibling language leaf.
- `elixirImportedModuleOwnsFile` must confirm the resolved callee file
  actually belongs to the aliased module's import paths (trying both the
  bare and repository-root-joined form) before accepting a same-name match
  — this is the guard against an unrelated same-named function elsewhere in
  the repo.
- Prove changes with `go test ./internal/reducer/code/call/... -count=1`
  and the Elixir resolver tests in `code/call`.
