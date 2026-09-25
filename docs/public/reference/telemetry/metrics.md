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
- `eshu_runtime_provenance_edge_identity_upgrade_applied`
- `eshu_runtime_provenance_edge_identity_upgrade_required`

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
| `eshu_dp_queue_claim_conflict_retries_total` | Claim statements retried after a Postgres deadlock (`40P01`) or serialization failure (`40001`), by `queue` and `failure_class`. The projector claim takes every row lock with `SKIP LOCKED`, so a sustained nonzero rate is worth investigating as a possible new lock-order conflict (#7108). |
| `eshu_dp_worker_pool_active` | Active worker count by pool. |
| `eshu_dp_collector_observe_duration_seconds` | Collector observe cycle cost. |
| `eshu_dp_scope_assign_duration_seconds` | Repository or source scope assignment cost. |
| `eshu_dp_fact_emit_duration_seconds` | Fact emission cost. |
| `eshu_dp_facts_emitted_total` | Collector fact output volume. |
| `eshu_dp_facts_committed_total` | Durable fact commit volume. |
| `eshu_dp_generation_fact_count` | Fact volume per scope generation. |
| `eshu_dp_collector_delta_baseline_fallback_total` | Git delta syncs that fell back to a full snapshot, by `skip_reason`. |
| `eshu_dp_git_repo_sync_failures_total` | Per-repository git sync operations (`clone`, `fetch`, `list_refs`) that failed and were isolated to that one repository for the cycle rather than aborting the rest of the fleet, by bounded `operation` (issue #7001). A `list_refs` spike points at remote/DNS trouble for the affected repos; a rate near the fleet size points at an auth or network outage. |
| `eshu_dp_collector_reconciliation_full_snapshots_total` | Git scopes forced to a full reconciliation snapshot to retract delta-path drift. |
| `eshu_dp_reconciliation_drift_retractions_total` | Graph nodes and edges actually retracted while applying a forced reconciliation snapshot, by bounded `domain`, `write_phase`, and `kind`. |
| `eshu_dp_reconciliation_convergence_total` | Denormalized graph edges classified by the dual-write reconciliation pass, by bounded `domain` and `drift_kind` (`in_sync` / `stale_generation` / `orphan_resolved_id`). Non-`in_sync` values are stranded edges whose denormalized `generation_id`/`resolved_id` no longer match the authoritative Postgres generation after a swap; a sustained nonzero `stale_generation` or `orphan_resolved_id` rate means a Postgres↔graph partial failure left inconsistent edges that the pass is retracting to converge. |
| `eshu_dp_projector_run_duration_seconds` | Projector claim-and-project cycle cost. |
| `eshu_dp_projector_stage_duration_seconds` | Projector substage duration. |
| `eshu_dp_projector_ack_deferrals_total` | Projector Acks deferred because a same-scope ingestion commit held the scope row, by `outcome`: `retried`, `abandoned` (the 150-retry bound ran out), or `shutdown`. `retried` is counted before the lease renewal; a renewal that then ends the wait shows its result only in `eshu_dp_projector_ack_wait_seconds`. A rising `abandoned` rate means work is being dropped for re-projection after its lease expires. |
| `eshu_dp_component_producer_grant_decisions_total` | Producer-grant allow/deny decisions for core-owned fact kinds, by `decision` (`allow`/`deny`), `stage` (`install`/`readback`/`activation`/`emission`), closed `reason` (`granted` or a deny reason), and core `fact_kind`. Producer id is span/log only. See [Producer-Grant Decisions](producer-grant-decisions.md). |
| `eshu_dp_projector_ack_wait_seconds` | Time a deferred projector Ack waited for a busy scope, by terminal `outcome`: `succeeded`, `abandoned`, `shutdown`, `superseded`, `claim_lost`, or `failed`. Acks that never waited record nothing. |
| `eshu_dp_projections_completed_total` | Projection completion volume. |
| `eshu_dp_reducer_admission_deferrals_total` | Ingester source-local reducer intent admission deferrals by bounded reason. |
| `eshu_dp_reducer_readiness_waits_total` | Reducer cross-scope readiness-wait evaluations with missing endpoints, by bounded `domain` and `outcome` (`deferred` / `abandoned` / `settled_missing`). Ready edges commit before any `deferred`; `abandoned` fires once per settled missing set. |
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
| `eshu_dp_graph_orphan_nodes` | Bounded zero-relationship graph node count by closed `node_label`, served from a background snapshot. |
| `eshu_dp_gauge_snapshot_refreshes_total` | Background graph-gauge snapshot refreshes by `gauge` and `outcome` (`success`, `error`, `timeout`). |
| `eshu_dp_gauge_snapshot_refresh_duration_seconds` | Duration of each background graph-gauge snapshot refresh by `gauge` and `outcome`. |
| `eshu_dp_gauge_snapshot_age_seconds` | Age of the snapshot each graph-backed gauge is serving. |
| `eshu_dp_canonical_write_duration_seconds` | Canonical graph/content write latency. |
| `eshu_dp_search_decay_policy_applications_total` | Search decay scoring decisions by policy id, evidence class, and outcome. |
| `eshu_dp_query_scoped_grant_denied_total` | Scoped-caller query reads decided closed in Go rather than in a Cypher predicate (#6786), by bounded `operation` and `reason` (`grant_denied` = the Go grant check admitted no candidate, counted once per request; scoped name lookups filter by grant in Cypher first, so there it counts only rows the backend should have excluded; `backend_anchor_mismatch` = a returned row did not match the request, a graph-backend regression signal that should page). |

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
| Semantic extraction | `eshu_dp_queue_depth{queue="semantic_extraction"}`, `eshu_dp_queue_oldest_age_seconds{queue="semantic_extraction"}` |
| Shared follow-up | `eshu_dp_shared_projection_cycles_total`, `eshu_dp_shared_projection_intent_wait_seconds`, `eshu_dp_shared_projection_processing_seconds`, `eshu_dp_shared_projection_stale_intents_total`, `eshu_dp_shared_acceptance_lookup_duration_seconds` |
| Generation retention | `eshu_dp_generation_retention_generations_pruned_total`, `eshu_dp_generation_retention_rows_pruned_total`, `eshu_dp_generation_retention_failures_total`, `eshu_dp_generation_retention_skipped_total`, `eshu_dp_generation_retention_duration_seconds`, `eshu_dp_generation_retention_batch_size`, `eshu_dp_generation_retention_oldest_eligible_age_seconds` |
| Generation liveness | `eshu_dp_active_generations`, `eshu_dp_generation_liveness_recovered_total`, `eshu_dp_generation_liveness_superseded_total`, `eshu_dp_generation_liveness_failures_total` |
| Graph cleanup | `eshu_dp_graph_orphan_nodes`, `eshu_dp_neo4j_query_duration_seconds`, reducer logs with `failure_class=graph_orphan_sweep_error` |
| Storage pressure | `eshu_dp_postgres_query_duration_seconds`, `eshu_dp_neo4j_query_duration_seconds`, `eshu_dp_neo4j_deadlock_retries_total`, `eshu_dp_canonical_write_duration_seconds`, `eshu_dp_canonical_atomic_fallbacks_total`, `eshu_dp_graph_oversized_index_keys_skipped_total`, `eshu_dp_graph_index_key_guard_unanalyzed_total` |

When a metric points to one repo, scope, generation, or work item, move to
[logs](logs.md) and [traces](traces.md). Do not add high-cardinality labels to
make metrics carry the full debugging payload.
