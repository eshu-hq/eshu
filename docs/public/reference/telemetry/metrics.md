# Telemetry Metrics

This page is the operator map for Eshu metrics. It keeps the first dashboard
questions short and links to focused catalogs for source-specific and
reducer/storage-specific names.

The code source of truth for Go data-plane metric names is
`go/internal/telemetry/instruments.go`. Runtime `eshu_runtime_*` gauges come
from the shared runtime status surface.

## Reading Metrics

- Runtime metrics come from the shared `/metrics` endpoint on long-running
  runtimes.
- Data-plane metrics use the `eshu_dp_` prefix.
- Prometheus resource labels include `service_name` and `service_namespace`.
  Dashboards should filter by those labels.
- High-cardinality identifiers do not belong in labels. Use logs and traces for
  exact repository, scope, generation, locator, package, resource, delivery, or
  work-item detail.

## Runtime Health And Backlog

Use these metrics for the first "is it stuck?" dashboard:

| Metric | Type | Use |
| --- | --- | --- |
| `eshu_runtime_info` | gauge | Confirms a runtime endpoint is scrapeable. |
| `eshu_runtime_health_state` | gauge | Shows one runtime verdict: `healthy`, `progressing`, `degraded`, or `stalled`. |
| `eshu_runtime_scope_active` | gauge | Shows active scope count for the runtime. |
| `eshu_runtime_scope_changed` | gauge | Shows scopes where incremental refresh found meaningful change. |
| `eshu_runtime_scope_unchanged` | gauge | Shows scopes where refresh avoided work. |
| `eshu_runtime_refresh_skipped_total` | counter | Counts refreshes skipped because no meaningful change was observed. |
| `eshu_runtime_retry_policy_max_attempts` | gauge | Exposes effective retry attempts by runtime/stage. |
| `eshu_runtime_retry_policy_retry_delay_seconds` | gauge | Exposes effective retry delay by runtime/stage. |

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

Stage and domain gauges:

- `eshu_runtime_stage_items`
- `eshu_runtime_generation_total`
- `eshu_runtime_domain_outstanding`
- `eshu_runtime_domain_retrying`
- `eshu_runtime_domain_dead_letter`
- `eshu_runtime_domain_failed`
- `eshu_runtime_domain_oldest_age_seconds`

Use runtime queue age before changing worker counts. A service can be healthy
while a domain is old, blocked, retrying, or dead-lettered.

## Data-Plane Core

Use these metrics to identify the pipeline phase that changed:

| Metric | Type | Use |
| --- | --- | --- |
| `eshu_dp_queue_depth` | gauge | Queue depth by queue and status. |
| `eshu_dp_queue_oldest_age_seconds` | gauge | Oldest queued item age by queue. |
| `eshu_dp_queue_claim_duration_seconds` | histogram | Queue claim latency. |
| `eshu_dp_worker_pool_active` | gauge | Active worker count by pool. |
| `eshu_dp_collector_observe_duration_seconds` | histogram | Collector observe cycle cost. |
| `eshu_dp_repo_snapshot_duration_seconds` | histogram | Per-repository snapshot cost. |
| `eshu_dp_file_parse_duration_seconds` | histogram | Per-file parse cost. |
| `eshu_dp_fact_emit_duration_seconds` | histogram | Fact emission cost. |
| `eshu_dp_facts_emitted_total` | counter | Facts emitted by collector. |
| `eshu_dp_facts_committed_total` | counter | Facts committed to the durable store. |
| `eshu_dp_fact_batches_committed_total` | counter | Streaming fact batch commits. |
| `eshu_dp_generation_fact_count` | histogram | Fact count per scope generation. |
| `eshu_dp_projector_run_duration_seconds` | histogram | Projector claim-and-project cycle cost. |
| `eshu_dp_projector_stage_duration_seconds` | histogram | Projector substage duration. |
| `eshu_dp_projections_completed_total` | counter | Projection completion volume. |

`eshu_dp_projector_stage_duration_seconds` uses bounded `stage` values such as
`build_projection`, `graph_write`, `content_write`, and `intent_enqueue`.

## Focused Catalogs

- [Ingestion And Collector Metrics](metrics-ingestion-collectors.md) covers
  Git ingestion, discovery pruning, Terraform-state, OCI registry, Package
  Registry, AWS, Confluence, and webhook intake.
- [Reducer And Storage Metrics](metrics-reducer-storage.md) covers reducer
  execution, shared follow-up, graph writes, storage, correlation, drift,
  supply-chain impact, capacity, and memory.

## Recommended Dashboards

Runtime health:

- `eshu_runtime_health_state`
- `eshu_runtime_queue_outstanding`
- `eshu_runtime_queue_oldest_outstanding_age_seconds`
- `eshu_runtime_stage_items`
- `eshu_runtime_domain_oldest_age_seconds`

Ingest throughput:

- `eshu_dp_repos_snapshotted_total`
- `eshu_dp_files_parsed_total`
- `eshu_dp_facts_emitted_total`
- `eshu_dp_collector_observe_duration_seconds`
- `eshu_dp_projector_run_duration_seconds`
- `eshu_dp_reducer_run_duration_seconds`

Webhook intake:

- `eshu_dp_webhook_requests_total`
- `eshu_dp_webhook_trigger_decisions_total`
- `eshu_dp_webhook_store_operations_total`
- `eshu_dp_webhook_request_duration_seconds`
- `eshu_dp_webhook_store_duration_seconds`

Shared follow-up:

- `eshu_dp_shared_projection_cycles_total`
- `eshu_dp_shared_projection_intent_wait_seconds`
- `eshu_dp_shared_projection_processing_seconds`
- `eshu_dp_shared_projection_stale_intents_total`
- `eshu_dp_shared_acceptance_lookup_duration_seconds`
- `eshu_dp_shared_acceptance_lookup_errors_total`
- `eshu_dp_shared_acceptance_upsert_duration_seconds`

Storage pressure:

- `eshu_dp_postgres_query_duration_seconds`
- `eshu_dp_neo4j_query_duration_seconds`
- `eshu_dp_neo4j_deadlock_retries_total`
- `eshu_dp_canonical_write_duration_seconds`
- `eshu_dp_canonical_atomic_fallbacks_total`

When a metric points to one repo, scope, generation, or work item, move to
[logs](logs.md) and [traces](traces.md). Do not add high-cardinality labels to
make metrics carry the full debugging payload.
