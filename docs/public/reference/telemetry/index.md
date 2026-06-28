# Telemetry Overview

Eshu telemetry exists to answer operator questions quickly:

- Is a runtime healthy or only alive?
- Is work stuck, retrying, dead-lettered, or simply not needed?
- Which stage owns the cost: collection, parsing, fact commit, projection,
  reducer materialization, graph write, or read query?
- Can the next engineer prove a runtime change with metrics, traces, and logs?

The code source of truth is `go/internal/telemetry`. Public docs explain how to
use that contract without turning this page into a second copy of the code.

## Start Here

Use this route map instead of reading every telemetry page front to back.

| Question | Start with |
| --- | --- |
| Which runtime is unhealthy or behind? | [Runtime Signals](runtime-signals.md) |
| Which metric names should I graph? | [Metrics](metrics.md) |
| Which headline panels should I import into Grafana? | The `eshu-operator-overview.json` dashboard under `docs/public/observability/dashboards/` (also linked in the Observability nav, top of this page) |
| Which collector or ingestion counter changed? | [Ingestion And Collector Metrics](metrics-ingestion-collectors.md) |
| Which reducer, graph, storage, or correlation metric changed? | [Reducer And Storage Metrics](metrics-reducer-storage.md) |
| Where did time go for one request, scope, or graph write? | [Traces](traces.md) |
| What exact error, repo, work item, or retry happened? | [Logs](logs.md) |
| How do I stitch async service work together? | [Cross-Service Correlation](cross-service-correlation.md) |
| How do I validate shared-write changes safely? | [Shared-Write Operations](shared-write-operations.md) |
| How do I debug large ingestion and memory pressure? | [Streaming And Memory](streaming-memory.md) |

## Signal Order

1. Start with runtime health and queue age.
2. Use metrics to find the service, phase, and backlog that changed.
3. Use logs to identify the affected scope, generation, work item, domain, or
   failure class.
4. Use traces to explain the latency shape inside that exact operation.
5. Use `/admin/status` to confirm live queue, generation, and failure state
   before restarting services or forcing a broader re-index.

Metrics should stay bounded and dashboard-safe. Repository paths, file paths,
state locators, page IDs, package names, delivery IDs, commit SHAs, and
work-item IDs belong in logs or trace attributes, not metric labels. Cloud and
infrastructure resource identifiers that can contain sensitive names should use
safe log fields such as `resource.fingerprint`, `resource.identity_kind`, and
`resource.type` instead of raw values.

## Health Versus Completeness

Do not treat a green pod as proof that the graph is complete.

| Signal | What it proves | What it does not prove |
| --- | --- | --- |
| `/healthz` | The process is alive. | Work is current. |
| `/readyz` | The runtime has enough dependencies to serve. | Queues are empty. |
| `/metrics` | Prometheus can scrape runtime and data-plane signals. | The graph is correct. |
| `/admin/status` | Runtime backlog, generation, failure, and domain status. | The underlying source did not change after the last collection. |
| Query/API result | Current read-path answer. | The whole pipeline is healthy. |

Use `/admin/status` and queue metrics when the user-facing question is
freshness, convergence, or "why is this repo still not reflected?"

## Change Gate

Runtime-affecting changes must keep telemetry useful. A PR that touches
collectors, parsers, reducers, projectors, graph writes, queues, workers,
runtime settings, NornicDB defaults, or API/MCP graph queries needs one of
these tracked notes in versioned docs:

- `Performance Evidence:` for before/after runtime proof.
- `Benchmark Evidence:` for focused benchmark proof.
- `No-Regression Evidence:` for correctness-only changes with same-shape
  no-regression proof.
- `Observability Evidence:` when new or existing signals prove the path is
  diagnosable.
- `No-Observability-Change:` only when existing metrics, spans, logs, status
  fields, or pprof output already diagnose the changed path. Name them.

PR text alone is not enough. Keep the evidence in repo docs so the next
reviewer and the next agent can verify it without scraping GitHub.

## Local And Kubernetes Collection

