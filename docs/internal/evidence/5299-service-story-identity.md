# Service Story identity and narrative evidence (#5299)

## Scope and retained input

Issue #5299 changes the existing Service Story read and visualization transform;
it does not add a graph write, queue, worker, or unbounded query. Measurements
use base commit `c5fb1de4e8` and the same retained `api-node-boats` corpus on both
sides. The retained topology contains 887 repositories and uses the NornicDB
PR-261 compatibility build (snapshot `149245885258`) over Bolt with Postgres as
the content/read-model store.

The final source story contains 44 nodes and 28 evidence-graph edges. The
bounded visualization packet retains 44 nodes and 62 edges after adding the
existing dossier's upstream, downstream, and runtime relationships. Both
counts remain below the 60-node and 120-edge packet limits.

## Measurements

Performance Evidence: ten sequential warm retained requests measured the same
start and terminal events for `GET /api/v0/services/api-node-boats/story` and
`POST /api/v0/visualizations/derive`. Story latency was 299 ms median and
723.4 ms p95; derive latency was 4.6 ms median and 6.5 ms p95; the combined
interactive path was 303.8 ms median and 729.9 ms p95. The route stayed inside
the checked-in 2-3 second interactive target.

Benchmark Evidence: the committed
`BenchmarkBuildServiceStoryVisualizationPacketRetainedShape` used the same
38-node/56-edge input shape on base and candidate with Go 1.26.5 on darwin/arm64
(Apple M5 Max):

| Build | ns/op range | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| `c5fb1de4e8` baseline | 56,274-58,384 | 105,343-105,344 | 1,591 |
| #5299 candidate | 61,670-64,511 | 104,947-104,949 | 1,633 |

The packet transform therefore adds about 5-7 microseconds and 42 allocations
to preserve canonical-reconciliation provenance, while slightly reducing bytes
per operation. This cost is explicit rather than described as a speedup; it is
about 1.4% of the measured 4.6 ms derive median.

The allocation-gate investigation also compared the full service-story
response. Base measured 665 allocations normally and 685-687 with the race
detector. The final candidate measured 675 normally and 697-698 with the race
detector. The 705 guard leaves seven allocations of race margin while still
detecting repeated API-surface normalization.

The prove-the-theory-first shims showed why the final implementation uses
capacity reservation and generic sorting:

- response-map growth: 626.4 ns/op, 2,184 B/op, 19 allocs/op;
- preallocated response map: 285.0 ns/op, 1,288 B/op, 16 allocs/op;
- reflective two-node sort: 179.4-193.3 ns/op, 744 B/op, 7 allocs/op;
- generic two-node sort: 129.5-146.1 ns/op, 672 B/op, 4 allocs/op.

## Correctness and operator proof

No-Regression Evidence: `go test ./internal/query ./internal/mcp -count=1`, the
full query-package coverage run, and 20 race-instrumented allocation-guard runs
pass. The console's 30 focused Service Story tests, TypeScript typecheck,
production build, bundle-budget assertions, strict MkDocs build, and
`git diff --check` pass. The output-preserving allocation and sort changes keep
the existing response assertions and deterministic-order tests green.

Authenticated retained-browser proof shows exactly one hero node labeled
`api-node-boats` with role `workload service`; the source repository remains a
separate source-backed node; Helm, Argo, runtime, and downstream roles are
distinct; noncanonical equal-label observations expose privacy-safe hashed
scope disambiguation; relationship sentences lead with human labels and
edge-proven contextual roles; and node plus relationship selections open their
evidence panels. Raw `viznode:*` values remain secondary diagnostics.

No-Observability-Change: the change keeps the existing HTTP query spans,
`eshu_http_request_duration_seconds` histogram, structured handler errors,
truth/freshness envelope, packet limits, and truncation fields. It introduces
no background execution or new failure boundary requiring a metric, span, log,
or status field. Operators continue to diagnose route latency, freshness, and
bounded-result behavior through those existing signals.
