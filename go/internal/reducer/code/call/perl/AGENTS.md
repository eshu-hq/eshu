# perl — agent instructions (issue #6061)

Read `code/call/shared/AGENTS.md` first for the `UniqueNameByPath` lookup
this package depends on.

## Invariants

- Never import `code/call` or a sibling language leaf.
- `resolvePerlPackageImportCallee` requires `perlFileImportsPackage` to
  confirm the package name before trying any import path — a `Package::sub`
  call with no matching `import`-like declaration must not resolve, even if
  a same-named package happens to appear in `repositoryImports`.
- Prove changes with `go test ./internal/reducer/code/call/... -count=1`
  and the Perl resolver tests in `code/call`.
