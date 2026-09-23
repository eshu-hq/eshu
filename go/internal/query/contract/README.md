# query/contract

Declares the capability support rows the query surface answers under: per
capability, the maximum truth level reachable on each runtime profile, and the
profile the route requires at all.

## What lives here

One file per family, named for the family — `supply_chain.go`, `kubernetes.go`,
`freshness.go` — each an `init()` that registers its rows. `capability_matrix.go`
carries the base matrix literal and `capability_matrix_ext.go` its overflow, the
two split only because the first sits at the repo's 500-line cap.

`registry.go` holds the shared pieces: the `capabilitySupport` alias, the
profile and truth-level aliases, the `register` helper, and the capability key
constants.

## How a row reaches production

Each `init()` calls `register`, which calls
`querycontract.RegisterCapabilities`. Package `query/capability` blank-imports
this package (`capability/lookup.go`), and root imports `capability`, so those
`init()`s link into every binary that serves the query surface. Root reads the assembled registry through
`querycontract.CompatibilityCapabilityMatrix()`; it no longer writes it.

That indirection is why this package can exist at all. It does not import
`query`, so there is no cycle — it writes the same registry root reads.

## Why `register` rather than a map write

The rows used to be `capabilityMatrix[key] = support` in the query root, which
wrote the registry map directly and so skipped the duplicate and ordering
bookkeeping `RegisterCapabilities` does. A repeated key went unnoticed by
`querycontract_boundary_test.go`'s `DuplicateCapabilityRegistrations`
assertion. Registering through the API brings every row here under that guard.

## Keys

Where the family owning a route has already moved to a leaf package, the key
forwards that package's exported const so the two cannot drift. Where the route
still lives in the query root, the key is a string literal on purpose: the
capability sweep gate resolves string literals, not cross-package const
aliases. Root declares its own copy of those five in `capability_keys.go`
beside the handlers that name them.

## Checks that cover this package

`TestCapabilityMatrixMatchesYAMLContract` (root) pins the assembled matrix
against `specs/`'s capability YAML, both directions, all four truth ceilings —
it fails on a dropped row, an added row, or a changed ceiling. It does not
assert `RequiredProfile`; the YAML has no such field.
