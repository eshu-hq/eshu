# AGENTS.md - impact

## Read first

1. `doc.go` for the public contract.
2. `README.md` for the ownership boundary and proof requirements.
3. Parent `../AGENTS.md` for query-wide invariants.

## Invariants

- This package must not import the query root or graph drivers. Root
  (`compare.go`, `family_impact_shim.go`) imports this package, so a root
  import here cycles, including from `_test.go` files in this package. The
  external `impact_test` package (`impact_defaults_test.go`) is the only
  exception: nothing imports it, so it may wire root constructors.
- Import `impacttrace`, never the reverse.
- Family capability rows live in `capabilities.go` and register through
  `querycontract.RegisterCapabilities` in this package's `init`. Do not
  re-add them to the root matrix: duplicate initialization is a contract
  failure, and two copies drift silently.
- Keep the `family_impact_shim.go` aliases and forwarders until every
  external caller has a separately reviewed migration path.
- `RepositoryAccessFilter` values must be derived from the request's
  `AuthContext`, never hand-built to widen access.
- `BuildDeploymentTraceResponse` requires a non-nil caller-built overview
  map; counts attach in place. Evidence lists attach only when absent;
  `deployment_truth_tier` stays caller-owned.

## Verification

Run focused `impact` tests, then root `query`, `impacttrace`, `queryplan`,
and `mcp` suites, plus whole-module build and vet. Run
`scripts/verify-package-docs.sh` whenever this package changes. The B-7
cassettes and B-12 snapshot must stay byte-identical: this family moves code,
never Cypher text or queue/projection behavior.

## Common changes

- Add a handler method with its route in `Mount`, gate on the family's
  capability, and register the capability here if it is new.
- Promote a helper to `impacttrace` only when it touches no handler state;
  otherwise it stays here.
- Export a boundary symbol only when a staying root test or external caller
  pins it; keep the home here and document the reason.

## Failure modes

- A test binary for this package skips root `init`: gated paths 501 and
  unset backends nil-panic without `impact_defaults_test.go`.
- Re-adding a capability row to the root matrix trips the duplicate-
  initialization contract test.
- A copied type instead of an alias breaks source identity across storage
  and handler adapters.
- Rebuilding the service-story overview inside this package duplicates root
  logic that will drift; derive only what shared `querycontract` helpers
  yield identically.

## Anti-patterns

- Do not import the query root, even from tests (internal test files cycle;
  use the external test package when root constructors are needed).
- Do not duplicate capability rows, overview shaping, or test fakes that
  already live in `querytestutil` or `querycontract`.
- Do not replace root function wrappers with mutable function variables.

## ADR-controlled changes

Changing capability overwrite semantics, removing the root compatibility
layer, or moving route assembly into this package requires an accepted
architecture decision before implementation.
