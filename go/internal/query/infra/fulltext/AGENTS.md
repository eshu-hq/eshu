# AGENTS.md - query/infra/fulltext

## Read first

1. `doc.go` and `README.md` for the contract.
2. `../../infra.go` (`searchResources`) and `../../infra_scope.go`
   (`infraSearchNeo4jScopedCypher`) for the legacy statements this package
   replaces.
3. `../../../storage/cypher/infra/search/` for the key, the overflow flag,
   `IndexState`, and `CountUnkeyedSearchable`.

## Invariants

- The full-text statement only narrows candidates. The legacy `whereExtra`
  (free-text or `::` equality predicate plus filters) is appended unchanged.
  Never drop or rewrite it.
- Unscoped uses `RETURN DISTINCT`; scoped does not. This mirrors the legacy
  UNION semantics (rows versus nodes). `DISTINCT` stays before
  `ORDER BY ... LIMIT`; no inner top-k.
- `Escape` covers every Lucene classic syntax character and every
  `unicode.IsSpace` rune. Add a table row in `lucene_test.go` for any change.
- NornicDB, empty-query searches, and the ArgoCD category-only read never use
  the index. The root golden tests
  (`resource_search_golden_test.go`) pin their statements byte for byte.
- Requests read the cached readiness only. Never probe per request:
  `CountUnkeyedSearchable` runs one count per infra label.
- Readiness counts only unkeyed nodes that hold a searched field. Do not
  switch it back to `CountUnkeyed`: posture writers create flagless nodes with
  no searched field, and `readiness_live_test.go` fails on that change.
- Queries past `MaxQueryRunes` keep the legacy statement. Re-measure the
  Lucene bound on the pinned Neo4j before raising it.
- A fallback runs the legacy statement with the caller's params unchanged.
  Never return partial full-text rows.
- Do not import the root `query` package; it imports this one.

## Verification

`go test ./internal/query/infra/fulltext ./internal/query -run
'TestInfraSearch|TestInfraScope' -count=1`, then the live differential
`TestLiveInfraSearchFulltextDifferential` (see `README.md` in
`go/internal/query` live-test notes and `specs/live-tests.d`).
