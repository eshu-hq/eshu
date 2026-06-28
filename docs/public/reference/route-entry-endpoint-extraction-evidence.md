# Route Entry Endpoint Extraction Evidence

This note records the performance and observability evidence for preserving
parser-emitted method/path pairs when reducer endpoint extraction consumes
`framework_semantics.route_entries` (#4041).

Performance Evidence: framework endpoint extraction stays an in-memory pass over
parser-emitted framework semantics. The baseline path iterated flattened
`route_paths` and attached one shared normalized method set to every path; the
after path uses `route_entries` when present, preserving one method/path pair per
entry and avoiding graph, Postgres, queue, or filesystem I/O.

Benchmark Evidence: on Apple M5 Max, `go test ./internal/reducer -run '^$'
-bench BenchmarkFrameworkAPIEndpointSignalsRouteEntries -benchmem -count=3`
extracted one parser file with 100 exact `route_entries` in 39.0us/op,
33.1us/op, and 31.8us/op with 75.4KB/op and 705 allocs/op. The input has no
terminal queue, graph rows, or database rows because the extractor is a pure
in-memory reducer helper.

No-Regression Evidence: `go test ./internal/parser -run
'TestDefaultEngineParsePathGo(EmitsDeadCodeRegistrationRoots|EmitsMixedCaseServeMuxRouteEntry|IgnoresUnknownHandleFuncReceivers)|TestDefaultEngineParsePathNextJS'
-count=1` proves Go `net/http` route entries still emit exact handlers,
including mixed-case `ServeMux` local variables, and JavaScript-family Next.js
route entries emit only for exact app-router handler exports or named
`pages/api` defaults. `go test ./internal/reducer -run
'TestFrameworkAPIEndpointSignalsPreserveRouteEntryMethodPairs|TestFrameworkAPIEndpointSignalsPreferNextJSRouteEntries|HandlesRoute|APIEndpoint'
-count=1` proves endpoint extraction consumes paired `route_entries` before
falling back to legacy flattened lists, including the Next.js handler-entry path.
`go test ./internal/query -run TestParseFrameworkSemanticsSurfacesNextJSRouteEntries
-count=1` proves framework-route readback preserves the Next.js handler symbol.

No-Observability-Change: this is a pure extraction correction inside existing
reducer fact processing. It adds no route, graph query, graph write, queue
domain, worker, runtime knob, metric instrument, metric label, span, or log line.
Operators continue to diagnose endpoint and `HANDLES_ROUTE` projection through
the existing reducer run spans, execution counters, projection intent payloads,
and shared projection status/readiness surfaces.

## Go Third-Party Route Parity (#4096)

No-Regression Evidence: `go test ./internal/parser -run
'TestDefaultEngineParsePathGo(EmitsThirdPartyRouteEntries|SkipsAmbiguousThirdPartyRoutes|EmitsDeadCodeRegistrationRoots|EmitsMixedCaseServeMuxRouteEntry|IgnoresUnknownHandleFuncReceivers)'
-count=1` proves Gin, Echo, Chi, Fiber, and `net/http` route entries emit only
constructor-proven, literal path/method, identifier-handler registrations, while
dynamic paths, unknown receivers, closures, method values, middleware chains,
and adapter wrappers stay non-emitting. `go test ./internal/reducer -run
'TestBuildHandlesRouteIntentRows(EmitsGoFrameworkRouteMatches|EmitsExactSameFileMatch|SkipsUnknownHandler|SkipsAmbiguousHandler|SkipsEntryWithoutHandler|SkipsFrameworkWithoutRouteEntries)|TestFrameworkAPIEndpointSignalsPreserveRouteEntryMethodPairs'
-count=1` proves the parser-owned route entries produce exact
`HANDLES_ROUTE` intents only when the handler resolves to one Function entity.
`go test ./internal/query -run
'TestParseFrameworkSemantics(ExtractsGoFrameworkRoutes|SurfacesRouteHandlerSymbol|ExtractsHapiAndExpressRoutes)'
-count=1` proves the API/query readback path surfaces the persisted framework
route entries and handler symbols for all supported Go route buckets.

No-Regression Evidence: this route-parity slice is no-provider-required. It
uses local Go source bytes, tree-sitter AST nodes, import aliases, literal
constructor/group calls, and identifier handlers only. It adds no semantic
provider call, embedder, collector credential, external network dependency, or
provider-key-gated fact family.

No-Observability-Change: Gin, Echo, Chi, and Fiber route extraction is a pure
parser payload addition consumed by the existing endpoint and `HANDLES_ROUTE`
projection paths. It adds no route, graph query, graph write shape, queue
domain, worker, lease, runtime knob, metric instrument, metric label, span, or
log line. Operators continue to diagnose route projection through existing file
fact payloads, reducer run spans, reducer execution counters, projection intent
payloads, shared projection status/readiness surfaces, and API/MCP query spans.
