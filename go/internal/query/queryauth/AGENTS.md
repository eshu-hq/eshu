# Agent instructions: queryauth

This is an authorization surface. Read `doc.go` and `README.md` before editing,
and treat every change here as a security change.

## Invariants

- The context key MUST have exactly one definition, in this package. A second
  key type anywhere in the query surface compiles cleanly and makes scoped
  requests read as unauthenticated. Proven: reintroducing one in root builds at
  exit 0 and fails 316 tests.
- `AuthContext` and `AuthMode` MUST NOT gain unexported methods. Package `query`
  aliases both, and an alias cannot reach unexported methods across a package
  boundary, so callers outside `internal/query` would break.
- `AuthContextFromContext` MUST return `(zero, false)` for a nil or unset
  context. A caller reading a zero AuthContext as authorized is a tenant-boundary
  failure; the boolean is the guard.
- `CleanedStrings` MUST keep preserving order and dropping blanks. Allow-list
  membership is compared against its output.

## Common changes

Adding a field to `AuthContext`: check every place that constructs one
(middleware, the scoped-token resolver, the browser-session resolver, and test
builders), because a field left at its zero value here is an implicit grant or
denial.

## Verification

From `go/`:
`go test ./internal/query/... ./internal/oidcbearer ./internal/scopedtoken -count=1`.
The last two are external consumers of the aliased type and are what prove the
alias still holds. Confirm the auth suite ran a real case count rather than
matching zero.
