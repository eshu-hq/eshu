# dependency — agent instructions

## Read first

`doc.go` for the route contract, `README.md` for layout and telemetry.

## Invariants

- The route stays bounded: page cap 200, one probe row for truncation, and a
  10-second read timeout. Do not add an unanchored reverse scan.
- `listDependencies` is pinned in `internal/queryplan/testdata/query-source-coverage.yaml`
  by source hash. A body edit must re-pin it, and a Cypher change needs the
  cypher-performance evidence the manifest entry points to.
- This package must not import the root query package or another handler
  family. Shared types come from `querycontract`.

- Change the capability row only in `capability.go`. The matrix and
  `main_test.go` both call `Support()`; do not reintroduce a literal copy.

## Common changes

Adding a response field: add it to `Row`, decode it in `listDependencies`,
update `internal/query/openapi/paths/repository/dependencies.go` and
`docs/public/reference/http-api/evidence-and-supply-chain.md` in the same
change.

## Anti-patterns

- Returning repository ownership from this route.
- Reintroducing root forwarders (`QueryParam`, `WriteError`, ...) instead of
  calling `querycontract` directly.
