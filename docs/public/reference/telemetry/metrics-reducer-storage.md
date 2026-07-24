# Reducer And Storage Metrics

This catalog covers reducer execution, shared follow-up, graph writes, storage,
correlation, supply-chain impact, capacity, and memory metrics.

## Reducer Execution

| Metric | Type | Use |
| --- | --- | --- |
| `eshu_dp_reducer_intents_enqueued_total` | counter | Reducer intent enqueue volume by domain. |
| `eshu_dp_reducer_admission_deferrals_total` | counter | Ingester source-local reducer intent enqueue deferrals while reducer backlog is at the configured high-water mark. |
| `eshu_dp_reducer_executions_total` | counter | Reducer execution volume by domain and status. |
| `eshu_dp_reducer_run_duration_seconds` | histogram | Handler execution window after a worker starts a work item. |
| `eshu_dp_reducer_queue_wait_seconds` | histogram | Time visible in the reducer queue before handler start. |
| `eshu_dp_reducer_batch_claim_size` | histogram | Batch claim size where batched reducer claiming is used. |
| `eshu_dp_reducer_heartbeat_missed_total` | counter | Reducer lease heartbeat failures by domain, including the immediate pre-heartbeat emitted at claim time. A non-zero rate means a worker's lease may be reclaimed and re-executed by another worker. |

Compare queue wait with run duration before changing worker counts. High queue
age with low run duration points to claim, routing, or conflict-domain pressure.
High run duration points to the handler, store, or graph-write path.

## Persisted Search Index

| Metric | Type | Use |
| --- | --- | --- |
| `eshu_dp_search_index_mutations_total` | counter | Document and term upsert/retire volume for the persisted semantic search index. |
| `eshu_dp_search_index_errors_total` | counter | Search index write failures by bounded operation. |
| `eshu_dp_search_index_write_duration_seconds` | histogram | Persisted search index write duration split by bounded operation and result. |

Search index metrics use bounded labels such as `domain`, `kind`, `operation`,
and `result`. Scope IDs, generation IDs, document IDs, paths, terms, and
provider-native identifiers stay in spans, structured logs, or durable facts.

## Generation Retention

| Metric | Type | Use |
| --- | --- | --- |
| `eshu_dp_generation_retention_generations_pruned_total` | counter | Superseded generations pruned by bounded retention cleanup. |
| `eshu_dp_generation_retention_rows_pruned_total` | counter | Rows pruned by bounded table/data-class label. |
| `eshu_dp_generation_retention_failures_total` | counter | Cleanup failures by bounded reason. |
| `eshu_dp_generation_retention_skipped_total` | counter | Candidate generations skipped by bounded reason such as `row_limit`. |
| `eshu_dp_generation_retention_duration_seconds` | histogram | Cleanup transaction duration. |
| `eshu_dp_generation_retention_batch_size` | histogram | Superseded generation count selected by one cleanup batch. |
| `eshu_dp_generation_retention_oldest_eligible_age_seconds` | histogram | Oldest selected superseded generation age in one batch. |

Retention metrics intentionally do not label raw scope IDs, generation IDs,
repository paths, source names, or provider identifiers. Use the retention event
table's safe hashes and structured logs for authorized drilldown.

## Generation Liveness

| Metric | Type | Use |
| --- | --- | --- |
| `eshu_dp_active_generations` | observable gauge | Current active scope generation count by closed activation-age bucket `age_bucket` (`fresh`, `aging`, `stuck`). |
| `eshu_dp_generation_liveness_recovered_total` | counter | Wedged active generations re-driven through projector re-enqueue by the liveness sweep. |
| `eshu_dp_generation_liveness_superseded_total` | counter | Orphaned older active generations superseded by the liveness sweep. |
| `eshu_dp_generation_liveness_failures_total` | counter | Generation liveness recovery sweep failures by bounded reason. |

