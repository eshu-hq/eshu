# prod-route-to-caller — production validation

Validation-Slug: prod-route-to-caller
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5552-claim-honesty-20260809-9 ESHU_POSTGRES_PORT=44142 NEO4J_BOLT_PORT=44187 NEO4J_HTTP_PORT=44174 GATE_API_PORT=44180 GATE_MCP_PORT=44191 GATE_PROMETHEUS_SOURCE_PORT=44190 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh --keep >/tmp/eshu-5552-b7-20260809-9.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: call_graph.route_to_caller passed its non-vacuous capability-specific assertion through the deployed API or MCP surface.

## Fresh deployed validation

A fresh credential-free Compose run rebuilt every binary, replayed all 37 cassette scope generations, drained the projector and reducer to terminal state, and exercised the committed B-12 API/MCP assertion for this capability. The gate completed with 547 passes, zero required failures, and zero advisory warnings in 133 seconds.


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
