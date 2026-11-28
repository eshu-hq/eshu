# Telemetry Overview

Eshu telemetry exists to answer operator questions quickly:

- Is a runtime healthy or only alive?
- Is work stuck, retrying, dead-lettered, or simply not needed?
- Which stage owns the cost: collection, parsing, fact commit, projection,
  reducer materialization, graph write, or read query?
- Can an engineer verify a runtime change with metrics, traces, and logs?

The code source of truth is `go/internal/telemetry`. Public docs explain how to
use that contract without turning this page into a second copy of the code.

## Start Here

Use this route map instead of reading every telemetry page front to back.

| Question | Start with |
| --- | --- |
| Which runtime is unhealthy or behind? | [Runtime Signals](runtime-signals.md) |
| Which metric names should I graph? | [Metrics](metrics.md) |
| Which headline panels should I import into Grafana? | The `eshu-operator-overview.json` dashboard under `docs/public/observability/dashboards/` |
| Which collector or ingestion counter changed? | [Ingestion And Collector Metrics](metrics-ingestion-collectors.md); generation outcomes: [Generation Outcome Telemetry](generation-outcomes.md) |
| Which reducer, graph, storage, or correlation metric changed? | [Reducer And Storage Metrics](metrics-reducer-storage.md); identity fact cache: [Identity Fact Cache Metrics](metrics-identity-cache.md) |
| Where did time go for one request, scope, or graph write? | [Traces](traces.md); deferred backfill timing: [Deferred Backfill Block Timing](deferred-backfill-blocks.md); supply-chain impact winners and supports: [Supply-Chain Impact Winners Resweep](supply-chain-impact-winners.md) and [Supply-Chain Impact Supports](supply-chain-impact-supports.md); per-request `Server-Timing` header: [Request Timing](../http-api/request-timing.md) |
| What exact error, repo, work item, or retry happened? | [Logs](logs.md) |
| How do I stitch async service work together? | [Cross-Service Correlation](cross-service-correlation.md) |
| How do I validate shared-write changes safely? | [Shared-Write Operations](shared-write-operations.md) |
| How do I debug large ingestion and memory pressure? | [Streaming And Memory](streaming-memory.md) |

## Signal Order

1. Start with runtime health and queue age.
2. Use metrics to find the service, phase, and backlog that changed.
3. Use logs to identify the affected scope, generation, work item, domain, or
   failure class.
