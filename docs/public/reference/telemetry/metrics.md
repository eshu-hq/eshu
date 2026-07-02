# Telemetry Metrics

This page is the first-stop map for Eshu metrics. It keeps dashboard questions
short and links to focused catalogs for source-specific and reducer/storage
names.

Current metric sources:

- `go/internal/runtime/metrics.go` for `eshu_runtime_*` status metrics.
- `go/internal/telemetry/instruments.go` for most `eshu_dp_*` instruments.
- `go/internal/collector/terraformstate/metrics.go` for Terraform-state
  discovery candidate metrics.
- `go/internal/coordinator/metrics.go` for workflow-coordinator loop metrics.

## Reading Metrics

- Long-running runtimes expose `/metrics`.
- Runtime metrics use the `eshu_runtime_` prefix.
- Data-plane metrics use the `eshu_dp_` prefix.
- Prometheus resource labels include `service_name` and `service_namespace`;
  dashboards should filter by them.
- High-cardinality identifiers do not belong in labels. Use logs and traces for
  exact repository, scope, generation, locator, package, resource, delivery, or
  work-item detail.

## Runtime Health And Backlog

Use these first when asking whether a runtime is alive, ready, or stuck:

- `eshu_runtime_info`
- `eshu_runtime_health_state`
- `eshu_runtime_scope_active`
- `eshu_runtime_scope_changed`
- `eshu_runtime_scope_unchanged`
- `eshu_runtime_refresh_skipped_total`
- `eshu_runtime_retry_policy_max_attempts`
- `eshu_runtime_retry_policy_retry_delay_seconds`

Queue gauges:

- `eshu_runtime_queue_total`
- `eshu_runtime_queue_outstanding`
- `eshu_runtime_queue_pending`
- `eshu_runtime_queue_in_flight`
- `eshu_runtime_queue_retrying`
- `eshu_runtime_queue_succeeded`
- `eshu_runtime_queue_dead_letter`
- `eshu_runtime_queue_failed`
- `eshu_runtime_queue_overdue_claims`
- `eshu_runtime_queue_oldest_outstanding_age_seconds`

Stage, generation, and domain gauges:

- `eshu_runtime_generation_total`
- `eshu_runtime_stage_items`
- `eshu_runtime_domain_outstanding`
- `eshu_runtime_domain_retrying`
- `eshu_runtime_domain_dead_letter`
- `eshu_runtime_domain_failed`
- `eshu_runtime_domain_oldest_age_seconds`

Workflow-coordinator status exports add coordinator-specific
`eshu_runtime_coordinator_*` gauges for active claims, overdue claims, oldest
pending age, collector instances, run status, work-item status, and
completeness.

## Data-Plane Core

Use these to locate the phase that changed before opening logs or traces:

| Metric | Use |
| --- | --- |
| `eshu_dp_queue_depth` | Queue depth by queue and status. |
| `eshu_dp_queue_oldest_age_seconds` | Oldest queued item age by queue. |
| `eshu_dp_queue_source_depth` | Queue depth by queue, source system, and status. |
| `eshu_dp_queue_source_oldest_age_seconds` | Oldest queued item age by queue and source system. |
| `eshu_dp_queue_claim_duration_seconds` | Queue claim latency. |
| `eshu_dp_worker_pool_active` | Active worker count by pool. |
| `eshu_dp_collector_observe_duration_seconds` | Collector observe cycle cost. |
| `eshu_dp_scope_assign_duration_seconds` | Repository or source scope assignment cost. |
| `eshu_dp_fact_emit_duration_seconds` | Fact emission cost. |
| `eshu_dp_facts_emitted_total` | Collector fact output volume. |
| `eshu_dp_facts_committed_total` | Durable fact commit volume. |
| `eshu_dp_fact_batches_committed_total` | Streaming fact batch commits. |
| `eshu_dp_generation_fact_count` | Fact volume per scope generation. |
| `eshu_dp_collector_delta_baseline_fallback_total` | Git delta syncs that fell back to a full snapshot, by `skip_reason`. |
| `eshu_dp_collector_reconciliation_full_snapshots_total` | Git scopes forced to a full reconciliation snapshot to retract delta-path drift. |
| `eshu_dp_reconciliation_drift_retractions_total` | Graph nodes and edges actually retracted while applying a forced reconciliation snapshot, by bounded `domain`, `write_phase`, and `kind`. |
| `eshu_dp_reconciliation_convergence_total` | Denormalized graph edges classified by the dual-write reconciliation pass, by bounded `domain` and `drift_kind` (`in_sync` / `stale_generation` / `orphan_resolved_id`). Non-`in_sync` values are stranded edges whose denormalized `generation_id`/`resolved_id` no longer match the authoritative Postgres generation after a swap; a sustained nonzero `stale_generation` or `orphan_resolved_id` rate means a Postgres↔graph partial failure left inconsistent edges that the pass is retracting to converge. |
| `eshu_dp_projector_run_duration_seconds` | Projector claim-and-project cycle cost. |
| `eshu_dp_projector_stage_duration_seconds` | Projector substage duration. |
| `eshu_dp_projections_completed_total` | Projection completion volume. |
| `eshu_dp_reducer_admission_deferrals_total` | Ingester source-local reducer intent admission deferrals by bounded reason. |
| `eshu_dp_reducer_run_duration_seconds` | Reducer handler execution window. |
| `eshu_dp_search_index_mutations_total` | Persisted search index document and term mutations by bounded reducer domain, kind, operation, and result. |
| `eshu_dp_search_index_errors_total` | Persisted search index write failures by bounded reducer domain and operation. |
| `eshu_dp_search_index_write_duration_seconds` | Persisted search index write duration by bounded reducer domain, operation, and result. |
| `eshu_dp_generation_retention_generations_pruned_total` | Superseded generation cleanup volume. |
| `eshu_dp_generation_retention_rows_pruned_total` | Generation-retention row cleanup volume by bounded table/data-class label. |
| `eshu_dp_generation_retention_failures_total` | Generation-retention cleanup failures by bounded reason. |
| `eshu_dp_generation_retention_skipped_total` | Generation-retention candidate skips by bounded reason. |
| `eshu_dp_generation_retention_duration_seconds` | Generation-retention cleanup transaction cost. |
| `eshu_dp_generation_retention_batch_size` | Generation-retention batch size selected for one cleanup transaction. |
| `eshu_dp_active_generations` | Current active scope generation count by closed activation-age bucket `age_bucket` (`fresh`, `aging`, `stuck`); the `stuck` bucket is the operator alarm signal. |
| `eshu_dp_generation_liveness_recovered_total` | Wedged active generations re-driven through projector re-enqueue by the liveness sweep. |
| `eshu_dp_generation_liveness_superseded_total` | Orphaned older active generations superseded by the liveness sweep. |
| `eshu_dp_generation_liveness_failures_total` | Generation liveness recovery sweep failures by bounded reason. |
| `eshu_dp_graph_orphan_nodes` | Bounded zero-relationship graph node count by closed `node_label`. |
| `eshu_dp_canonical_write_duration_seconds` | Canonical graph/content write latency. |
| `eshu_dp_search_decay_policy_applications_total` | Search decay scoring decisions by policy id, evidence class, and outcome. |
| `eshu_dp_semantic_extraction_queue_events_total` | Semantic extraction queue lifecycle events by bounded source, provider, profile class, status, failure class, and budget class. |
| `eshu_dp_semantic_extraction_budget_tokens_total` | Semantic extraction estimated and actual token budget consumption by bounded source, provider, profile class, and budget class. |
| `eshu_dp_semantic_extraction_budget_cost_micros_total` | Semantic extraction estimated and actual cost budget consumption in micros by bounded source, provider, profile class, and budget class. |