The `eshu_dp_active_generations{age_bucket="stuck"}` series is the operator alarm
signal: a non-zero, non-draining `stuck` count means generations are activating
but not completing. Read it against the recovered and superseded counters to
separate self-healing from a backlog the sweep cannot clear; a rising
`eshu_dp_generation_liveness_failures_total` means the sweep itself is failing,
and the bounded failure reason lives in reducer logs.

## Graph Orphan Sweep

| Metric | Type | Use |
| --- | --- | --- |
| `eshu_dp_graph_orphan_nodes` | observable gauge | Current zero-relationship node count by closed `node_label`. |

The graph orphan sweep counts only the closed label set used by the reducer
cleanup path: `Repository`, `Platform`, and `EvidenceArtifact`. Counts are
capped by `ESHU_GRAPH_ORPHAN_SWEEP_COUNT_LIMIT` per label, so the gauge is a
dashboard signal, not an exact audit record. Sweep cycle logs carry per-label
counts, marks, deletes, duration, and `failure_class=graph_orphan_sweep_error`
without repository paths, resource identifiers, or generation ids.

## Shared Follow-Up And Acceptance

| Metric | Type | Use |
| --- | --- | --- |
| `eshu_dp_shared_projection_cycles_total` | counter | Shared projection partition cycles by domain and partition key. |
| `eshu_dp_shared_projection_intent_wait_seconds` | histogram | Maximum selected intent age for a partition cycle. |
| `eshu_dp_shared_projection_processing_seconds` | histogram | Graph-write and completion duration after partition selection. |
| `eshu_dp_shared_projection_step_seconds` | histogram | Shared projection substeps such as selection, load, retract, write, replay, and mark-completed. |
| `eshu_dp_shared_projection_stale_intents_total` | counter | Stale shared projection intents filtered during processing. |
| `eshu_dp_shared_projection_partition_heartbeat_missed_total` | counter | Shared projection partition lease heartbeat failures by domain. A non-zero rate means a slow partition cycle's lease may be reclaimed by another worker while the original holder is still processing. |
| `eshu_dp_shared_acceptance_lookup_duration_seconds` | histogram | Shared acceptance lookup latency. |
| `eshu_dp_shared_acceptance_lookup_errors_total` | counter | Shared acceptance lookup failures. |
| `eshu_dp_shared_acceptance_upsert_duration_seconds` | histogram | Shared acceptance write latency. |
| `eshu_dp_shared_acceptance_upserts_total` | counter | Shared acceptance write volume. |
| `eshu_dp_shared_acceptance_prefetch_size` | histogram | Shared acceptance prefetch size. |
| `eshu_dp_shared_acceptance_rows` | observable gauge | Durable shared acceptance row count. |
| `eshu_dp_shared_edge_write_groups_total` | counter | Shared edge write group volume. |
| `eshu_dp_shared_edge_write_group_duration_seconds` | histogram | Shared edge write group latency. |
| `eshu_dp_shared_edge_write_group_statement_count` | histogram | Statements per shared edge write group. |
| `eshu_dp_shared_edge_runs_on_retract_omissions_total` | counter | Impossible `RUNS_ON` retract roles omitted by bounded `domain` and `reason`; use the structured omission log for source and repository context. |
| `eshu_dp_code_call_edge_batches_total` | counter | Isolated code-call edge batch volume. |
| `eshu_dp_code_call_edge_batch_duration_seconds` | histogram | Isolated code-call edge batch latency. |

These metrics are domain-scoped. Use traces and logs when you need repository
or generation context.

## Storage And Graph Writes

