# AGENTS.md - internal/queryplan guidance for LLM assistants

## Read first

1. `README.md` - package purpose, fixture contract, and evidence notes.
2. `doc.go` - godoc boundary and non-runtime behavior.
3. `validator.go` - manifest schema and static validation rules.
4. `source_coverage.go` - production query-call discovery and fail-closed
   inventory rules.

## Invariants

- This package is static validation only. Do not add live Neo4j, NornicDB,
  Postgres, provider, or network calls here.
- Keep source paths generic repo-relative paths. Do not put private hosts,
  local machine paths, IPs, credentials, or customer details into fixtures.
- When a read path is backed by SQL/read-model evidence rather than Cypher,
  mark it as `query_kind: sql_read_model` and include a caveat.
- New Cypher hot paths must declare source owner, anchor labels/properties,
  exact production-builder SHA-256, schema evidence names, bounds, ordering
  requirements, and bad plan signatures. Do not copy production Cypher into the
  handler manifest.
- Every non-test `Run` or `RunSingle` call under `internal/query` must appear in
  `testdata/query-source-coverage.yaml` with the exact enclosing symbol and call
  count. Link hot calls through `entry_ids`; new non-hot registrations must use
  a typed `non_hot` class with a production source digest and applicable bounds.
  `non_hot_reason` remains source-digest-frozen legacy migration debt and must
  not be added or changed without converting the entry to the typed form.
- The single directory outside that inventory is `internal/query/querytestutil`,
  whose non-test files hold test doubles. `DiscoverQueryCallsites` skips that one
  exact path -- not every directory that happens to carry the name -- and fails
  unless the package still earns the skip: it holds at least one non-test Go
  file, imports the standard library only, reaches `Run` or `RunSingle` only
  from a fake's own `Run`/`RunSingle` delegating to its receiver, and no non-test
  file under `internal/query` imports it. The skip and its proof are the same
  code path, so an inventory that omits the package cannot be produced without
  it. Widening the exclusion means widening that proof, not relaxing a name
  match.
- Editing the production source of a symbol registered in
  `grandfatheredNonHotSourceDigests` must convert that symbol's inventory entry
  to the typed `non_hot` form in the same change. This covers a doc-comment-only
  edit, which is deliberately stricter than the gate: the frozen digest starts at
  the `func` keyword, so a doc comment change cannot trip it and no CI check
  catches the omission. The doc comment above a grandfathered symbol is often
  where its rationale lives, and rewriting that rationale while the gate stays
  green is the case this rule exists to stop.
- Keep production-builder binding and live `PROFILE` tests in `internal/query`,
  never in this package.

## Verification

Run:

```bash
go test ./internal/queryplan -count=1
scripts/verify-package-docs.sh
```
