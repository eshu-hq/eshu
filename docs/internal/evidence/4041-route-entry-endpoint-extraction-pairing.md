# Route Entry Endpoint Extraction Pairing Evidence (#4041)

Moved from `go/internal/reducer/README.md` (issue #5786) to keep the
package README under the repository's 500-line cap. Content is
unchanged from the original section.

## Route Entry Endpoint Extraction Pairing (#4041)

Performance Evidence: framework endpoint extraction stays an in-memory pass over
parser-emitted framework semantics. The baseline path iterated flattened
`route_paths` and attached one shared normalized method set to every path; the
after path uses `route_entries` when present, preserving one method/path pair per
entry and avoiding any graph, Postgres, queue, or filesystem I/O. On Apple M5
Max, `go test ./internal/reducer -run '^$' -bench
BenchmarkFrameworkAPIEndpointSignalsRouteEntries -benchmem -count=3` extracted
100 route entries in 39.0us/op, 33.1us/op, and 31.8us/op with 75.4KB/op and 705
allocs/op.

Benchmark Evidence: the benchmark input is one parser file with 100 exact
`route_entries` and no terminal queue, graph rows, or database rows because the
extractor is a pure in-memory reducer helper. The after measurement above is the
full post-change path used before endpoint-signal merge.

No-Regression Evidence: `go test ./internal/parser -run
'TestDefaultEngineParsePathGo(EmitsDeadCodeRegistrationRoots|EmitsMixedCaseServeMuxRouteEntry|IgnoresUnknownHandleFuncReceivers)'
-count=1` proves Go `net/http` route entries still emit exact handlers, including
mixed-case `ServeMux` local variables. `go test ./internal/reducer -run
'TestFrameworkAPIEndpointSignalsPreserveRouteEntryMethodPairs|HandlesRoute|APIEndpoint'
-count=1` proves endpoint extraction consumes paired `route_entries` before
falling back to legacy flattened lists, so `/payments` does not inherit `GET`
from a separate `GET /status` route.

Observability Evidence: No-Observability-Change: this is a pure extraction
correction inside existing reducer fact processing. It adds no route, graph
query, graph write, queue domain, worker, runtime knob, metric instrument,
metric label, span, or log line; operators continue to diagnose endpoint and
`HANDLES_ROUTE` projection through the existing reducer run spans, execution
counters, projection intent payloads, and shared projection status/readiness
surfaces.
