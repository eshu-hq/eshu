# Resource-to-code traversal observability: proof (#7931, option C)

The trace-resource-to-code handler emitted no traversal span, so the
operator could not separate anchor cost, expansion fan-out, and row
drain behind slow Repository-origin reads. This change adds the span
and histogram only. A short-circuit fix is off the table: the
contract requires repo-to-repo dependency rows for a Repository
start (arbiter RULING), and a narrowed traversal waits on a
live-backend equivalence oracle. No traversal, response, limit,
ordering, or Cypher change ships here.

## No-Regression Evidence:

- Baseline: `deployment.ImpactRepoPathCypher` /
  `ImpactScopedRepoPathCypher` built with the same anchor pattern,
  depth, `start_id`, `limit+1`, and grant params as before the change.
- After: the new span test asserts byte-equality of the emitted
  Cypher against `deployment.ImpactRepoPathCypher` and exactly one
  graph call. Trim, filter, shaping, and the response envelope are
  untouched; `impact/...`, `queryplan`, `telemetry`, and `query`
  suites pass, `go vet ./...` is clean.
- Backend: unit and fake-graph proof only. Live-backend span
  verification, the P3-P5 PROFILE census, and the option-B oracle
  are NOT_CHECKED here and stay deployment-gated.

## Observability Evidence:

- New span `query.resource_to_code.traversal` carrying start label,
  max depth, limit, scope, rows expanded and returned, and timing;
  it ends via `defer` on every post-anchor path and nests the graph
  call so error and cancel reporting attributes to it.
- New histogram
  `eshu_dp_query_resource_to_code_traversal_duration_seconds`
  (labels route, node label, outcome) reusing the sibling bucket
  literal. Registrations and the coverage doc row agree
  (`verify-telemetry-coverage.sh`).
- An operator separates anchor from expansion with the span's
  attributes and watches `rows_expanded` versus `rows_returned`
  for the next fan-out regression. Error paths end the span
  without `rows_returned`, which is success-only by design.

## Why the change is safe

Reads only, no schema, planner, queue, or lease surface. The one
incidental cleanup removes a provably dead `max_depth < 1` floor
(unreachable after the `<= 0` default). Failing test first:
`TestTraceResourceToCodeTraversalSpanCarriesStageAttributes`
failed with no ended span before the wiring and passes after.