| Metric | Type | Use |
| --- | --- | --- |
| `eshu_dp_postgres_query_duration_seconds` | histogram | Postgres query and exec latency from the instrumented wrapper. |
| `eshu_dp_neo4j_query_duration_seconds` | histogram | Neo4j/NornicDB Bolt query latency. Logical reads use `operation="read"` and bounded `outcome` values: `success`, `slow`, `recovered`, `deadline`, `caller_deadline`, `unavailable`, `canceled`, or `error`. |
| `eshu_dp_iac_resource_list_duration_seconds` | histogram | Bounded IaC resource list (`GET /api/v0/iac/resources`) handler latency, labeled by `iac.kind`. |
| `eshu_dp_iac_resource_list_errors_total` | counter | Bounded IaC resource list handler errors, labeled by `iac.kind` and `reason`. |
| `eshu_dp_neo4j_deadlock_retries_total` | counter | Legacy graph-write retry counter labeled by bounded `write_phase` and `reason` (`connectivity_error`, `transient_error`, `write_conflict`, or `commit_unique_conflict`) for deadlocks, lock timeouts, driver connectivity failures, and retryable NornicDB commit conflicts. Repository, node, statement, and raw error values stay out of labels. |
| `eshu_dp_neo4j_batch_size` | histogram | Grouped graph write batch size. |
| `eshu_dp_neo4j_batches_executed_total` | counter | Grouped graph write batch execution volume. |
| `eshu_dp_canonical_writes_total` | counter | Canonical graph write batch volume. |
| `eshu_dp_canonical_write_duration_seconds` | histogram | Canonical graph/content write latency. |
| `eshu_dp_canonical_atomic_writes_total` | counter | Atomic canonical write attempts. |
| `eshu_dp_canonical_atomic_fallbacks_total` | counter | Atomic write fallbacks. |
| `eshu_dp_canonical_nodes_written_total` | counter | Canonical node write volume. |
| `eshu_dp_canonical_edges_written_total` | counter | Canonical edge write volume. |
| `eshu_dp_canonical_projection_duration_seconds` | histogram | Canonical projection phase cost. |
| `eshu_dp_canonical_retract_duration_seconds` | histogram | Canonical retract phase cost. |
| `eshu_dp_canonical_batch_size` | histogram | Canonical write batch size. |
| `eshu_dp_canonical_phase_duration_seconds` | histogram | Canonical phase-level cost. |
| `eshu_dp_graph_write_backpressure_engaged_total` | counter | Graph writes that blocked for an in-flight permit (write-path backpressure engaged), labeled by operation and gate (`canonical` or `semantic`; the projector has a single pool and always reports `canonical`). |
| `eshu_dp_graph_write_backpressure_wait_seconds` | histogram | Time a graph write blocked waiting for an in-flight permit, labeled by operation and gate (`canonical` or `semantic`). |