- Docker Compose exposes runtime `/metrics` endpoints directly. Add the
  telemetry Compose overlay when you want the local OTEL Collector and Jaeger.
- Kubernetes deployments use Helm-rendered service ports and optional
  `ServiceMonitor` resources.
- `OTEL_EXPORTER_OTLP_ENDPOINT` enables OTLP gRPC trace and metric export.
  When it is empty, the Go data plane uses noop OTLP exporters while keeping
  the Prometheus endpoint active.
- Bootstrap indexing is a local or operator-run one-shot activity. It is not a
  steady-state `ServiceMonitor` target in the public chart.

## Code Contract

Telemetry names are frozen in the Go package:

- metric instruments: `go/internal/telemetry/instruments.go`
- metric dimensions, span names, and log keys:
  `go/internal/telemetry/contract.go` plus companion `contract_*.go` files
- pipeline phases and logger helpers: `go/internal/telemetry/logging.go`
- package-level maintainer contract: `go/internal/telemetry/README.md`

For the machine-enforced enumeration of every reducer / collector / parser /
queue / graph-write / MCP-API / span / log-key stage and the metric or
`No-Observability-Change:` marker it must emit, see
[Telemetry Coverage Contract](../../observability/telemetry-coverage.md).
That contract is the source of truth that the CI coverage script (X2) diffs
against when a new pipeline stage lands without a corresponding metric or
marker.

When adding a signal, update the code contract first, then update the focused
public telemetry page that operators use for that signal.
The incident context read route uses `query.incident_context` with stable
`http.route` and `eshu.capability` span attributes so on-call lookups can be
distinguished from generic service context or supply-chain reads.

Semantic evidence list routes use `query.semantic_evidence` with stable
`http.route` and `eshu.capability` span attributes. The underlying Postgres read
uses `postgres.query` with `db.operation=list_semantic_evidence`, letting
operators separate opt-in semantic observation or code-hint inspection from
deterministic documentation, code, and graph-truth reads.

## Reducer worker-pool gauge

The reducer publishes `eshu_dp_worker_pool_active` (labeled `pool="reducer"`),
an observable gauge of the number of intents executing concurrently — i.e. the
active reducer workers. It is sourced by decorating the reducer's executor with
an atomic in-flight counter, so every execution path (sequential, per-item
concurrent, and batch concurrent) is counted in one place without touching the
worker loops. Reading it costs an atomic load per scrape. Compare it against the
configured `ESHU_REDUCER_WORKERS` to see saturation, and against
`eshu_dp_queue_depth` to see whether a backlog is caused by too few active
workers or by upstream starvation.

## Shared-acceptance read-model gauge

The reducer publishes `eshu_dp_shared_acceptance_rows`, an observable gauge of
the durable shared-projection acceptance row count. It lets an operator see the
size of the shared-acceptance read model — growth indicates accepted bounded
units accumulating; a flatline alongside a draining queue indicates projection
has caught up.

The gauge is sourced from the PostgreSQL planner row estimate
(`pg_class.reltuples`), not an exact `COUNT(*)`, so it is an approximation that
tracks the true count within autovacuum/`ANALYZE` freshness. This is deliberate:
the observable callback runs on every metrics scrape, and a per-scrape full table
scan would be a real cost on a large acceptance table.

Observability Evidence: `eshu_dp_shared_acceptance_rows` and
`eshu_dp_worker_pool_active` were defined-but-never-registered instruments; the
reducer now registers both callbacks, so the read-model size and active worker
count are visible to operators for the first time. Verified by
`TestRegisterAcceptanceObservableGauges_WithObserver` /
`TestRegisterObservableGauges_WithObservers` (emission), `TestAcceptanceRowCount*`
(the estimate source), and `TestActiveWorkerExecutorTracksConcurrency` (the
in-flight counter under `-race`).

No-Regression Evidence: the acceptance observer issues a single O(1) catalog
lookup (`SELECT reltuples FROM pg_class …`) per scrape — no table scan. The
worker gauge adds two atomic operations per executed intent (one increment, one
deferred decrement) and an atomic load per scrape — no lock, no contention, and
no new worker, queue, lease, or graph write. Backend: PostgreSQL via the existing
reducer pool. Verified by `go test ./internal/storage/postgres ./cmd/reducer
./internal/telemetry -count=1` (and `-race` on the worker-gauge test).

