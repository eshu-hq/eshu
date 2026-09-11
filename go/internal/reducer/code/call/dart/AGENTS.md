# dart — agent instructions (issue #6061)

Read `code/call/shared/AGENTS.md` first; the Dart-specific bare-fallback
classifier (`codeCallDartQualifiedClassReceiver`) lives in
`code/call/shared/names.go`, not here — it is tested by
`shared/names_test.go`, not by this package.

## Invariants

- Never import `code/call` or a sibling language leaf.
- Dart's `package:` import resolution must try both the `rawPath` and
  `relativePath` caller candidates when locating the package root
  (`dartCallerPackageRoot`) — a repo checkout can carry either shape
  depending on the ingestion path.
- `BlocksRepoFallback` must return true whenever an import directive's base
  name matches the call name case-insensitively, even when the matched
  import did not resolve to a repository path — this documents "the file
  explained the failed match" and blocks the ambiguous repo-unique-name
  guess.
- Prove changes with `go test ./internal/reducer/code/call/... -count=1`
  and the Dart resolver/receiver-fallback tests in `code/call`.