`eshu_dp_projector_stage_duration_seconds` uses bounded `stage` values such as
`build_projection`, `graph_write`, `content_write`, and `intent_enqueue`.

## Focused Catalogs

- [Ingestion And Collector Metrics](metrics-ingestion-collectors.md) covers
  Git ingestion, discovery pruning, Terraform-state, OCI registry, Package
  Registry, AWS, Confluence, Grafana, Prometheus/Mimir, Loki, Tempo, workflow
  coordinator, and webhook intake.
- [Reducer And Storage Metrics](metrics-reducer-storage.md) covers reducer
  execution, shared follow-up, graph writes, storage, correlation, drift,
  supply-chain impact, capacity, and memory.

## Dashboard Starting Points

| Dashboard | Start with |
| --- | --- |
| Runtime health | `eshu_runtime_health_state`, `eshu_runtime_queue_outstanding`, `eshu_runtime_queue_oldest_outstanding_age_seconds`, `eshu_runtime_stage_items`, `eshu_runtime_domain_oldest_age_seconds` |
| Ingest throughput | `eshu_dp_repos_snapshotted_total`, `eshu_dp_files_parsed_total`, `eshu_dp_facts_emitted_total`, `eshu_dp_collector_observe_duration_seconds`, `eshu_dp_projector_run_duration_seconds`, `eshu_dp_reducer_run_duration_seconds` |
| Webhook intake | `eshu_dp_webhook_requests_total`, `eshu_dp_webhook_trigger_decisions_total`, `eshu_dp_webhook_store_operations_total`, `eshu_dp_webhook_request_duration_seconds`, `eshu_dp_webhook_store_duration_seconds` |
| Semantic extraction | `eshu_dp_queue_depth{queue="semantic_extraction"}`, `eshu_dp_queue_oldest_age_seconds{queue="semantic_extraction"}`, `eshu_dp_semantic_extraction_queue_events_total`, `eshu_dp_semantic_extraction_budget_tokens_total`, `eshu_dp_semantic_extraction_budget_cost_micros_total` |
| Shared follow-up | `eshu_dp_shared_projection_cycles_total`, `eshu_dp_shared_projection_intent_wait_seconds`, `eshu_dp_shared_projection_processing_seconds`, `eshu_dp_shared_projection_stale_intents_total`, `eshu_dp_shared_acceptance_lookup_duration_seconds` |
| Generation retention | `eshu_dp_generation_retention_generations_pruned_total`, `eshu_dp_generation_retention_rows_pruned_total`, `eshu_dp_generation_retention_failures_total`, `eshu_dp_generation_retention_skipped_total`, `eshu_dp_generation_retention_duration_seconds`, `eshu_dp_generation_retention_batch_size`, `eshu_dp_generation_retention_oldest_eligible_age_seconds` |
| Generation liveness | `eshu_dp_active_generations`, `eshu_dp_generation_liveness_recovered_total`, `eshu_dp_generation_liveness_superseded_total`, `eshu_dp_generation_liveness_failures_total` |
| Graph cleanup | `eshu_dp_graph_orphan_nodes`, `eshu_dp_neo4j_query_duration_seconds`, reducer logs with `failure_class=graph_orphan_sweep_error` |
| Storage pressure | `eshu_dp_postgres_query_duration_seconds`, `eshu_dp_neo4j_query_duration_seconds`, `eshu_dp_neo4j_deadlock_retries_total`, `eshu_dp_canonical_write_duration_seconds`, `eshu_dp_canonical_atomic_fallbacks_total` |

When a metric points to one repo, scope, generation, or work item, move to
[logs](logs.md) and [traces](traces.md). Do not add high-cardinality labels to
make metrics carry the full debugging payload.
