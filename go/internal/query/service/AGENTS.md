# AGENTS.md - service

## Read first

1. `doc.go` for the public contract.
2. `README.md` for the ownership boundary and proof requirements.
3. Parent `../AGENTS.md` for query-wide invariants.

## Invariants

- This package must not import the query root or graph drivers. Root
  (`handler.go`, `service_alias.go`) imports this package, so a root import
  here cycles, including from `_test.go` files in this package. Tests that
  need root doubles use `querytestutil`, never the root.
- Import `impact`/`impacttrace`, `repository`/`repositoryartifacts`,
  `service/evidence`, and `supplychain`, never the reverse. Those leaves must
  not import this package: several service files already import them, so a
  back-import cycles.
- `RepositoryAccessFilter` values must be derived from the request's
  `AuthContext` via `querycontract.RepositoryAccessFilterFromContext`, never
  hand-built to widen access.
- The family capability row lives in `capabilities.go` and registers through
  `querycontract.RegisterCapabilities` in this package's `init`. Do not
  re-add it to the root matrix: duplicate initialization is a contract
  failure, and two copies drift silently.
- Keep the root `service_alias.go` aliases and forwarders until every
  external caller has a separately reviewed migration path.
- The B-7 cassettes and B-12 snapshot must stay byte-identical: move code,
  never Cypher text or queue/projection behavior.

## Verification

Run focused `service` tests, then root `query`, `queryplan`, `mcp`,
`cmd/api`, and `cmd/mcp-server` suites, plus whole-module build and vet. Run
`scripts/verify-package-docs.sh` whenever this package changes.

## Common changes

- Add a handler method with its route in `Mount`, gate on the family's
  capability, and update the matching fragment under `openapi/paths/service/`
  in the same change (scripts/verify-openapi.sh scans that tree recursively).
