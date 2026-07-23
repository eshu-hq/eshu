# prod-route-to-caller — production validation

Capability: `call_graph.route_to_caller` (tool `trace_route_callers`).
Production profile: `required_runtime: deployed_services`,
`max_scope_size: multi_repo_platform`, `p95_latency_ms: 5000`,
`max_truth_level: exact`.

## Claim validated

Exact `HANDLES_ROUTE` handler resolution with bounded `CALLS` traversal and
impact summaries; `404` for a missing route, `409` for an ambiguous
endpoint/handler match, and explicit unsupported output when an endpoint
exists without a handler edge — never a guess.

## Committed reproducible evidence

**Exact resolution, conflict/not-found handling, scoped traversal filtering** —
`go/internal/query/code_route_to_caller_test.go`:
`TestHandleRouteToCallerReturnsExactHandlerAndBoundedCallers`,
`TestHandleRouteToCallerReportsUnsupportedWithoutHandlesRoute`,
`TestHandleRouteToCallerAmbiguousRouteIsConflict`,
`TestHandleRouteToCallerServiceScopeUsesWorkloadEndpointEdges`,
`TestHandleRouteToCallerScopedTraversalFiltersEveryPathNode`,
`TestHandleRouteToCallerScopedRepoOutsideGrantIsNotFound`,
`TestHandleRouteToCallerMissingRouteIsNotFound`. Reproduce:

```bash
cd go && go test ./internal/query -run TestHandleRouteToCaller -count=1
```

**Java/Spring handler resolution** —
`go/internal/query/code_route_to_caller_java_test.go`:
`TestHandleRouteToCallerResolvesJavaSpringHandler`.

**Live NornicDB correctness fix** —
`docs/internal/evidence/5287-route-to-caller-nornicdb.md` (fixes three
multi-clause reads in `go/internal/query/code_route_to_caller_graph.go` that
corrupted on the pinned NornicDB backend).

**Workflow-level bounds evidence** —
`docs/internal/evidence/route-to-caller-workflow-evidence.md` (#4089
read-only workflow; bounded selector reads, `max_depth <= 5`, `limit + 1`
caps, scoped-token in-grant path enforcement).

## Notes

No private data: this artifact cites only committed tests and committed
evidence notes, no deployment-specific values.

Related: #5552 (burn-down), #5407 (artifact-existence gate).
