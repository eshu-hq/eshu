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

## Content substring index finalization

Cold Compose bootstrap persists one `content_substring_index_state` row with a
bounded state: `not_built`, `building`, `ready`, or `failed`. Bootstrap-index
logs the finalization start and terminal state with `index_state` and
`duration_seconds`; failures also carry
`failure_class=content_substring_index_build_failure`. Pair those logs with
`eshu_dp_bootstrap_pipeline_phase_seconds{bootstrap_phase="content_index_finalization",collector_kind="bootstrap-index"}`
to identify the total exact-index build, validation, and `ANALYZE` phase as a
bootstrap long pole. All-repository substring reads fail closed until the
durable state is `ready` and both catalog indexes validate; repository-scoped
reads do not depend on this cold-build lifecycle.

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

## Typed-payload decode quarantine counters

Two counters report facts skipped during Contract System v1 typed-payload
decode because a required identity field was missing or null (`input_invalid`).
Each increment is a per-fact dead-letter: the malformed fact is skipped and NOT
projected, while every valid fact in the same batch still materializes, so one
malformed fact never stalls a scope generation or fails a whole repository
projection. A non-zero rate means the graph is under-projecting for that
label set until the collector defect is fixed; treat a sustained spike as an
accuracy alarm, not routine noise. The paired structured error log
(`reducer input_invalid fact quarantined` / `projector input_invalid fact
quarantined`) carries the `fact_id` and `missing_field` so an operator can
locate the exact fact and the field the collector dropped.

- `eshu_dp_reducer_input_invalid_facts_total` — reducer handler decode
  quarantine. Labels: `domain` (the reducer domain that consumed the fact),
  `fact_kind`.
- `eshu_dp_projector_input_invalid_facts_total` — projector canonical-extractor
  decode quarantine. Labels: `stage` (the projector extractor, for example
  `oci_registry_canonical`), `fact_kind`. The projector-side counterpart to the
  reducer counter; the two are separate instruments so an operator can tell
  which pipeline stage quarantined a fact.
