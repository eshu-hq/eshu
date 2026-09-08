# AGENTS.md - entity

## Read first

1. `doc.go` for the public contract.
2. `README.md` for the ownership boundary and proof requirements.
3. Parent `../AGENTS.md` for query-wide invariants.

## Invariants

- This package must not import the query root or graph drivers. Root
  (`handler.go`, `entity_alias.go`, `family_impact_trace_deployment.go`)
  imports this package, so a root import here cycles, including from
  `_test.go` files in this package. Tests that need root doubles use
  `querytestutil`, never the root.
- Import `querycontract`, `queryselector`, `repository`, `service`, and
  `supplychain`, never the reverse. Those leaves must not import this
  package: several entity files already import them, so a back-import
  cycles. In particular `service` must keep treating `EntityHandler` as an
  opaque caller (comments only); the method-home rule put the seam here.
- `RepositoryAccessFilter` values must be derived from the request's
  `AuthContext` via `querycontract.RepositoryAccessFilterFromContext`, never
  hand-built to widen access.
- The shared `platform_impact.context_overview` capability matrix row stays
  in the query root: many families gate on it, so it is not this family's
  row to move. Keep the capability strings and the `TruthBasisHybrid`
  envelope basis on the moved handlers verbatim.
- Keep the root `entity_alias.go` aliases and forwarders until every
  external caller has a separately reviewed migration path.
- The B-7 cassettes and B-12 snapshot must stay byte-identical: move code,
  never Cypher text or queue/projection behavior.

## Verification

Run focused `entity` tests, then root `query`, `queryplan`, `mcp`,
`cmd/api`, and `cmd/mcp-server` suites, plus whole-module build and vet. Run
`scripts/verify-package-docs.sh` whenever this package changes.

## Common changes

- Add a handler method with its route in `Mount`, gate on the family's
  capability, and update the matching `openapi_paths_entities.go` fragment
  in the query root in the same change (scripts/verify-openapi.sh only
  scans the root `openapi_paths_*.go` files; fragments must stay there).
- A new cross-family workload-context consumer belongs on the exported
  `FetchWorkloadContextForOperation` seam with a queryplan audit entry, not
  on a new unexported back-channel.