## Cross-repo activation fence counter

The reducer publishes `eshu_dp_cross_repo_activation_fenced_total` (label
`scope_id`), a counter incremented when a cross-repo resolution generation's
activation (publish to the repo-dependency surface) is withheld because its
durable graph-acceptance intents failed to commit. The handler commits the
graph-acceptance intents before it activates the generation, and the
repo-dependency projection lane additionally gates graph-projection authority on
the relationship generation being active. Together these make activation the
single fence that opens the graph and the Postgres relationship read-model
surfaces at the same time, so neither surface can run ahead of the other.

A non-zero rate means a partial failure left a generation un-published (no
stranded denormalized edges were exposed); the reducer retry path converges the
generation idempotently on a later cycle. Pair it with the `cross-repo
activation fenced` warn log, which names the scope, generation, withheld intent
count, and failure class. The #3559/#3616 reconciler
(`eshu_dp_reconciliation_convergence_total`) remains as defense-in-depth for any
residual drift.

## Graph orphan-node gauge

The reducer publishes `eshu_dp_graph_orphan_nodes`, an observable gauge of
zero-relationship graph nodes in the closed orphan-sweep label set. The only
metric label is `node_label`; repository ids, resource names, generation ids,
and graph node ids stay out of metrics.

The callback runs bounded count queries through the graph read port and caps
each label by `ESHU_GRAPH_ORPHAN_SWEEP_COUNT_LIMIT`. Treat the gauge as a cleanup
pressure signal. Use reducer sweep logs for cycle duration, mark/delete counts,
and `failure_class=graph_orphan_sweep_error` when the count is not draining.

Observability Evidence: `TestRegisterGraphOrphanObservableGauge_WithObserver`
proves the gauge emits one datapoint per observed closed label. No-Regression
Evidence: the count path uses static-label, zero-relationship queries with a
configured per-label limit and no graph mutation; cleanup mutations run in the
separate `GraphOrphanSweepRunner`.

## Extraction-provenance drift gauges

The reducer publishes two observable gauges that let an operator see how the
graph's edge-provenance and file-language composition is shifting over time,
without exposing any repository identifiers or resource names.

`eshu_dp_edges_by_source_tool` is an observable gauge of the current graph
edge count partitioned by the `source_tool` label. The only metric label is
`source_tool`; its value is bounded by the closed `sourcetool.Canonical`
vocabulary (terraform, ansible, helm, …). Any value from the graph that is not
in that vocabulary is coerced to `"unknown"` before reaching the label so the
time-series set stays closed even if the graph holds a stale or unrecognised
token. A `source_tool` series dropping to zero is the signal that a parser or
ingester stopped writing that edge type.

`eshu_dp_files_by_language` is an observable gauge of the current `File` node
count partitioned by language. The only metric label is `language`; its value
is written by the parser registry at ingest time, so the set is bounded by the
parsers Eshu ships. Empty language values are skipped. A language series
dropping to zero is the signal that the corresponding parser stopped indexing
files.

Both callbacks read through the graph read port (`ProvenanceCountStore`) and
return **exact** counts, so the drift signal is sound — a series dropping to
zero is a real "stopped emitting" event, never a sampling artifact. The edge
gauge runs one aggregate per Tier-2 relationship type that carries
`source_tool` (`DEPENDS_ON`, `DEPLOYS_FROM`, `USES_MODULE`,
`READS_CONFIG_FROM`, `PROVISIONS_DEPENDENCY_FOR`, `DISCOVERS_CONFIG_IN`,
`RUNS_ON`) and sums them; each per-type aggregate is answered by the
relationship-type index, so there is no unanchored all-edge scan. The file
gauge is a `File`-label-anchored group. Both filter on the relevant property
IS NOT NULL, so cost is proportional to the provenance-annotated fraction of
the graph.