4. Use traces to explain latency inside that operation; [PostgreSQL reader metrics](metrics.md#postgresql-reader-access) separate checkpoint, pool pressure, replay lag, and business SQL, while [code-topic probe spans](code-topic-probes.md) identify the slow partition.
5. Use `/admin/status` to confirm live queue, generation, and failure state
   before restarting services or forcing a broader re-index.

Metrics stay bounded and dashboard-safe. Repository paths, file paths, state
locators, page IDs, package names, delivery IDs, commit SHAs, and work-item IDs
belong in logs or trace attributes, not metric labels. Cloud and infrastructure
resource identifiers that can contain sensitive names use safe log fields such
as `resource.fingerprint`, `resource.identity_kind`, and `resource.type`
instead of raw values.

## Health Versus Completeness

Do not treat a green pod as proof that the graph is complete.

| Signal | What it proves | What it does not prove |
| --- | --- | --- |
| `/healthz` | The process is alive. | Work is current. |
| `/readyz` | The runtime has enough dependencies to serve. | Queues are empty. |
| `/metrics` | Prometheus can scrape runtime and data-plane signals; `eshu_runtime_status_snapshot_available=0` means the status snapshot failed for that scrape while OTEL metrics remain available; with `ESHU_STATUS_SUMMARY_READ_ENABLED=true`, `eshu_runtime_status_summary_stale=1` means the scrape served a stale or empty active-work summary rather than the live statement. | The graph is correct. |
| `/admin/status` | Runtime backlog, generation, failure, and domain status; `eshu_dp_status_snapshot_read_duration_seconds` (`read`, `outcome`) times each status read, and `eshu_dp_status_summary_read_total` (`source`, `reason`) shows whether active-work counts came from the stored summary or live SQL ([Reducer And Storage Metrics](metrics-reducer-storage.md#status-summary-reader)). | The underlying source did not change after the last collection. |
| Query/API result | Current read-path answer. | The whole pipeline is healthy. |

Use `/admin/status` and queue metrics when the user-facing question is
freshness, convergence, or "why is this repo still not reflected?"

During provenance-edge identity upgrades, pair
`eshu_runtime_provenance_edge_identity_upgrade_applied` with
`eshu_runtime_provenance_edge_identity_upgrade_required`. An applied value of
`1` with a non-zero required count after reducer ACKs means migration 096 is
fencing an older reducer binary; roll the reducer forward rather than disabling
database triggers.

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

## Code Contract And Change Gate

Telemetry names are frozen in the Go package:

- metric instruments: `go/internal/telemetry/instruments.go`
- metric dimensions, span names, and log keys:
  `go/internal/telemetry/contract.go` plus companion `contract_*.go` files
- pipeline phases and logger helpers: `go/internal/telemetry/logging.go`
- package-level maintainer contract: `go/internal/telemetry/README.md`

For the machine-enforced enumeration of every pipeline stage and its required
metric or `No-Observability-Change:` marker, see
[Telemetry Coverage Contract](../../observability/telemetry-coverage.md).

Runtime-affecting pull requests (collectors, parsers, reducers, projectors,
graph writes, queues, workers, runtime settings, or API/MCP graph queries) must
record their performance/no-regression and observability evidence in the **PR
description**, not in ephemeral markdown files or appended sections of public
reference pages. When adding or changing a signal, update the Go telemetry
contract and the focused operator telemetry page that owns that signal.

## API And MCP Request Signals

Every query API and MCP read route emits two shared middleware metrics labeled
by matched `route` pattern and `status_class`:

- `eshu_dp_api_request_duration_seconds` — handler latency histogram (its
  `_count` series is the per-route request rate).
- `eshu_dp_api_request_errors_total` — server-error (`5xx`) counter.

Requests that match no route use `route="unmatched"`; the admin mux (probes and
`/metrics`) is excluded. Companion pages document specialized read signals:

- MCP response budgets, tool calls, and router calls:
  [MCP Response-Budget Telemetry](mcp-response-budget.md),
  [MCP Tool-Call Telemetry](mcp-tool-calls.md), and
  [MCP Router Telemetry](mcp-router.md).
- Relevance walks (`find_code`, `search_entity_content`):
  [Relevance Walk Telemetry](rank-cursor.md).
- Cloud resource paging: `eshu_dp_cloud_resource_list_duration_seconds`,
  `eshu_dp_cloud_resource_list_scanned_rows`,
  `eshu_dp_cloud_resource_list_page_size`,
  `eshu_dp_cloud_resource_list_truncations_total`, and
  `eshu_dp_cloud_resource_list_errors_total` (no resource, account, region, or
  tenant labels).
- Relationship catalog admission control: a shared four-permit limiter exposes
  `eshu_dp_relationship_breakdown_permit_wait_seconds`,
  `eshu_dp_relationship_breakdown_queued`, and
  `eshu_dp_relationship_breakdown_in_flight`. High wait with in-flight at 4
  means local admission backpressure rather than slow graph reads.
- Key route spans: `query.incident_context` (with `query.incident_context.stage`
  events), `supply_chain.cloud_runtime_probe` and
  `supply_chain.kubernetes_runtime_probe`, `query.semantic_evidence`,
  `query.language_query`, `query.call_graph.metrics` (including
  `scan_overflow=true` at the 50,001-edge guard), and
  `query.graph_entity_inventory`.

### Search Hybrid-Degradation Signal

`eshu_dp_search_hybrid_degraded_total` (`query_type`=`hybrid`|`semantic`,
`reason`=`no_embedder`|`index_unready`) increments when a request asked for
semantic ranking and fell back to BM25 or was refused. Plain `keyword` requests
and active hybrid/semantic responses do not increment it. Request spans carry
`search.retrieval_state`, `search.degraded`, `search.degraded_reason`, and
`search.index_cache` (`hit`, `miss`, `coalesced`, `bypass_unready`,
`retry_snapshot_changed`); persisted BM25 reads emit
`query.semantic_search.persisted_index`, and vector-backed modes emit
`query.semantic_search.vector_ready`.

Degraded search is expected in no-provider deployments where no embedder is
configured; treat an unexpected spike after enabling an embedder as the
actionable alarm.

## Collector, Parse, And Ingestion Signals

Every collector running under `ClaimedService.processClaimed` records three
chokepoint metrics with bounded `collector_kind` and `source_system` labels:

- `eshu_dp_workflow_claim_run_duration_seconds` (`outcome`: `success`,
  `refused`, `unchanged`, `released`, `fail_retryable`, `fail_terminal`) —
  wall time per claim.
- `eshu_dp_workflow_claim_facts_emitted_total` — committed facts per successful
  claim.
- `eshu_dp_workflow_fair_claims_total` (`claim_priority` 0–4) — granted claims;
  see [Claim-Pacing Signals](claim-pacing.md),
  [Claim Quotas](claim-quotas.md),
  [Fair-Claim Rotation Signals](fair-claim-rotation.md),
  [Workflow Claim Store Signals](workflow-claim-store.md), and
  [Claim-Plane Commit Fence Signals](claim-plane-commit-fence.md).

Additional ingestion signals:

- **Per-file parse and pre-scan cost:** `eshu_dp_file_parse_duration_seconds`
  and `eshu_dp_file_prescan_duration_seconds` (labeled by `language`), paired
  with `language_parse_summary` and `language_prescan_summary` logs; see
  [Ingestion And Collector Metrics](metrics-ingestion-collectors.md).
- **Content substring index finalization:** cold Compose bootstrap records
  `content_substring_index_state` (`not_built`, `building`, `ready`, `failed`),
  logs `failure_class=content_substring_index_build_failure` on failure, and
  times the phase in
  `eshu_dp_bootstrap_pipeline_phase_seconds{bootstrap_phase="content_index_finalization"}`.
  The secret-line side table has [finalizer signals](secret-lines.md).
- **Vulnerability intelligence:** NVD fetch duration carries `page_count`
  (0–100); OSV skips and CVE identity edges are documented in
  [Ingestion And Collector Metrics](metrics-ingestion-collectors.md).

## Reducer, Projection, And Liveness Signals

### Worker Pool, Acceptance, And Generation Liveness

- `eshu_dp_worker_pool_active{pool="reducer"}` — active concurrent reducer
  intents. Compare against `ESHU_REDUCER_WORKERS` and `eshu_dp_queue_depth`.
- `eshu_dp_shared_acceptance_rows` — approximate durable shared-projection
  acceptance row count from `pg_class.reltuples`.
- `eshu_dp_active_generations{age_bucket}` — active scope generations bucketed
  as `fresh`, `aging`, `draining`, or `stuck` (see
  [Reducer And Storage Metrics](metrics-reducer-storage.md#generation-liveness)).
  Pair `stuck` with `eshu_dp_generation_liveness_recovered_total`,
  `eshu_dp_generation_liveness_superseded_total`, and
  `eshu_dp_generation_liveness_failures_total`.
- `eshu_dp_poison_dead_letter_scopes`, `eshu_dp_poison_dead_letter_items`, and
  `eshu_dp_poison_dead_letter_oldest_age_seconds` — scopes stuck in
  `fact_work_items` `dead_letter` with no newer generation. Always active in
  surface-only mode; optional bounded auto-retry tracks recovery in
  `eshu_dp_poison_liveness_recovered_total` and
  `eshu_dp_poison_liveness_failures_total`.
- `eshu_dp_cross_repo_activation_fenced_total{scope_id}` — cross-repo
  generations withheld from activation because graph-acceptance intents failed
  to commit; pair with the `cross-repo activation fenced` warn log.

### Shared Projection, Graph Writes, And Backfill

- **Per-partition shared projection:**
  `eshu_dp_shared_projection_partition_processing_seconds` (`domain`,
  `partition_id`), `eshu_dp_shared_projection_intents_completed_total`
  (`domain`), and `eshu_dp_shared_projection_lease_quarantines_total`
  (`domain`, `reason`=`cycle_deadline`|`heartbeat_lost`|`cycle_error`).
- **Unroutable shared edges:** `eshu_dp_shared_edge_unroutable_rows_total`
  (`domain`, `reason`=`partial_batch`|`whole_batch`), paired with the
  `shared edge rows unroutable` warn log and durable
  `shared_projection_unroutable_intents` table.
- **Repo-dependency `RUNS_ON` retract omissions:**
  `eshu_dp_shared_edge_runs_on_retract_omissions_total` (`domain`,
  `reason`=`source_capability`) confirms skipped impossible `RUNS_ON` retracts
  for exact code-import refreshes.
- **Rationale retract probe outcomes:**
  `eshu_dp_rationale_retract_probe_outcomes_total` (`outcome`=`skipped`|`deleted`|`unsupported`|`probe_error`,
  `scope`=`whole_scope`|`delta_by_file_path`). Alert on sustained `unsupported`
  or `probe_error`, which means the existence guard is inert while graph truth
  stays intact.
- **Graph-write permit pools:** canonical and semantic reducer graph writes
  have separate permit pools (`ESHU_GRAPH_WRITE_CANONICAL_MAX_IN_FLIGHT`,
  `ESHU_GRAPH_WRITE_SEMANTIC_MAX_IN_FLIGHT`). When neither per-class variable
  is set, an outer `aggregate` gate preserves the legacy
  `ESHU_GRAPH_WRITE_MAX_IN_FLIGHT` combined ceiling. Both
  `eshu_dp_graph_write_backpressure_engaged_total` and
  `eshu_dp_graph_write_backpressure_wait_seconds` carry bounded `gate`
  (`canonical`, `semantic`, `aggregate`, `unknown`) and `operation` labels.
- **Lock-only gate contention:** `eshu_dp_lock_only_gate_locked_rows_total` and
  `eshu_dp_lock_only_gate_lock_wait_seconds` (label `family`:
  `rds_posture`, `ec2_internet_exposure`, `ec2_block_device_kms_posture`,
  `s3_internet_exposure`, `ec2_instance_identity`) track Postgres advisory-lock
  traffic and wait times alongside the slow-wait structured log.
- **Deferred backfill:** `eshu_dp_deferred_backfill_partition_load_duration_seconds`
  and `eshu_dp_deferred_backfill_partition_load_fact_count` (unattributed
  histograms; failed or canceled tasks record zero facts alongside duration)
  pair with the capped `partition_load_completed` span event (128-event OTEL
  default) and uncapped `deferred_backfill_fact_load_task_completed` log. The
  partition memo gate tracks hit rate via
  `eshu_dp_deferred_backfill_partitions_skipped_total` and
  `eshu_dp_deferred_backfill_partitions_loaded_total` (`reason`).

### Graph Integrity, Quarantine, And Identity Signals

- **Graph orphan nodes:** `eshu_dp_graph_orphan_nodes{node_label}` serves a
  background snapshot ([snapshots](graph-gauge-snapshots.md); id-anchor census
  in [Id-Anchor Census](id-anchor-census.md)) capped per label by
  `ESHU_GRAPH_ORPHAN_SWEEP_COUNT_LIMIT`. Every label (including `Module`,
  anchored by `(name, language)`) should drain across sweep cycles.
- **Extraction-provenance drift:** `eshu_dp_edges_by_source_tool{source_tool}`
  (out-of-vocabulary values coerce to `"unknown"`) and
  `eshu_dp_files_by_language{language}` serve exact background snapshots
  through `ProvenanceCountStore`. A series dropping to zero signals a parser or
  ingester stopped emitting that family.
- **Typed-payload decode quarantine & ownership contention:**
  `eshu_dp_reducer_input_invalid_facts_total{domain,fact_kind}` and
  `eshu_dp_projector_input_invalid_facts_total{stage,fact_kind}` count
  malformed facts skipped on decode (paired with `input_invalid fact
  quarantined` error logs carrying `fact_id` and `missing_field`).
  `eshu_dp_cross_scope_ownership_contended_rows_total{family}` counts
  owner-ledger rows resolved deterministically across overlapping scopes.
- **Container image identity retirement:**
  `eshu_dp_container_image_identity_retirements_total{outcome}` separates
  `retirement_attempted`, `legacy_deleted`, `held_config_blob_unavailable`,
  `held_tag_list_truncated`, and `held_missing_manifest_digest`. During rolling
  upgrades, `container_image_identity_cutovers` and
  `container_image_identity_scope_state` expose active digest-v3 authority.
- **AWS cloud-image edges:** `eshu_dp_aws_cloud_image_edges_total{resolution_mode}`
  counts materialized `CloudResource` -> `ContainerImage` edges whose target
  `:ContainerImage` node exists in the graph (tag-only ECS relationships stay
  in PostgreSQL).
