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
- `AllowsPermissionFeature` and `AllowsPermissionDataClasses` MUST keep failing
  open on all three pre-catalog cases: no auth context, `PermissionCatalogEnforced`
  false, and `AuthModeShared`. Narrowing any of them denies live traffic on
  deployments that have not enabled the catalog. `AllowsPermissionDataClasses`
  MUST keep requiring EVERY requested class — switching it to any-of would let a
  handler answer from a class the caller was never granted.
- The ask_search feature name and data-class list MUST have exactly one
  definition ON THE ENFORCEMENT PATH, here. Root package `query` and
  `internal/query/semanticsearch` both authorize against them and no longer
  share a package (#6060); a second enforcement literal makes a caller granted
  by one surface denied by the other.

  The same family is ALSO declared as a product contract, in
  `specs/authorization-catalog.v1.yaml` (`family: ask_search`, with the same
  three data classes) and republished in
  `go/internal/capabilitycatalog/data/catalog.generated.json`; the family name
  is additionally hardcoded in `capabilitycatalog/authz.go`'s
  `requiredPermissionFamilies`. Nothing reconciles those with this list. So a
  change to the classes here is a change to all of them: edit this file alone
  and enforcement demands a class the published catalog never advertises and the
  spec never grants, which is a 403 for every catalog-enforced caller with no
  gate to catch it.

## Common changes

Adding a field to `AuthContext`: check every place that constructs one
(middleware, the scoped-token resolver, the browser-session resolver, and test
builders), because a field left at its zero value here is an implicit grant or
denial.

## Verification

From `go/`:
`go test ./internal/query/... ./internal/oidcbearer ./internal/scopedtoken -count=1`.
The last two are external consumers of the aliased type and are what prove the
alias still holds. `./internal/query/...` covers both permission-predicate
consumers: root's ask handler and the semantic-search family. Confirm the auth suite ran a real case count rather than
matching zero.
