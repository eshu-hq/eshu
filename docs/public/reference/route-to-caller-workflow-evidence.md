# Route-to-Caller Workflow Evidence

Issue #4089 adds a read-only route-to-caller workflow for exact
`HANDLES_ROUTE` evidence. The route lookup never guesses a handler: it first
resolves one endpoint and one exact handler edge, returns `404` for no endpoint,
returns `409` for ambiguous endpoint or handler matches, and returns an
explicit unsupported payload when an endpoint exists without a handler edge.

Performance Evidence: the workflow adds bounded graph reads only. It performs no
graph writes, Postgres writes, queue claims, reducer projection, or background
work. The exact route selector reads at most `limit + 1` rows, service/workload
scopes use the materialized `Workload -> Endpoint` edge, the caller/callee
traversal is bounded by `max_depth <= 5` and `limit + 1`, scoped-token traversals
require every node in the returned `CALLS` path to remain in-grant, and
materialized workload/repository impact summary lists are capped by the same
request limit.

Benchmark Evidence: this PR does not add a standalone benchmark because the
change is query orchestration over the shared `GraphQuery` contract rather than
a new local algorithm. The exercised test shape uses one exact endpoint, one
exact `HANDLES_ROUTE` handler, three `CALLS` rows with `limit = 2` to prove
truncation, and separate tests for no-handler, ambiguous, service-scoped
workload endpoint ownership, scoped traversal path-node authorization,
missing-route, and scoped-denial paths.

No-Regression Evidence: `go test ./internal/query -run
'TestHandleRouteToCaller|TestOpenAPIRouteToCaller|TestServeOpenAPI|TestCapabilityMatrixMatchesYAMLContract'
-count=1` covers the API handler, exact `HANDLES_ROUTE` contract, bounded
truncation, unsupported no-handler response, ambiguous selector rejection,
scoped-token denial, OpenAPI shape, and capability matrix contract. `go test
./internal/mcp -run 'TestResolveRouteMapsTraceRouteCallers' -count=1` covers the
MCP transport route and body mapping.

No-Observability-Change: the route uses the existing API/MCP request, error, and
truth-envelope diagnostics. It introduces no new metric label, span name, log
field, runtime knob, worker, queue, retry loop, or graph-write operation.