- `eshu_dp_cross_scope_ownership_contended_rows_total` — owner-ledger node rows
  (#5007) a graphowner batch lost to a higher-order-key contributor from another
  scope. Label: `family` (`cloud_resource`, `ec2_instance`, or
  `kubernetes_workload`). A nonzero rate means two scopes are writing the same
  canonical node uid and the ledger is deterministically resolving the winner by
  `max (observed_at, source_fact_id)`; a sustained high rate points at
  overlapping-identity ingestion worth investigating, not data loss (the losing
  rows are intentionally skipped so the winning contributor's row stands).

Both label sets are closed and bounded (a domain/stage name and a fact-kind
string); repository ids, resource names, and generation ids stay out of
metrics and live on the structured log instead. The operator dashboard's
"Reducer input_invalid Facts (rate)" and "Projector input_invalid Facts (rate)"
panels chart both.

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

- `query_type` — the requested ranking family: `hybrid` or `semantic`. A plain
  `keyword` request is never counted (it is not a degradation).
- `reason` — why semantic ranking was unavailable: `no_embedder` (no embedder is
  configured, so hybrid is served by BM25 and semantic is refused) or
  `index_unready` (vectors are configured but the persisted vector index is not
  ready yet, so the request fell back to keyword).

An explicit `keyword` request and a fully active `hybrid_active` /
`semantic_active` run do **not** increment the counter — only genuine
degradations do. The handler also stamps `search.retrieval_state`,
`search.degraded`, and (when degraded) `search.degraded_reason` span attributes on
every request span, so a single trace shows the served mode.

Persisted semantic/hybrid retrieval also stamps the bounded
`search.index_cache` attribute on the existing request span. Values are `hit`,
`miss`, `coalesced`, `bypass_unready`, or `retry_snapshot_changed`. The API and
MCP snapshot check runs through the instrumented Postgres store named
`semantic_search_snapshot`, so its child span and database duration remain
visible alongside document, vector-metadata, and vector-value loads. This is a
span-only signal: no new metric is needed because the existing route-duration
histogram measures impact and traces retain the per-request cache decision.

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
generations have outstanding shared projection work after same-generation reducer
fact-work has drained, and no source-local projector row is already pending,
in progress. Scopes still moving through reducer backlog or in-flight liveness
recovery stay in `aging`, not `stuck`.

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

## Poison dead-letter liveness signals

The generation-liveness sweep above does not reach every wedged scope. `dead_letter`
is a `fact_work_items` **work-item** status, not a `scope_generations` status
(the generation table has no `dead_letter` state), and `eshu_dp_active_generations`
only buckets `active` generations — so a scope whose work is stuck in
`dead_letter` with no strictly-newer generation never appears there at all. The
poison-liveness sweep publishes three observable gauges and two counters for
exactly this class: `fact_work_items` rows that are `dead_letter` and whose
scope has no strictly-newer `scope_generations` row (regardless of that
generation's own status), so a permanently-wedged scope still raises an alarm.

`eshu_dp_poison_dead_letter_scopes` and `eshu_dp_poison_dead_letter_items` are
observable gauges of the current poison-class scope count and row count. Either
one being non-zero and non-draining is the alarm signal: the scope has
permanently wedged and cannot self-heal without an operator or the bounded
recovery arm. `eshu_dp_poison_dead_letter_oldest_age_seconds` reports the age,
in seconds, of the oldest poison item's `updated_at`; it is zero when the class
is empty.

These three gauges are always active regardless of whether bounded auto-retry
is enabled (`ESHU_POISON_LIVENESS_AUTO_RETRY_ENABLED`, default `false`). The
default operational posture is surface-only: an operator sees the class size
and decides whether to intervene.

Two counters describe what the bounded auto-retry sweep did, when enabled:

- `eshu_dp_poison_liveness_recovered_total` — dead-letter/poison-class rows the
  sweep re-enqueued to `pending`, bounded by a per-row recovery-attempt budget
  (`ESHU_POISON_LIVENESS_MAX_RECOVER_ATTEMPTS`, default 1) so a genuinely
  poison item cannot loop forever.
- `eshu_dp_poison_liveness_failures_total` — poison-recovery sweep failures by
  bounded reason.

Read `eshu_dp_poison_dead_letter_scopes` and `eshu_dp_poison_dead_letter_items`
against `eshu_dp_poison_liveness_recovered_total` to separate a poison class an
operator has not yet acted on from one the bounded arm is actively draining. A
rising `eshu_dp_poison_liveness_failures_total` means the sweep itself is
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

Cloud resource paging also emits route-specific, label-safe signals:

- `eshu_dp_cloud_resource_list_duration_seconds` records total page selection
  plus graph hydration time by bounded outcome.
- `eshu_dp_cloud_resource_list_scanned_rows` records the owner-ledger candidates
  returned by the bounded `limit+1` selection.
- `eshu_dp_cloud_resource_list_page_size` records resources emitted to the
  caller.
- `eshu_dp_cloud_resource_list_truncations_total` counts pages with a
  continuation cursor.
- `eshu_dp_cloud_resource_list_errors_total` counts bounded store, graph, and
  ledger/graph parity failures.

These metrics never label resource IDs, account IDs, regions, providers,
tenant IDs, or cursor values.

`POST /api/v0/code/call-graph/metrics` keeps route latency and failures in the
same two per-endpoint metrics. Its `query.call_graph.metrics` span adds the
bounded metric variant plus `expanded_edge_count`, `expanded_node_count`,
`result_count`, and `truncated`. These attributes show whether time was spent
reading a large repository edge set or processing a small result page. They do
not include repository IDs, function IDs, names, or paths.

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

### Allowed-read governance-audit async appender (F-9, #5170)

The mcp-server MCP transport middleware (`GET /sse`, `POST /mcp/message`)
records an ALLOWED `read_authorization` governance-audit event for every
scoped-token or OIDC-bearer credential that resolves successfully,
immediately before dispatch — the allowed counterpart of the DENIED events
the same middleware already recorded synchronously. A naive synchronous
append would add a Postgres round trip to every successful MCP read, so
emission goes through `governanceauditasync.AsyncAppender`: a bounded
(default 1024), single-worker, non-blocking buffered-channel appender that
drops rather than blocks when full, and flushes batches to the durable
`governance_audit_events` store from a background worker. See
`go/internal/governanceauditasync/README.md` for the full design.

Three counters, all label-free, expose the appender's drop-observability:

- `eshu_dp_governance_audit_allowed_emitted_total` — events accepted into the
  buffer. The volume dial.
- `eshu_dp_governance_audit_allowed_dropped_total` — events rejected because
  the buffer was full or the appender was closed. Non-zero means governance
  data loss is happening; this is the signal an operator should alert on.
- `eshu_dp_governance_audit_allowed_persist_failures_total` — events accepted
  but a flush to the durable store failed. Non-zero points at the durable
  store itself (bad connection, schema mismatch), not the appender.

Only the mcp-server MCP transport middleware wires a non-nil allowed-read
sink; the `/api/v0/*` HTTP API surface and `cmd/api` do not, since
`tools/call` already dispatches internally through the same credential chain
and wiring both would double-emit one logical MCP read.

Enabling this path adds one `governance_audit_events` row per allowed MCP
transport request, so it assumes the deployment has the hosted retention job
configured (`GovernanceAuditStore.DeleteExpired`,
`go/internal/storage/postgres/governance_audit_store.go`); see
[Hosted Retention And Deletion Policy](../hosted-retention-deletion-policy.md)
for how to configure the cutoff.

Observability Evidence: `go/internal/governanceauditasync/appender_test.go`
and `appender_concurrency_test.go` assert exact emitted/dropped/persist-failure
counter values against a manual OTEL reader for full-buffer drops, a
deliberately blocked sink under concurrent overflow, and sink persist
failures; `go/internal/query/auth_allowed_read_audit_test.go` proves the
emission site records exactly one event per allowed scoped/OIDC-bearer read
and zero events for shared-key, dev-open, denied, and resolver-error paths.
`go/internal/telemetry/instruments_test.go` proves all three counters
register against a real meter.

No-Regression Evidence: `AsyncAppender.Append` costs one struct copy plus one
non-blocking channel send (~130ns/op serial, ~3.5ns/op amortized under
64-way concurrent load per `BenchmarkAsyncAppenderEnqueueSerial` /
`…Parallel64`, `-benchmem -count=5`) and never touches Postgres on the
request path; the prove-theory-first benchmark
(`go/internal/storage/postgres/governance_audit_append_bench_test.go`)
recorded the rejected synchronous alternative at ~10.48ms/op against local
Postgres, ~427,000x slower than the async enqueue path measured by
`go/internal/governanceaudit/async_enqueue_prove_bench_test.go` (~24.5ns/op).
Denial paths, `/api/v0/*`, and `cmd/api` are unchanged (nil allowed-read
sink). Concurrency proof: `go test -race` on
`go/internal/governanceauditasync/...` covers 64-goroutine concurrent
enqueue with zero drops, exact drop accounting under a blocked sink, and a
bounded `Close()` even against a sink that never returns.

### Relationship catalog breakdown limiter

The relationship catalog issues one grouped `source_tool` aggregate per fixed
source-owner label (currently Repository and WorkloadInstance). The independent
owner reads overlap, scan each label once, and return every stamped verb/tool
bucket. One four-permit limiter shared by all requests in an API or MCP process
caps concurrent aggregate reads without serializing callers.
Three label-free metrics distinguish graph-query latency from local admission
pressure without adding repository, verb, tenant, or request cardinality:

- `eshu_dp_relationship_breakdown_permit_wait_seconds` records every permit
  wait, including waits that end through request cancellation.
- `eshu_dp_relationship_breakdown_queued` is the current number of breakdown
  reads waiting for a permit.
- `eshu_dp_relationship_breakdown_in_flight` is the current number holding a
  permit and cannot exceed four while the limiter contract is intact.

An elevated queued value and permit-wait p95 with in-flight pinned at four
means the local read cap is applying backpressure. Low permit wait with a slow
`POST /api/v0/relationships/catalog` route instead points to graph query cost,
not admission delay.

Observability Evidence: the focused semaphore test saturates all four permits,
observes two concurrent waiters, cancels one waiter, releases and reuses a
permit, and proves queued/in-flight return to zero. It also proves all three
instruments emit one label-free datapoint.

No-Regression Evidence: the metric calls wrap the channel send and receive at
the per-owner aggregate-read semaphore chokepoint. The four-slot capacity,
cancellation select, and catalog result ordering are unchanged. Five concurrent
callers prove four owner reads enter and the fifth waits, then every permit is
reusable. The OTEL histogram and up/down-counter operations are
concurrency-safe.

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

## Per-File Parse And Pre-Scan Cost

Within the `parse` and `pre_scan` snapshot stages above, two histograms
attribute cost per file and per bounded `language`:

- `eshu_dp_file_parse_duration_seconds` — per-file parse duration, labeled by
  `language`.
- `eshu_dp_file_prescan_duration_seconds` — per-file pre_scan duration, labeled
  by `language`. Only files that actually dispatch to a language pre-scanner
  emit a sample; languages that derive their `ImportsMap` contribution from the
  parse stage instead (php, javascript, typescript, tsx, on a full ingest)
  contribute no pre_scan sample.

Both pair with a structured-log language summary on their owning stage
(`language_parse_summary` for `parse`, `language_prescan_summary` for
`pre_scan`) so an operator can graph or log-pivot per-language cost the same
way for both stages. Full row detail, including the derive-from-parse caveat
above, lives in the "Git, Discovery, And Fact Streaming" table of
[Ingestion And Collector Metrics](metrics-ingestion-collectors.md).

Observability Evidence: `eshu_dp_file_prescan_duration_seconds` was registered
in `go/internal/telemetry/instruments.go` (#4767/#4811) but this operator
reference page never gained a row for it or its `eshu_dp_file_parse_duration_seconds`
sibling — a frozen-inventory gap this section closes. No behavior changed;
this is a documentation-only addition.

No-Regression Evidence: fixes a `git_snapshot_native.go` pre_scan stage-duration
measurement bug — `recordSnapshotStage` self-captured its end time inside the
call, so the caller's `preScanLanguageSummary(...)` argument expression (a
per-file histogram-record loop) ran before that capture and was silently
folded into `eshu_dp_collector_snapshot_stage_duration_seconds{stage="pre_scan"}`.
The fix captures `preScanEndedAt` immediately after
`PreScanRepositoryPathsWithWorkersStats` returns, builds the summary after that,
and records the stage via the new `recordSnapshotStageAt(..., preScanEndedAt, ...)`
overload that takes an already-captured end time instead of self-capturing one.
The `parse` stage was audited and does not have this bug: its
`languageParseSummary` is returned from `buildParsedRepositoryFiles` itself (the
per-file `FileParseDuration.Record` call happens inline during the measured
parse work), so no equivalent post-processing loop runs between the parse work
finishing and its `recordSnapshotStage` call. No new Cypher, graph write,
worker, lease, batch, or queue behavior; this only changes which `time.Now()`
sample is used as the stage end time. Verified by
`TestRecordSnapshotStageAtExcludesPostEndTimeWork`,
`TestRecordSnapshotStageSelfCapturesEndTimeAtCallTime`, and the existing
`TestSnapshotRepositoryRecordsPerStageTelemetry` in
`go/internal/collector/git_snapshot_stage_endtime_test.go` and
`git_snapshot_stage_telemetry_test.go`.

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
- `eshu_dp_shared_projection_lease_quarantines_total` — a counter of
  fail-closed shard pauses, labeled by `domain` and a bounded `reason` set:
  `cycle_deadline`, `heartbeat_lost`, or `cycle_error`. Repository identity and
  lease owner are deliberately excluded.

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

## Repo-Dependency RUNS_ON Retract Omissions

`eshu_dp_shared_edge_runs_on_retract_omissions_total` counts impossible
`RUNS_ON` retract transactions that the shared edge writer omits because the
exact evidence source cannot emit that relationship type. It has two bounded
labels:

- `domain` identifies the shared projection domain;
- `reason` identifies the closed omission reason, currently
  `source_capability`.

Evidence source and repository identity are intentionally excluded from metric
labels. Use the accompanying `shared edge retract role omitted` structured log
for the exact evidence source, statement role, and repository count. A rising
counter for `domain=repo_dependency` is expected while code-import refreshes
drain; a missing counter alongside those refreshes means the source-capability
fast path is not active.

Performance Evidence: the retained 896-repository run spent `362.94s` across
838 serialized code-import `RUNS_ON` retracts. Omitting that exact-source arm is
expected to save `300-360s`; the counter confirms how many transactions were
removed without introducing repository-level metric cardinality.

No-Regression Evidence: ordinary evidence sources and the prefix lookalike
`projection/code-imports-extra` retain the `RUNS_ON` retract and do not
increment the counter. The exact source increments once per omitted role. The
graph writer's worker, partition, lease, retry, ordering, and remaining
transaction boundaries are unchanged.

## Deferred backfill partition memo gate (#3624 Track 1)

The corpus-wide deferred relationship pass now skips re-loading a partition whose
`(scope_id, generation_id)` already committed backward evidence under an unchanged
catalog fingerprint (the partition memo). Two counters expose the gate's decision:

- `eshu_dp_deferred_backfill_partitions_skipped_total` — partitions skipped this
  pass, labeled `reason` (currently `catalog_unchanged`). This is the primary
  steady-state signal: a skip ratio near the partition count means the memo is
  eliminating redundant re-loads on no-change drains.
- `eshu_dp_deferred_backfill_partitions_loaded_total` — partitions loaded despite
  the memo lookup, labeled `reason` (`memo_miss`: no memo row, or the catalog
  fingerprint changed). ArgoCD-bearing partitions are excluded from the memo on
  the write side and so surface here as `memo_miss` reloads.

Read `skipped / (skipped + loaded)` for the memo hit rate. A hit rate that
collapses to zero against a stable corpus points to catalog churn (every repo
onboard/rename/remove flips the fingerprint and reloads all partitions) or a
memo-write regression. The gate also logs
`deferred_backfill_partition_memo_gate_completed` with candidate/skipped/loaded
counts for a single-line per-pass summary.

Observability Evidence: the two counters register via the `telemetry.Instruments`
contract; the gate is otherwise a single indexed `deferred_backfill_partition_memo`
lookup with no payload scan. Verified against a disposable Postgres 18 by the
`TestDeferredBackfillPartitionMemo*` suite in `go/internal/storage/postgres`.

## Graph-Write Permit Pool Split By Class (#4448)

Before this change every reducer graph write — canonical, handler-edge,
shared-projection, secrets/IAM, orphan-sweep, materializer, and semantic
entity writes — drew from ONE shared `cypher.BackpressureGate` permit pool
sized by `ESHU_GRAPH_WRITE_MAX_IN_FLIGHT` (#3652). That coupling meant a slow
write on one class could exhaust the shared permits and starve every other
class's writes even when the starved class's own workload never came close to
saturating its logical share of the pool (head-of-line blocking).

The reducer now bounds canonical-class and semantic-class writes with two
permit pools that become fully INDEPENDENT once an operator opts in:

- `ESHU_GRAPH_WRITE_CANONICAL_MAX_IN_FLIGHT` sizes the canonical gate
  (canonical, handler-edge, shared-projection, secrets/IAM, orphan-sweep, and
  materializer writes).
- `ESHU_GRAPH_WRITE_SEMANTIC_MAX_IN_FLIGHT` sizes the semantic gate (the
  semantic entity write path).
- **Legacy-only back-compat (both above unset):** a review of the first
  version of this change caught a P1 regression here — falling back each
  class independently to the legacy `ESHU_GRAPH_WRITE_MAX_IN_FLIGHT=N` would
  let up to `2N` writes run concurrently (`N` canonical + `N` semantic), an
  unmeasured doubling of the concurrency budget an existing deployment sized
  to backend headroom. The fix is a third, outer "aggregate" gate sized to
  `N` that BOTH classes draw a permit from, in addition to their own class
  gate, whenever neither per-class env is set. The combined canonical+semantic
  total therefore stays bounded by `N`, exactly reproducing the pre-#4448
  shared-pool capacity, while each class still gets its own labeled wait
  signal. As soon as an operator sets either per-class env, the aggregate gate
  is disabled and the two class gates become the sole, fully independent
  bounds — the opt-in head-of-line-blocking fix.

The projector has only one write class, so it is unaffected and keeps using
`ESHU_GRAPH_WRITE_MAX_IN_FLIGHT` as its single knob.

The existing `eshu_dp_graph_write_backpressure_engaged_total` counter and
`eshu_dp_graph_write_backpressure_wait_seconds` histogram now carry a `gate`
label (`canonical`, `semantic`, or, only in legacy-only mode, `aggregate`) in
addition to `operation`, so an operator can read each pool's engagement rate
and wait-time distribution independently instead of a blended signal that
could mask one class starving the other. In legacy-only mode a single write
acquires both the aggregate gate and its class gate, so it can emit a wait
sample for each layer independently — this is two different measurements for
the same write ("did the combined legacy-shaped budget saturate" versus "did
this class's own share saturate"), not double-counting. No new metric name
was introduced.

Performance Evidence: this is a structural concurrency fix, not a throughput
change under normal (non-starved) load — with both gates sized generously
relative to their own class's workload, wall-clock behavior is unchanged from
before the split. The fix targets the pathological case where one class's
writes are slow: before the split that pathological case degraded the OTHER
class's latency too; after the split it does not. No-Regression Evidence:
verified by the full reducer/graphbackpressure/telemetry/projector suites
under `-race` (`go test ./cmd/reducer ./internal/graphbackpressure
./internal/telemetry ./cmd/projector -race -count=1`, all passing) proving no
new goroutine leaks, deadlocks, or worker-count changes were introduced.

Observability Evidence: `TestGraphWriteGateSplitEliminatesHeadOfLineBlocking`
in `go/cmd/reducer/graph_write_permit_split_test.go` is the deterministic
regression: it saturates one class's single-permit gate with a write that
blocks forever, then proves a write on the OTHER class completes within a
500ms deadline instead of queuing behind the stuck permit, in both directions
(slow semantic blocking canonical, and slow canonical blocking semantic). The
test was verified two-sided by temporarily forcing both gates to share one
underlying `*cypher.BackpressureGate` (simulating the pre-#4448 behavior) and
confirming the test fails in both directions against that simulation, then
reverting to the real independent-gate implementation and confirming it
passes. The `gate` label on `eshu_dp_graph_write_backpressure_wait_seconds`
is the runtime signal an operator uses to confirm the same property in
production: independent per-gate wait distributions rather than one blended
series.

`TestLegacyOnlyConfigBoundsCombinedTotalToLegacyCeiling` in the same file is
the deterministic regression for the P1 fix: with only
`ESHU_GRAPH_WRITE_MAX_IN_FLIGHT=N` set (no per-class envs), it drives
concurrent writes through both the real canonical and semantic write paths
against one shared probe and asserts the combined peak concurrency never
exceeds `N`. It was verified two-sided by temporarily disabling the aggregate
gate (simulating the regression the review caught) and confirming the test
fails at exactly `2N` peak concurrency, then restoring the fix and confirming
it passes. `TestPerClassConfigDisablesAggregateGate`,
`TestAnyClassMaxInFlightSet`, and `TestAggregateMaxInFlight` in
`go/internal/graphbackpressure/backpressure_test.go` cover the
opt-in/opt-out boundary directly.

### Gate-Label Cardinality Guard

A second review (Copilot) flagged that `NewObserver` in
`go/internal/graphbackpressure/backpressure.go` documented `gateName` as
required to be one of the closed set (`CanonicalGateName`, `SemanticGateName`,
`AggregateGateName`) but recorded whatever string was passed with no
validation, so a future call-site mistake — for example accidentally passing
an operation or raw Cypher statement string instead of a gate name — could
explode the `gate` label's cardinality on
`eshu_dp_graph_write_backpressure_engaged_total` and
`eshu_dp_graph_write_backpressure_wait_seconds`.

The fix mirrors the existing `source_tool` coercion pattern
(`sourcetool.IsValid` / `"unknown"` in
`go/internal/telemetry/instruments.go:4745`): `IsValidGateName` checks
membership in the closed vocabulary, and `NewObserver` coerces any
out-of-vocabulary `gateName` to `"unknown"` before storing it, so the `gate`
label's value space stays bounded to four values
(`canonical`/`semantic`/`aggregate`/`unknown`) regardless of what a future
caller passes in. `TestIsValidGateName` and
`TestNewObserverCoercesUnknownGateName` are the direct regressions, verified
two-sided by temporarily removing the coercion and confirming
`TestNewObserverCoercesUnknownGateName` fails (the stored `gateName` was the
raw unvalidated input instead of `"unknown"`), then restoring the fix and
confirming it passes.

### Test Goroutine Hygiene

The same Copilot review flagged two goroutine leaks in
`testHeadOfLineBlockingEliminated`
(`go/cmd/reducer/graph_write_permit_split_test.go`): the holder and
"other-class" write goroutines both ran under `context.Background()`, so if
the test failed before the explicit `close(holder.release)` cleanup — either
on the holder-entered wait or on the 500ms other-class deadline, the exact
failure path this regression exists to catch — the blocked goroutine(s) would
run until the test process exited rather than being released. The fix threads
one `context.WithCancel(context.Background())` with a `defer cancel()` through
both goroutines' `Execute` calls, so every exit path (both `t.Fatal` calls and
normal completion) unblocks both via `ctx.Done()`, which
`cypher.BackpressureGate.Acquire` and `slowThenSignalExecutor.Execute` both
already select on.

No-Regression Evidence: verified with a temporary scratch harness
(deleted before commit) that forced the 500ms deadline path deterministically
and measured `runtime.NumGoroutine()` before and after. With the cancelable
context, goroutine count stayed flat (3→3) despite the forced failure; with
`context.Background()` restored (simulating the pre-fix shape), goroutine
count grew by exactly 2 (3→5) — the holder and other-class goroutines both
leaking, reproducing the bug Copilot flagged.
