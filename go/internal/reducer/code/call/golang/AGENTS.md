# golang — agent instructions (issue #6061)

Read `code/call/shared/AGENTS.md` first for the `EntityIndex` invariant this
package depends on.

## Invariants

- Never import `code/call` or a sibling language leaf. Only `code/call/shared`.
- `Resolvers` must keep its declared phase order
  (`resolveGoPackageQualifiedCallee`, `resolveGoMethodReturnChainCallee`,
  `resolveGoSameDirectoryCallee` before the repo fallback;
  `resolveGoCrossRepoExportCallee` after it) — `code/call/languages.go`
  dispatches resolvers in list order per phase, so reordering changes
  resolution precedence.
- Cross-repo export resolution only ever resolves to a *different*
  repository than the caller (`candidate.RepositoryID == repositoryID`
  is rejected) — same-repo package-qualified calls go through
  `resolveGoPackageQualifiedCallee` instead. Do not merge the two paths.
- Prove changes with `go test ./internal/reducer/code/call/... -count=1`
  and the Go resolution goldens (`resolution_goldens_go_test.go` in
  `code/call`).
