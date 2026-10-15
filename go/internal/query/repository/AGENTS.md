# AGENTS.md - repository

## Read first

1. `doc.go` for the public contract.
2. `README.md` for the ownership boundary and proof requirements.
3. Parent `../AGENTS.md` for query-wide invariants.

## Invariants

- This package must not import the query root or graph drivers. Root
  (`handler.go`, `repository_alias.go`, `repository_compat.go`) imports this
  package, so a root import here cycles, including from `_test.go` files in
  this package. Tests that need root doubles use `testutil`, never the
  root.
- Import `repositoryartifacts` and `repository/readmodel`, never the reverse.
- `RepositoryAccessFilter` values must be derived from the request's
  `AuthContext` via `querycontract.RepositoryAccessFilterFromContext`, never
  hand-built to widen access.
- Capability rows stay in the root matrix: `platform_impact.context_overview`
  and `platform_impact.catalog` are shared with service, workload, entity,
  and catalog stayers. Gate through `querycontract` like every other caller.
- Keep the root `repository_alias.go` alias and `repository_compat.go`
  forwarders until every external caller has a separately reviewed migration
  path.
- The B-7 cassettes and B-12 snapshot must stay byte-identical: move code,
  never Cypher text or queue/projection behavior.
- Selector errors keep the shared order (#7626):
  `querycontract.WriteGraphReadError` (503/504), then (stats only) the 504
  for a `context.DeadlineExceeded` route-budget expiry through
  `tracing.WriteServerFailure` (fixed body, span error), then
  `selector.WriteLookupFailure` (500, fixed body, span error), then
  `writeSelectorAnswer`: 404 with the typed `NotFoundError` text, 400 with the
  typed `AmbiguousError` text, and the fixed 500 for anything else. Never write
  a selector error's `err.Error()` to a body; a wrapper carries backend text.
- Any read that fails after the selector resolved answers
  `querycontract.WriteGraphReadError` with the literal capability first, then
  `tracing.WriteServerFailure` with that step's message constant from
  `failure.go` (`writeReadFailure`; stats passes
  `repositoryStatsErrorStatus(err)` so its own budget keeps 504). Add one
  constant per new failing step. Never format `err` into a body (#7626, #7674).
- Stats and story degrade a failed coverage read to a 200 partial, but call
  `writeCoverageVerdict` first so a reader fence answers 503 and a client
  cancel 499. `coverage.last_error` is a closed set of fixed strings; never
  copy `err` into it (#7674). Keep the OpenAPI `last_error` descriptions and
  `docs/public/reference/http-api/repository-coverage.md` in step with it.

## Verification

Run focused `repository` tests, then root `query`, `queryplan`, `mcp`,
`cmd/api`, and `cmd/mcp-server` suites, plus whole-module build and vet. Run
`scripts/verify-package-docs.sh` whenever this package changes.

## Common changes

- Add a handler method with its route in `Mount`, gate on the family's
  capability, and update the matching fragment under `openapi/paths/repository/`
  in the same change.