`ESHU_GRAPH_COUNT_LIMIT` (default 10 000) bounds the number of distinct groups
returned — label cardinality only, **not** the rows counted, so per-group
counts are always exact. The closed `source_tool` and language vocabularies are
small, so the cap is a safety valve, not a limit reached in practice.

Observability Evidence: `TestRegisterEdgesBySourceToolObservableGauge_WithObserver`
proves the gauge emits one datapoint per observed closed label;
`TestRegisterEdgesBySourceToolObservableGauge_UnknownCoercion` proves
out-of-vocabulary tokens are coerced to `"unknown"`.
`TestRegisterFilesByLanguageObservableGauge_WithObserver` proves the files
gauge emits one datapoint per observed language.
`TestEdgesBySourceToolCypherIsTypeAnchored` and
`TestFilesByLanguageCypherIsLabelAnchoredGroupLimited` prove the read queries
are index-answered and exact (type-anchored edges, label-anchored grouped
files, no row sampling). No-Regression Evidence: callbacks read through
`ProvenanceCountStore` using relationship-type-index and File-label-anchored
aggregates and perform no graph mutations.

## Search hybrid-degradation signal

`eshu_dp_search_hybrid_degraded_total` is a counter the `POST /api/v0/search/semantic`
handler increments when a request asked for semantic ranking but was served
without it. Hybrid search degrades to deterministic BM25 (keyword) ranking when
no embedder/vector ranking is available, and semantic-only requests are refused
in that mode. The metric makes that degradation visible to operators; the
response itself already reports the served mode in its `retrieval_state` field
(`hybrid_active`, `hybrid_degraded`, `semantic_active`, `semantic_unavailable`,
`keyword_only`).

The two labels are bounded:

- `query_type` — the requested ranking family: `hybrid` or `semantic`.
- `reason` — why semantic ranking was unavailable. Today the only value is
  `no_embedder`.

An explicit `keyword` request and a fully active `hybrid_active` /
`semantic_active` run do **not** increment the counter — only genuine
degradations do. The handler also stamps `search.retrieval_state`,
`search.degraded`, and (when degraded) `search.degraded_reason` span attributes on
every request span, so a single trace shows the served mode.

**Degraded search is expected, not an error, in no-provider mode.** Eshu runs
deterministic keyword search with no embedder configured by design (the
no-provider invariant); this counter is how an operator confirms semantic ranking
is or is not active, not an alarm by itself. A sudden rise after an embedder was
expected to be configured is the actionable signal.

Degradation rate over a 5-minute window:

```promql
sum(rate(eshu_dp_search_hybrid_degraded_total[5m])) by (query_type, reason)
```

Fraction of semantic-search traffic served degraded (the denominator is the
per-endpoint request-duration histogram count for the route):

```promql
sum(rate(eshu_dp_search_hybrid_degraded_total[5m]))
  /
sum(rate(eshu_dp_api_request_duration_seconds_count{route="POST /api/v0/search/semantic"}[5m]))
```

Observability Evidence: `TestSemanticSearchHandlerEmitsDegradedCounterOnHybridFallback`
and `TestSemanticSearchHandlerEmitsDegradedOnSemanticNoEmbedder` prove the counter
increments on the hybrid-degraded and semantic-no-embedder paths;
`TestSemanticSearchHandlerDoesNotEmitDegradedOnActiveHybrid` proves an active
hybrid run does not; `TestSemanticSearchDegradation` proves the bounded
state-to-label classification. No-Regression Evidence: the counter is recorded at
the handler chokepoint only; the pure `searchhybrid` and `searchretrieval`
packages keep their no-telemetry, no-I/O contract.

## Generation-liveness signals

The reducer's generation-liveness sweep publishes one observable gauge and three
counters so an operator can tell whether active scope generations are converging
or wedged.

`eshu_dp_active_generations` is an observable gauge of the current active scope
generation count by closed activation-age bucket. The only metric label is
`age_bucket`, with the bounded values `fresh`, `aging`, and `stuck`. The `stuck`
bucket is the alarm signal: a non-zero, non-draining `stuck` count means
generations are activating but not completing.

Three counters describe what the sweep did about it:

