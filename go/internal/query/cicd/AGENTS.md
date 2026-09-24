# cicd — agent instructions

## Read first

`doc.go` for the route contract, `README.md` for ownership and move evidence.

## Invariants

- A scope anchor and a 1–200 limit stay required on the list route.
- An empty grant gets the empty page without a store read; an out-of-grant
  selector is a 404 that leaks nothing.
- Change the capability rows only in `capability.go` — both routes share one
  `Support()`.
- `collector_readiness.go` must stay behavior-identical to root's copy; the
  parity test trips drift.
- This package must not import the root query package.

## Common changes

Adding a filter dimension: extend `querycontract.CICDRunCorrelationFilter`
first (it is shared with the repository and incident families), then the
SQL predicates, the scope echo, and the SQL predicate-order test in
`queries_test.go`.
