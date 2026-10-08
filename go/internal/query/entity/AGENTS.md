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
  `testutil`, never the root. `internal/graph` is allowed: it holds the
  stdlib-only schema tables (`HasUIDUniquenessConstraint` picks the uid
  anchor for entity context, #7089), not a driver.
- Import `querycontract`, `selector`, `repository`, `service`, and
  `supplychain`, never the reverse. Those leaves must not import this
  package: several entity files already import them, so a back-import
  cycles. In particular `service` must keep treating `entity.Handler` (root
  alias `query.EntityHandler`) as an opaque caller (comments only); the
  method-home rule put the seam here.
- `RepositoryAccessFilter` values must be derived from the request's
  `AuthContext` via `querycontract.RepositoryAccessFilterFromContext`, never
  hand-built to widen access.
- The shared `platform_impact.context_overview` capability matrix row stays
  in the query root: many families gate on it, so it is not this family's
  row to move. Keep the capability strings and the `TruthBasisHybrid`
  envelope basis on the moved handlers verbatim.
- `ResolveEntity` maps a `repo_id` selector error in the order
  `querycontract.WriteGraphReadError` (503/504), then
  `selector.WriteLookupFailure` (500, fixed body, span error), then 404 for
  not-found and 400 otherwise. Never write `err.Error()` of a
  `selector.LookupError` to a body: it carries backend text (#7626).
- The service routes' optional `repo` selector follows the same rule.
  `InvestigateService` calls `selector.WriteLookupFailure` after
  `WriteGraphReadError`; `serviceStoryResolutionError` (behind
  `GET /api/v0/services/{service_name}/story` and every in-process
  `BuildServiceStoryEnvelope` caller) returns the fixed
  `selector.LookupFailureMessage` envelope through
  `tracing.ServerFailureEnvelope` after `GraphReadErrorEnvelope`.
- Every other 500 on the service context, investigation, and story routes
  answers a fixed message from `failure.go` through
  `tracing.WriteServerFailure` (HTTP) or `tracing.ServerFailureEnvelope`
  (story seam), after the route's own 503 and the graph-read verdict. Never
  format the error into the body. A client cancel answers 499 with no span
  error. The story's ci/cd and supply-chain steps run `GraphReadErrorEnvelope`
  first, so a reader fence answers the retryable 503.
- Keep the root `entity_alias.go` aliases and forwarders until every
  external caller has a separately reviewed migration path.
- `GetEntityContext` records `resolved_by` (anchor, fallback, content, none)
  once per answered request, in `context_resolution.go`, never at an error
  return, and never as a new log line (#7212). Add a new answer path by
  setting `res.resolvedBy` where it writes its 200 or 404; the label set is
  closed and shared with `eshu_dp_entity_context_resolution_total`.
- The Neo4j entity-context anchor seeks every uid- and id-constrained schema
  label (#7212), derived from `graph.UIDUniquenessConstrainedLabels` and
  `graph.IDUniquenessConstrainedLabels`; never hand-list them. Its text is
  pinned byte for byte by `TestNeo4jEntityContextAnchorIsTheMeasuredStatement`
  (`testdata/neo4j_wide_anchor.cypher` plus a SHA-256). A schema constraint
  change moves that text: re-render the golden and the hash in the same change
  and state the new width; the measured width is 124 labels. The NornicDB
  per-label loop keeps `EntityContextAnchorLabels`.
- The B-7 cassettes and B-12 snapshot must stay byte-identical: move code,
  never Cypher text or queue/projection behavior.

## Verification

Run focused `entity` tests, then root `query`, `queryplan`, `mcp`,
`cmd/api`, and `cmd/mcp-server` suites, plus whole-module build and vet. Run
`scripts/verify-package-docs.sh` whenever this package changes.

## Common changes

- Add a handler method with its route in `Mount`, gate on the family's
  capability, and update the matching `openapi/paths/search/entities.go`
  fragment in the same change (scripts/verify-openapi.sh scans the nested
  fragment tree recursively).
- A new cross-family workload-context consumer belongs on the exported
  `FetchWorkloadContextForOperation` seam with a queryplan audit entry, not
  on a new unexported back-channel.