- `eshu_dp_generation_liveness_recovered_total` — wedged active generations the
  sweep re-drove through projector re-enqueue.
- `eshu_dp_generation_liveness_superseded_total` — orphaned older active
  generations the sweep superseded.
- `eshu_dp_generation_liveness_failures_total` — recovery sweep failures by
  bounded reason.

Read `eshu_dp_active_generations{age_bucket="stuck"}` against the recovered and
superseded counters to separate self-healing (the sweep is re-driving or
superseding wedged generations) from a stuck backlog the sweep cannot clear. A
rising `eshu_dp_generation_liveness_failures_total` means the sweep itself is
failing; use reducer logs keyed on the bounded failure reason to find why.

## Per-Endpoint Request Metrics

Every query API and MCP read route emits two metrics, recorded once by a shared
middleware so coverage is uniform across all endpoints rather than only the few
with bespoke instruments:

- `eshu_dp_api_request_duration_seconds` — handler latency histogram. Its
  `_count` series doubles as the per-endpoint request rate.
- `eshu_dp_api_request_errors_total` — server-error (5xx) counter.

Both are labeled by `route` (the matched route pattern, e.g.
`GET /api/v0/iac/resources`, which already encodes the method) and
`status_class` (`2xx`, `4xx`, `5xx`, …). The `route` value space is the fixed
set of registered routes, so cardinality stays bounded; concrete request paths
with identifiers are never used as labels. Requests that match no route are
labeled `route="unmatched"`. The admin surface (probes, `/metrics`) is served by
a separate mux and is intentionally not counted.

Observability Evidence: before this change, sampled query/API handlers recorded
few read-path metrics and there was no per-endpoint latency or error signal, so
an operator could not tell which route was slow or failing from metrics alone.
The middleware gives uniform per-route p50/p95/p99 latency (via the histogram)
and per-route 5xx rate, verified by a Prometheus scrape assertion in
`go/internal/query/request_metrics_test.go` that exercises a success, a server
error, and an unmatched route and confirms the metric families and `route` /
`status_class` labels appear in the `/metrics` exposition.

No-Regression Evidence: recording adds one in-memory route match
(`mux.Handler`) and two metric records per request on the read path; no graph
write, query, worker, or queue behavior changes. Backend: NornicDB (Bolt) /
Postgres read path unchanged. Verified by
`go test ./internal/telemetry ./internal/query ./cmd/api ./cmd/mcp-server
-count=1`.

## Per-Collector Run Metrics

Every collector — git source, discovery, parser, terraform-state, sbom,
package-registry, documentation/media/ocr, the live cloud and ticketing
collectors, and so on — runs under the shared claimed-service worker harness
(`ClaimedService.processClaimed`). Two metrics are recorded once at that single
dispatch chokepoint, so one instrumentation point covers all collector families
without touching individual collector packages:

- `eshu_dp_workflow_claim_run_duration_seconds` — histogram of one claim's
  wall time from heartbeat to complete/fail. Labels: `collector_kind`,
  `source_system`, and `outcome` (a bounded enum: `success`, `unchanged`,
  `released`, `fail_retryable`, `fail_terminal`). Recorded on every return path,
  so failed and released claims are timed too. To find the per-collector long
  pole: `sum by (collector_kind)` of the `_sum` series over the `_count` series
  is the mean run duration per collector family.
- `eshu_dp_workflow_claim_facts_emitted_total` — counter of facts committed
  per run, from `CollectedGeneration.FactCount`. Labels: `collector_kind`,
  `source_system`. Recorded on the success path only.

Both labels are bounded: `collector_kind` is a fixed `scope.CollectorKind`
constant and `source_system` is a small fixed set, so cardinality stays bounded
— no repo, scope, generation, or instance identifiers are ever used as labels.
The matching trace span is `collector.claimed_run`, carrying the same
`collector_kind`, `source_system`, and `outcome` attributes so a trace
correlates with the duration histogram.

