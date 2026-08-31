# Agent instructions: queryselector

Read `doc.go` and `README.md` first. This resolves an untrusted client string
against the graph under authorization bounds, so treat changes as security work.

## Invariants

- `ResolveExactForAccess` MUST return `NotFoundError` when `access.Empty()`,
  before touching the graph. Removing that check lets a caller with no grants
  resolve any repository.
- The access predicate MUST stay in both queries. `access.GraphPredicate("r")`
  and `access.GraphParams(...)` are what bind the read to the caller's grants;
  a query that drops either still compiles and returns other tenants' rows.
- Both reads MUST stay parameterised (`$repo_selector`). The selector is
  client-supplied; interpolating it into the Cypher is an injection.
- The ordered-then-fallback pair is deliberate. Do not collapse it.

## When you change the query text

This callsite is pinned in `go/internal/queryplan/testdata/query-source-coverage.yaml`
by a SHA256 of its source text, and it is currently carried in
`grandfathered_non_hot.go` with an inherited non-hot disposition rather than a
modern typed one, because the query has no LIMIT and no bound has been audited.

Any edit fails the coverage gate. Before re-pinning, prove the Cypher itself did
not change (extract string literals per function with `go/parser` before and
after and compare). If the query DID change, the disposition needs a real audit,
not a re-pin.

## Verification

From `go/`: `go test ./internal/query/... ./internal/queryplan -count=1`.
Confirm the selector suite ran a real case count rather than matching zero.