Use graph/storage metrics before tuning NornicDB row caps, Neo4j batch sizes, or
worker counts. A non-zero `eshu_dp_graph_write_backpressure_engaged_total` rate
means the write path hit its concurrency ceiling and is slowing intake rather
than letting concurrent writes time out and flood the dead-letter queue; rising
`eshu_dp_graph_write_backpressure_wait_seconds` p95 is the precursor to write
timeouts. On the reducer, `ESHU_GRAPH_WRITE_CANONICAL_MAX_IN_FLIGHT` and
`ESHU_GRAPH_WRITE_SEMANTIC_MAX_IN_FLIGHT` (each falling back to
`ESHU_GRAPH_WRITE_MAX_IN_FLIGHT` when unset; issue #4448) size two independent
pools, so the two `gate` label values saturate independently — check both
before assuming the whole write path is bottlenecked.

## Correlation, Drift, And Relationship Work

| Metric | Type | Use |
| --- | --- | --- |
| `eshu_dp_correlation_rule_matches_total` | counter | Match-phase activity by rule pack and rule. |
| `eshu_dp_correlation_drift_detected_total` | counter | Admitted Terraform config/state drift by pack, rule, and drift kind. |
| `eshu_dp_correlation_drift_intents_enqueued_total` | counter | `config_state_drift` reducer intents emitted by bootstrap-index Phase 3.5. |
| `eshu_dp_correlation_orphan_detected_total` | counter | AWS runtime resources without Terraform-state backing. |
| `eshu_dp_correlation_unmanaged_detected_total` | counter | AWS/Terraform-state resources missing current Terraform config backing. |
| `eshu_dp_drift_unresolved_module_calls_total` | counter | Terraform module calls the drift loader could not resolve locally. |
| `eshu_dp_drift_schema_unknown_composite_total` | counter | State composite attributes dropped because provider schema coverage was missing or unsafe. |
| `eshu_dp_gcp_materialization_facts_total` | counter | GCP resource and relationship materialization input cardinality by reducer domain and fact kind. |
| `eshu_dp_gcp_materialization_graph_writes_total` | counter | GCP materialization graph-write cardinality by reducer domain and write kind (`node` or `edge`). |
| `eshu_dp_gcp_materialization_duration_seconds` | histogram | GCP materialization stage duration by reducer domain and write phase. |
| `eshu_dp_gcp_relationship_edges_total` | counter | GCP relationship edge outcomes by relationship type and join mode. |
| `eshu_dp_iac_reachability_rows_total` | counter | IaC usage rows materialized after projection drains. |
| `eshu_dp_iac_reachability_materialization_duration_seconds` | histogram | Corpus-wide IaC reachability materialization cost. |
| `eshu_dp_cross_repo_resolution_duration_seconds` | histogram | Cross-repo relationship resolution latency. |
| `eshu_dp_cross_repo_evidence_loaded_total` | counter | Evidence rows loaded for cross-repo resolution. |
| `eshu_dp_cross_repo_edges_resolved_total` | counter | Cross-repo edges resolved. |
| `eshu_dp_deferred_backfill_batch_duration_seconds` | histogram | Wall time of each per-repository batch transaction inside the deferred backward-evidence backfill. Watch batch-by-batch progress instead of waiting for the whole pass. |
| `eshu_dp_deferred_backfill_batches_completed_total` | counter | Committed per-repository batches in the deferred backward-evidence backfill. Rising during a pass is the operator-visible backfill progress signal. |
| `eshu_dp_evidence_facts_discovered_total` | counter | Evidence facts discovered during ingestion. |
| `eshu_dp_iam_can_perform_edges_total` | counter | IAM CAN_PERFORM edges committed by bounded resolution mode. |
| `eshu_dp_iam_can_perform_skipped_total` | counter | IAM CAN_PERFORM catalog-action evaluations withheld by bounded skip reason. |
| `eshu_dp_iam_can_perform_conditioned_total` | counter | Condition-gated IAM CAN_PERFORM evidence classified by bounded confidence. |
| `eshu_dp_incident_routing_evidence_total` | counter | PagerDuty incident-routing graph evidence outcomes by reducer domain, outcome, source class, and slot kind. |

No-Regression Evidence: #2409 adds nil-safe OTEL counter/histogram recording to
the existing GCP materialization handlers without changing queue claims, graph
writers, Cypher, worker counts, or terminal row counts. Baseline graph-write
shape stays `gcp_cloud_resource` 2 facts -> 1 node row and
`gcp_cloud_resource` 2 facts + `gcp_cloud_relationship` 2 facts -> 1 edge row;
after measurement is `go test ./internal/reducer -run
'TestImplementedDefaultDomainDefinitionsWiresGCP(Resource|Relationship)MaterializationInstruments|TestGCP(Resource|Relationship)MaterializationRecordsPrometheusSignals|TestGCPMaterialization(SkipsNoOpGraphWriteDurations|SignalsReachPrometheusExposition)|TestGCPRelationshipMaterializationMetricCarriesRelationshipTypeAndJoinMode'
-count=1` on the in-memory OTEL SDK manual reader and the same Prometheus
handler mounted by Compose runtimes, with NornicDB/Neo4j backend behavior
unchanged. Observability Evidence: the same test proves
`eshu_dp_gcp_materialization_facts_total`,
`eshu_dp_gcp_materialization_graph_writes_total`, and
`eshu_dp_gcp_materialization_duration_seconds` reach `/metrics` with bounded
`domain`, `fact_kind`, `kind`, and `write_phase` labels; no-op graph-write and
first-generation retract phases do not dilute duration histograms.
`eshu_dp_gcp_relationship_edges_total` now matches AWS edge telemetry by
emitting bounded `relationship_type` and `join_mode` labels. Completion logs
remain available for exact scope/generation diagnosis.

`eshu_dp_drift_unresolved_module_calls_total` uses the closed reasons
`external_registry`, `external_git`, `external_archive`, `cross_repo_local`,
`cycle_detected`, `depth_exceeded`, and `module_renamed`.

## Package, Image, CI/CD, And Supply Chain Correlation

| Metric | Type | Use |
| --- | --- | --- |
| `eshu_dp_package_source_correlations_total` | counter | Package source-correlation decisions by reducer domain and outcome. |
| `eshu_dp_package_consumption_repo_edges_total` | counter | Repo-to-repo `DEPENDS_ON` edge intents derived from package consumption-to-owner correlations, by reducer `domain` (`repo_dependency`) and `outcome` (`projected` for emitted edges, `skipped_no_owner` for consumers whose packages resolve no owner and instead emit a refresh/retraction). |
| `eshu_dp_code_import_repo_edges_total` | counter | Repo-to-repo `DEPENDS_ON` edge outcomes derived from per-file external import sources correlated to package-registry ownership (evidence_source `projection/code-imports`), by reducer `domain` (`repo_dependency`) and `outcome` (`considered`, `written`, and the conservative skip reasons `skipped_relative`, `skipped_unresolved`, `skipped_ambiguous`, `skipped_no_owner`, `skipped_self`). |
| `eshu_dp_container_image_identity_decisions_total` | counter | Container image identity decisions by reducer domain and outcome. |
| `eshu_dp_ci_cd_run_correlations_total` | counter | CI/CD run correlation decisions by reducer domain and outcome. |
| `eshu_dp_service_catalog_correlations_total` | counter | Service catalog correlation decisions by reducer domain and outcome. |
| `eshu_dp_service_catalog_correlation_guardrails_total` | counter | Service catalog correlation guardrail events by reducer domain and bounded guardrail. |
| `eshu_dp_search_decay_policy_applications_total` | counter | Search decay scoring decisions by policy id, evidence class, and outcome. |
| `eshu_dp_sbom_attestation_attachments_total` | counter | SBOM and attestation attachment decisions by reducer domain and outcome. |
| `eshu_dp_supply_chain_impact_findings_total` | counter | Supply-chain impact findings by reducer domain and outcome. |

Package names, image digests, run IDs, commit SHAs, environment names, and
artifact identifiers stay in logs, traces, or durable facts.

## Documentation Truth

| Metric | Type | Use |
| --- | --- | --- |
| `eshu_dp_documentation_entity_mentions_extracted_total` | counter | Documentation entity mention extraction by source system and outcome. |
| `eshu_dp_documentation_claim_candidates_extracted_total` | counter | Non-authoritative claim candidates after exact subject resolution. |
| `eshu_dp_documentation_claim_candidates_suppressed_total` | counter | Claim candidates intentionally suppressed before exact finding emission. |
| `eshu_dp_documentation_drift_findings_total` | counter | Read-only documentation drift findings by outcome. |
| `eshu_dp_documentation_drift_generation_duration_seconds` | histogram | Documentation drift finding generation latency. |

Ambiguous or unmatched mention outcomes usually point to catalog or alias
problems, not writer problems.

## Capacity And Memory

| Metric | Type | Use |
| --- | --- | --- |
| `eshu_dp_pipeline_overlap_seconds` | histogram | Overlap between major pipeline phases. |
| `eshu_dp_gomemlimit_bytes` | observable gauge | Effective Go memory limit exposed at startup. |

Use `eshu_dp_pipeline_overlap_seconds` when parallelism increases memory overlap
or storage contention. Use `eshu_dp_gomemlimit_bytes` with container RSS to
decide whether a service is undersized, over-concurrent, or missing cgroup-based
memory limit detection.