These share the `collector_kind` label with the per-stage
`eshu_dp_bootstrap_pipeline_phase_seconds` and `eshu_dp_content_entity_emitted_total`
metrics, so the per-collector and per-stage layers join cleanly: an operator
can attribute corpus-run wall time to a collector family, then to a pipeline
phase and a source-file kind, from metrics alone.

Observability Evidence: during the prior full-corpus sign-off the time spent
per collector could only be reconstructed with strace and DB forensics. With
these metrics an operator reads the per-collector long pole
(`eshu_dp_workflow_claim_run_duration_seconds`) and per-collector fact volume
(`eshu_dp_workflow_claim_facts_emitted_total`) directly from the metrics port,
and the `collector.claimed_run` span gives the correlated per-claim trace.
Verified by metric and span assertions in
`go/internal/collector/claimed_service_run_metrics_test.go` covering all five
outcomes and the span attributes.

No-Regression Evidence: the timing wrapper is a `time.Now` diff around work
`processClaimed` already performs; the fact counter reads an integer
(`CollectedGeneration.FactCount`) already populated at the seam. No extra IO,
graph write, query, worker, or queue behavior changes. The OTEL
`Float64Histogram.Record` and `Int64Counter.Add` calls are concurrency-safe;
the timing and outcome values are call-local, so the N concurrent claimed-service
workers share no mutable state through the recording path. Verified by
`go test ./internal/collector ./internal/telemetry -count=1` and the
claimed-service `-race` suite.

## Per-(Domain, Partition) Shared-Projection Drain Telemetry

The reducer shared-projection runner now publishes two instruments for #3624
Phase 1 per-domain attribution:

- `eshu_dp_shared_projection_partition_processing_seconds` — a histogram of
  the full `ProcessPartitionOnce` wall time (lease claim + selection + retract
  + write + mark_completed) labeled by `domain` and `partition_id`
  (0-based slot, bounded by `ESHU_SHARED_PROJECTION_PARTITION_COUNT`). This is
  the primary long-pole signal: read `max by (domain, partition_id)`
  to identify which (domain, partition) pair dominates the cycle and whether the
  cost is in graph writes or Postgres.
- `eshu_dp_shared_projection_intents_completed_total` — a counter of intents
  marked completed, labeled by `domain` only (bounded domain set).
  Combine with an intent-emit counter to derive per-domain pending depth and
  drain rate without a per-scrape table scan.

Graph-write back-pressure gate-acquire wait is already covered by the existing
`eshu_dp_graph_write_backpressure_wait_seconds` histogram (labeled by
`operation`), which records every time a write blocks for a permit from the
shared `ESHU_GRAPH_WRITE_MAX_IN_FLIGHT` pool. No new gate-wait instrument is
needed.

Performance Evidence: Phase 2 remote measurement pending — these instruments
exist so the next corpus run can provide the per-domain latency distribution
that confirms the Phase 2 scaling parameters
(`ESHU_SHARED_PROJECTION_PARTITION_COUNT`, `ESHU_SHARED_PROJECTION_WORKERS`).

Observability Evidence: `eshu_dp_shared_projection_partition_processing_seconds`
and `eshu_dp_shared_projection_intents_completed_total` are the missing
per-domain attribution signals. Before this change an operator could see total
cycle count and overall processing duration (via the existing instruments) but
could not attribute latency or throughput to a specific (domain, partition) pair.
Verified by `TestRecordSharedProjectionPartitionMetrics_HistogramAndCounter`
(emission and label correctness), `TestRecordSharedProjectionPartitionMetrics_SkipsZeroDuration`
(no spurious zero-bucket emission), and
`TestRecordSharedProjectionPartitionMetrics_CardinalityBounded`
(forbidden label discipline) in `go/internal/reducer/shared_projection_runner_test.go`.

No-Regression Evidence: both instruments record via two concurrency-safe OTEL
calls (`Float64Histogram.Record`, `Int64Counter.Add`) inside the existing
`processPartitionWithTelemetry` path. No new worker, goroutine, query, lease,
graph write, or queue behavior is introduced. Verified by
`go test ./internal/reducer ./cmd/reducer ./internal/telemetry -count=1 -race`
(2678 tests, all passing).
