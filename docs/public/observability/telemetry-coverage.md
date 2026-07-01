# Telemetry Coverage Contract

This page enumerates every observable stage in the Eshu data plane and the
metric, span, or log key it must emit. It is the single source of truth that
the CI coverage script (X2) diffs against. A new pipeline stage added to the
source tree without a corresponding entry here fails the coverage gate. The
five evidence markers policy (`Performance Evidence:`, `Benchmark Evidence:`,
`No-Regression Evidence:`, `Observability Evidence:`, `No-Observability-Change:`)
at `docs/internal/agent-guide.md:120-146` remains the per-PR discipline; this
doc makes that discipline machine-enforced. Historical precedent lives in
[#3633](https://github.com/eshu-hq/eshu/issues/3633) (closed 2026-06-23),
which proved defined-but-never-registered instruments are a real failure
class; in-flight adoption is [#3680](https://github.com/eshu-hq/eshu/issues/3680)
(open, 2026-06-24), which lands per-collector envelope telemetry at the
shared claimed-service dispatch seam. Metric names match
`go/internal/telemetry/instruments.go`; dimensions, span names, and log keys
match `go/internal/telemetry/contract.go` and its `contract_*.go` siblings.
The public operator contract is `docs/public/reference/telemetry/index.md`.

## How To Read This Doc

- The table is machine-parseable: stable column order
  `stage | file:line | required metric name(s) | category`.
- One row per stage. The metric column lists every registered name an operator
  needs to read at 3 AM, comma-separated. The category column names the owning
  sub-package or seam.
- `file:line` points at the Go file that emits the signal, not the contract.
  When the source spans several call sites, it points at the dispatch chokepoint.
- The literal string `No-Observability-Change:` in a row's metric column is a
  documented decision that the stage does not need a new metric because an
  existing one already diagnoses it.
- The script (X2) grep-parses this table; do not rename columns or insert
  prose between rows in a section.

<!-- eshu:metric:section=reducer-stages -->
## Reducer Stages

The reducer drains queue work items through the worker pool, projects shared
edges, and writes the canonical graph. Each row maps one stage to the metric
or marker that already diagnoses it.

| stage | file:line | required metric name(s) | category |
| --- | --- | --- | --- |
| queue claim | go/internal/reducer/service.go:189 | `eshu_dp_queue_claim_duration_seconds`, `eshu_dp_reducer_queue_wait_seconds`, `eshu_dp_queue_depth`, `eshu_dp_worker_pool_active` | reducer runtime |
| intent enqueue | go/internal/projector/runtime.go:173 | `eshu_dp_reducer_intents_enqueued_total` | reducer runtime |
| batch claim | go/internal/reducer/repo_dependency_projection_runner.go:149 | `eshu_dp_reducer_batch_claim_size`, `eshu_dp_queue_claim_duration_seconds` | reducer runtime |
| fact load | go/internal/reducer/cross_repo_resolution.go:178 | `eshu_dp_postgres_query_duration_seconds`, `eshu_dp_cross_repo_resolution_duration_seconds`, `eshu_dp_cross_repo_evidence_loaded_total` | reducer fact load |
| candidate classification (admission deferral) | go/cmd/ingester/reducer_admission.go:363 | `eshu_dp_reducer_admission_deferrals_total` | reducer admission |
| candidate classification (CI/CD run correlation) | go/internal/reducer/ci_cd_run_correlation.go:160 | `eshu_dp_ci_cd_run_correlations_total` | reducer admission |
| candidate classification (cloud inventory) | go/internal/reducer/cloud_inventory_admission.go:413 | `eshu_dp_cloud_inventory_admissions_total` | reducer admission |
| candidate classification (container image identity) | go/internal/reducer/container_image_identity.go:191 | `eshu_dp_container_image_identity_decisions_total` | reducer admission |
| projection (cross-repo edges) | go/internal/reducer/cross_repo_resolution.go:270 | `eshu_dp_cross_repo_edges_resolved_total`, `eshu_dp_cross_repo_activation_fenced_total` | reducer cross-repo |
| projection (cross-repo intent rows) | go/internal/reducer/cross_repo_intent_row.go:15 | No-Observability-Change: covered by `eshu_dp_cross_repo_edges_resolved_total` and `eshu_dp_cross_repo_resolution_duration_seconds`; this file holds the in-process intent-row builders for the cross-repo edge projection above (extracted from cross_repo_resolution.go for the 500-line cap) and emits no metric of its own | reducer cross-repo |
| projection (repo-dependency activation gate) | go/internal/reducer/accepted_generation_active_gate.go:52 | `eshu_dp_repo_dependency_gate_decisions_total` | reducer cross-repo |
| projection (shared edges) | go/internal/reducer/shared_projection_runner.go | `eshu_dp_shared_projection_cycles_total`, `eshu_dp_shared_projection_intent_wait_seconds`, `eshu_dp_shared_projection_processing_seconds`, `eshu_dp_shared_projection_step_seconds`, `eshu_dp_shared_projection_partition_processing_seconds`, `eshu_dp_shared_projection_intents_completed_total` | reducer shared projection |
| projection (search index) | go/internal/reducer/eshu_search_index_writer.go | `eshu_dp_search_index_mutations_total`, `eshu_dp_search_index_errors_total`, `eshu_dp_search_index_write_duration_seconds` | reducer search index |
| projection (infrastructure platform) | go/internal/reducer/platform_infra_materialization.go | `No-Observability-Change: the platform_infra_materialization reducer domain runs as a standard reducer execution and graph write, covered by eshu_dp_reducer_executions_total, eshu_dp_reducer_run_duration_seconds, and eshu_dp_canonical_writes_total (PROVISIONS_PLATFORM edge count flows through Result.CanonicalWrites); the "platform infra materialization completed" structured log carries per-stage timings and platform_edges_written` | reducer platform |
| projection (inheritance diagnostics) | go/internal/reducer/inheritance_materialization_diagnostics.go | `No-Observability-Change: the inheritance_materialization reducer domain is covered by the reducer.inheritance_materialization span (eshu_dp_postgres_query_duration_seconds) plus eshu_dp_reducer_executions_total / eshu_dp_reducer_run_duration_seconds; this file holds the in-process fact-input counters (extracted from inheritance_materialization.go for the 500-line cap) that feed the "inheritance materialization fact inputs" structured log (content_entity_facts, entities_with_declared_parent) for rc-12 diagnosis and emits no metric of its own` | reducer inheritance |
| projection (code-call row extraction) | go/internal/reducer/code_call_materialization_extract.go | `No-Observability-Change: code-call edge row builders extracted from code_call_materialization.go for the 400-line cap (#3788); the code_call_materialization reducer domain runs as a standard reducer execution and graph write covered by eshu_dp_reducer_executions_total, eshu_dp_reducer_run_duration_seconds, eshu_dp_code_call_edge_batches_total, and eshu_dp_code_call_edge_batch_duration_seconds; this file builds in-process edge rows and emits no metric of its own` | reducer code call |
| projection (code-call helpers) | go/internal/reducer/code_call_materialization_path_helpers.go | `No-Observability-Change: code-call path/language/context helper functions extracted from code_call_materialization_helpers.go for the 400-line cap (#3788); pure in-process string helpers for the code_call_materialization domain covered by eshu_dp_reducer_executions_total / eshu_dp_reducer_run_duration_seconds / eshu_dp_code_call_edge_batches_total; this file emits no metric of its own` | reducer code call |
| projection (code-call import resolution) | go/internal/reducer/code_call_materialization_imports_resolve.go | `No-Observability-Change: imported-target resolution helpers extracted from code_call_materialization_imports.go for the 400-line cap (#3788); pure in-process resolution for the code_call_materialization domain covered by eshu_dp_reducer_executions_total / eshu_dp_reducer_run_duration_seconds / eshu_dp_code_call_edge_batches_total; this file emits no metric of its own` | reducer code call |
| projection (code-call index rows) | go/internal/reducer/code_call_materialization_index_rows.go | `No-Observability-Change: SCIP/generic code-call row extraction extracted from code_call_materialization_index.go for the 400-line cap (#3788); in-process row builders for the code_call_materialization domain covered by eshu_dp_reducer_executions_total, eshu_dp_reducer_run_duration_seconds, eshu_dp_code_call_edge_batches_total, and eshu_dp_code_call_edge_batch_duration_seconds; this file emits no metric of its own` | reducer code call |
| projection (additive domain wiring: correlation) | go/internal/reducer/defaults_additive_domains_correlation.go | `No-Observability-Change: additive reducer-domain registration wiring extracted from defaults_additive_domains.go for the 500-line cap (#3787); registers adapter-gated correlation domains that run as standard reducer executions covered by eshu_dp_reducer_executions_total and eshu_dp_reducer_run_duration_seconds, with each domain's own edge/node metrics documented in its projection row; this file emits no metric of its own` | reducer additive |
| projection (additive domain wiring: supply chain) | go/internal/reducer/defaults_additive_domains_supply_chain.go | `No-Observability-Change: additive reducer-domain registration wiring extracted from defaults_additive_domains.go for the 500-line cap (#3787); registers adapter-gated supply-chain/observability/kubernetes correlation domains that run as standard reducer executions covered by eshu_dp_reducer_executions_total and eshu_dp_reducer_run_duration_seconds; this file emits no metric of its own` | reducer additive |
| projection (additive domain wiring: secrets/drift) | go/internal/reducer/defaults_additive_domains_secrets_drift.go | `No-Observability-Change: additive reducer-domain registration wiring extracted from defaults_additive_domains.go for the 500-line cap (#3787); registers adapter-gated secrets-IAM and cloud-runtime-drift domains that run as standard reducer executions covered by eshu_dp_reducer_executions_total and eshu_dp_reducer_run_duration_seconds; this file emits no metric of its own` | reducer additive |
| projection (additive domain wiring: cloud nodes) | go/internal/reducer/defaults_additive_domains_cloud_nodes.go | `No-Observability-Change: additive reducer-domain registration wiring extracted from defaults_additive_domains.go for the 500-line cap (#3787); registers adapter-gated cloud-resource node domains that run as standard reducer executions covered by eshu_dp_reducer_executions_total and eshu_dp_reducer_run_duration_seconds; this file emits no metric of its own` | reducer additive |
| projection (additive domain wiring: cloud relationships) | go/internal/reducer/defaults_additive_domains_cloud_relationships.go | `No-Observability-Change: additive reducer-domain registration wiring extracted from defaults_additive_domains.go for the 500-line cap (#3787); registers adapter-gated cloud-relationship edge domains that run as standard reducer executions covered by eshu_dp_reducer_executions_total and eshu_dp_reducer_run_duration_seconds; this file emits no metric of its own` | reducer additive |
| projection (additive domain wiring: cloud posture) | go/internal/reducer/defaults_additive_domains_cloud_posture.go | `No-Observability-Change: additive reducer-domain registration wiring extracted from defaults_additive_domains.go for the 500-line cap (#3787); registers adapter-gated cloud-posture and IAM-action edge domains that run as standard reducer executions covered by eshu_dp_reducer_executions_total and eshu_dp_reducer_run_duration_seconds; this file emits no metric of its own` | reducer additive |
| projection (additive domain wiring: incident/code) | go/internal/reducer/defaults_additive_domains_incident_code.go | `No-Observability-Change: additive reducer-domain registration wiring extracted from defaults_additive_domains.go for the 500-line cap (#3787); registers adapter-gated incident-routing and code-evidence domains that run as standard reducer executions covered by eshu_dp_reducer_executions_total and eshu_dp_reducer_run_duration_seconds; this file emits no metric of its own` | reducer additive |
| projection (AWS relationship) | go/internal/reducer/aws_relationship_materialization.go | `eshu_dp_aws_relationship_edges_total`, `eshu_dp_postgres_query_duration_seconds` | reducer AWS |
| projection (GCP relationship) | go/internal/reducer/gcp_materialization_observability.go:22 | `eshu_dp_gcp_relationship_edges_total`, `eshu_dp_gcp_materialization_facts_total`, `eshu_dp_gcp_materialization_graph_writes_total`, `eshu_dp_gcp_materialization_duration_seconds` | reducer GCP |
| projection (Kubernetes correlation) | go/internal/reducer/kubernetes_correlation_materialization.go | `eshu_dp_kubernetes_correlations_total`, `eshu_dp_kubernetes_correlation_edges_total`, `eshu_dp_kubernetes_workload_nodes_total` | reducer Kubernetes |
| projection (Observability coverage) | go/internal/reducer/observability_coverage_materialization.go | `eshu_dp_observability_coverage_correlations_total`, `eshu_dp_observability_coverage_edges_total` | reducer observability coverage |
| projection (IAM CAN_ASSUME) | go/internal/reducer/iam_can_assume_materialization.go | `eshu_dp_iam_can_assume_edges_total` | reducer IAM |
| projection (IAM escalation) | go/internal/reducer/iam_escalation_materialization.go | `eshu_dp_iam_escalation_edges_total`, `eshu_dp_iam_escalation_skipped_total` | reducer IAM |
| projection (IAM CAN_PERFORM) | go/internal/reducer/iam_can_perform_materialization.go | `eshu_dp_iam_can_perform_edges_total`, `eshu_dp_iam_can_perform_skipped_total`, `eshu_dp_iam_can_perform_conditioned_total` | reducer IAM |
| projection (IAM instance-profile HAS_ROLE) | go/internal/reducer/iam_instance_profile_role_materialization.go | `eshu_dp_iam_instance_profile_role_edges_total`, `eshu_dp_iam_instance_profile_role_skipped_total` | reducer IAM |
| projection (EC2 USES_PROFILE) | go/internal/reducer/ec2_uses_profile_materialization.go | `eshu_dp_ec2_uses_profile_edges_total`, `eshu_dp_ec2_uses_profile_skipped_total` | reducer EC2 |
| projection (EC2 instance node) | go/internal/reducer/ec2_instance_node_materialization.go:184 | `eshu_dp_ec2_instance_nodes_total`, `eshu_dp_ec2_instance_nodes_skipped_total` | reducer EC2 |
| projection (S3 LOGS_TO) | go/internal/reducer/s3_logs_to_materialization.go | `eshu_dp_s3_logs_to_edges_total`, `eshu_dp_s3_logs_to_skipped_total` | reducer S3 |
| projection (security-group reachability) | go/internal/reducer/security_group_reachability_materialization.go | `eshu_dp_security_group_endpoint_nodes_total`, `eshu_dp_security_group_reachability_rule_nodes_total`, `eshu_dp_security_group_reachability_edges_total`, `eshu_dp_security_group_reachability_skipped_total` | reducer security group |
| projection (EC2/S3 internet exposure) | go/internal/reducer/ec2_internet_exposure_materialization.go | `eshu_dp_ec2_internet_exposure_decisions_total`, `eshu_dp_ec2_internet_exposure_skipped_total`, `eshu_dp_s3_internet_exposure_decisions_total`, `eshu_dp_s3_internet_exposure_skipped_total` | reducer posture |
| projection (EC2 block-device KMS posture) | go/internal/reducer/ec2_block_device_kms_posture_materialization.go | `eshu_dp_ec2_block_device_kms_posture_decisions_total`, `eshu_dp_ec2_block_device_kms_posture_skipped_total` | reducer posture |
| projection (config-state drift) | go/internal/reducer/terraform_config_state_drift.go:263 | `eshu_dp_correlation_drift_detected_total`, `eshu_dp_correlation_drift_intents_enqueued_total`, `eshu_dp_drift_unresolved_module_calls_total`, `eshu_dp_drift_schema_unknown_composite_total` | reducer drift |
| projection (AWS runtime drift) | go/internal/correlation/drift/cloudruntime/telemetry.go:88 | `eshu_dp_correlation_orphan_detected_total`, `eshu_dp_correlation_unmanaged_detected_total` | reducer AWS drift |
| projection (drift rule match) | go/internal/correlation/drift/multicloud/telemetry.go:67 | `eshu_dp_correlation_rule_matches_total` | reducer drift |
| projection (correlated incidents) | go/internal/reducer/incident_repository_correlation.go:272 | `eshu_dp_incident_repository_correlations_total` | reducer incident |
| projection (incident routing) | go/internal/reducer/incident_routing_materialization.go:194 | `eshu_dp_incident_routing_evidence_total` | reducer incident |
| projection (package source correlation) | go/internal/reducer/package_source_correlation_handler.go:289 | `eshu_dp_package_source_correlations_total`, `eshu_dp_package_consumption_repo_edges_total` | reducer package |
| projection (SBOM attestation) | go/internal/reducer/sbom_attestation_attachment.go:210 | `eshu_dp_sbom_attestation_attachments_total` | reducer SBOM |
| projection (secrets/IAM graph) | go/internal/reducer/secrets_iam_graph_projection.go:232 | `eshu_dp_secrets_iam_graph_edges_written_total`, `eshu_dp_secrets_iam_graph_skipped_total` | reducer secrets/IAM |
| projection (secrets/IAM posture) | go/internal/reducer/secrets_iam_trust_chain.go:223 | `eshu_dp_secrets_iam_reducer_trust_chains_total`, `eshu_dp_secrets_iam_posture_observations_total` | reducer secrets/IAM |
| projection (supply-chain) | go/internal/reducer/supply_chain_impact.go:316 | `eshu_dp_supply_chain_impact_findings_total`, `eshu_dp_supply_chain_suppression_decisions_total`, `eshu_dp_supply_chain_remediation_decisions_total` | reducer supply-chain |
| projection (documentation drift) | go/internal/doctruth/observability.go:15 | `eshu_dp_documentation_entity_mentions_extracted_total`, `eshu_dp_documentation_claim_candidates_extracted_total`, `eshu_dp_documentation_claim_candidates_suppressed_total`, `eshu_dp_documentation_drift_findings_total`, `eshu_dp_documentation_drift_generation_duration_seconds` | reducer docs |
| projection (IaC reachability) | go/internal/storage/postgres/iac_reachability_materializer.go:64 | `eshu_dp_iac_reachability_materialization_duration_seconds`, `eshu_dp_iac_reachability_rows_total` | reducer IaC |
| graph write (shared edge groups) | go/internal/storage/cypher/edge_writer.go | `eshu_dp_shared_edge_write_groups_total`, `eshu_dp_shared_edge_write_group_duration_seconds`, `eshu_dp_shared_edge_write_group_statement_count` | reducer graph write |
| graph write (code-call edges) | go/internal/storage/cypher/edge_writer.go | `eshu_dp_code_call_edge_batches_total`, `eshu_dp_code_call_edge_batch_duration_seconds` | reducer graph write |
| graph write (canonical atomic) | go/internal/storage/cypher/canonical_node_writer.go | `eshu_dp_canonical_atomic_writes_total`, `eshu_dp_canonical_atomic_fallbacks_total`, `eshu_dp_canonical_projection_duration_seconds`, `eshu_dp_canonical_phase_duration_seconds` | reducer graph write |
| graph write (NornicDB retry) | go/internal/storage/cypher/retrying_executor.go | `eshu_dp_neo4j_deadlock_retries_total`, `eshu_dp_neo4j_query_duration_seconds`, `eshu_dp_graph_write_backpressure_engaged_total`, `eshu_dp_graph_write_backpressure_wait_seconds` | reducer graph write |
| graph write (batch size) | go/internal/storage/cypher/edge_writer.go | `eshu_dp_neo4j_batch_size`, `eshu_dp_neo4j_batches_executed_total` | reducer graph write |
| canonical projection (nodes/edges) | go/internal/storage/cypher/canonical_node_writer_entities.go | `eshu_dp_canonical_nodes_written_total`, `eshu_dp_canonical_edges_written_total`, `eshu_dp_canonical_write_duration_seconds`, `eshu_dp_canonical_retract_duration_seconds`, `eshu_dp_canonical_batch_size` | reducer canonical |
| ack/retry | go/internal/collector/claimed_service_backpressure_metrics.go:41 | `eshu_dp_workflow_claim_retries_total`, `eshu_dp_workflow_claim_attempt_budget_exhausted_total`, `eshu_dp_workflow_claim_provider_throttle_total` | reducer queue |
| dead-letter | go/internal/collector/claimed_service_backpressure_metrics.go:60 | `eshu_dp_workflow_claim_provider_throttle_total` | reducer queue |
| generation retention | go/internal/reducer/generation_retention_runner.go:144 | `eshu_dp_generation_retention_generations_pruned_total`, `eshu_dp_generation_retention_rows_pruned_total`, `eshu_dp_generation_retention_failures_total`, `eshu_dp_generation_retention_skipped_total`, `eshu_dp_generation_retention_duration_seconds`, `eshu_dp_generation_retention_batch_size`, `eshu_dp_generation_retention_oldest_eligible_age_seconds` | reducer retention |
| generation liveness sweep | go/internal/reducer/generation_liveness_runner.go:157 | `eshu_dp_generation_liveness_recovered_total`, `eshu_dp_generation_liveness_superseded_total`, `eshu_dp_generation_liveness_failures_total`, `eshu_dp_active_generations` | reducer liveness |
| reducer run duration | go/internal/reducer/service.go:358 | `eshu_dp_reducer_run_duration_seconds`, `eshu_dp_reducer_executions_total` | reducer runtime |
| graph orphan sweep | go/internal/telemetry/instruments.go:3690 | `eshu_dp_graph_orphan_nodes` | reducer graph |
| extraction provenance — edges by source tool | go/internal/telemetry/instruments.go (RegisterEdgesBySourceToolObservableGauge) | `eshu_dp_edges_by_source_tool` | reducer graph |
| extraction provenance — files by language | go/internal/telemetry/instruments.go (RegisterFilesByLanguageObservableGauge) | `eshu_dp_files_by_language` | reducer graph |
| search decay scoring | go/internal/searchdecaytelemetry/observer.go:31 | `eshu_dp_search_decay_policy_applications_total` | reducer search |
| shared acceptance read model | go/internal/storage/postgres/code_call_intent_writer.go:37 | `eshu_dp_shared_acceptance_rows`, `eshu_dp_shared_acceptance_upserts_total`, `eshu_dp_shared_acceptance_lookup_errors_total`, `eshu_dp_shared_acceptance_upsert_duration_seconds`, `eshu_dp_shared_acceptance_lookup_duration_seconds`, `eshu_dp_shared_acceptance_prefetch_size` | reducer shared acceptance |

<!-- eshu:metric:section=projector-stages -->
## Projector Stages

The projector drains queue work items from `fact_work_items` (stage
`projector`) and writes source-local canonical graph nodes. It shares the
queue-depth and claim-wait surfaces with the reducer.

| stage | file:line | required metric name(s) | category |
| --- | --- | --- | --- |
| queue claim | go/internal/projector/service.go:115 | `eshu_dp_queue_claim_duration_seconds`, `eshu_dp_reducer_queue_wait_seconds`, `eshu_dp_queue_depth` | projector runtime |
| projector run | go/internal/projector/service_logging.go:41 | `eshu_dp_projector_run_duration_seconds`, `eshu_dp_projector_stage_duration_seconds`, `eshu_dp_canonical_writes_total` | projector runtime |
| projection completion | go/internal/projector/service_logging.go:44 | `eshu_dp_projections_completed_total` | projector runtime |
| fact commit | go/internal/collector/git_source_processing.go:217 | `eshu_dp_fact_emit_duration_seconds`, `eshu_dp_facts_emitted_total`, `eshu_dp_facts_committed_total`, `eshu_dp_fact_batches_committed_total`, `eshu_dp_generation_fact_count` | projector fact commit |
| content re-read | go/internal/telemetry/instruments.go:795 | `No-Observability-Change: eshu_dp_content_rereads_total and eshu_dp_content_reread_skips_total counters are registered but no longer emit; facts emitted/fact batches committed cover the path` | projector fact commit |
| content shape tables | go/internal/content/shape/materialize_tables.go | `No-Observability-Change: static bucket-to-label slice and label sets extracted verbatim from materialize.go (pure data, no telemetry seam); content-entity emission for this stage is covered by eshu_dp_content_entity_emitted_total` | projector fact commit |
| phase publish | go/internal/projector/service.go (publish_phases) | `eshu_dp_canonical_phase_duration_seconds`, `eshu_dp_deployment_mapping_reopened_total`, `eshu_dp_code_import_repo_edge_reopened_total`, `eshu_dp_correlation_reopened_total` | projector phase publish |
| ack/retry | go/internal/collector/claimed_service_backpressure_metrics.go:41 | `eshu_dp_workflow_claim_retries_total`, `eshu_dp_workflow_claim_attempt_budget_exhausted_total` | projector queue |
| dead-letter | go/internal/collector/git_selection_baseline.go:187 | `eshu_dp_collector_reconciliation_full_snapshots_total`, `eshu_dp_reconciliation_drift_retractions_total`, `eshu_dp_reconciliation_convergence_total` | projector dead-letter |
| deferred backfill | go/internal/storage/postgres/ingestion_backfill.go:103 | `eshu_dp_deferred_backfill_duration_seconds`, `eshu_dp_deferred_backfill_batch_duration_seconds`, `eshu_dp_deferred_backfill_batches_completed_total`, `eshu_dp_deferred_backfill_evidence_total` | projector backfill |
| deferred backfill partition fan-out | go/internal/storage/postgres/ingestion_backfill_scoped_load.go:178 | `eshu_dp_deferred_backfill_partitions_total`, `eshu_dp_deferred_backfill_partition_workers`, `eshu_dp_deferred_backfill_partition_load_duration_seconds` | projector backfill |

<!-- eshu:metric:section=collector-dispatch-seams -->
## Collector Dispatch Seams

Every collector family runs under the shared claimed-service worker harness
(`go/internal/collector/claimed_service.go`). One row per dispatch chokepoint
covers all collector families; per-collector volume counters and durations
land at the same call sites.

| stage | file:line | required metric name(s) | category |
| --- | --- | --- | --- |
| collector.observe (claim → complete) | go/internal/collector/service.go:393 | `eshu_dp_collector_observe_duration_seconds`, `eshu_dp_workflow_claim_wait_seconds` | collector chokepoint |
| collector.claimed_run (per-cycle outcome) | go/internal/collector/claimed_service_run_metrics.go:36 | `eshu_dp_workflow_claim_run_duration_seconds`, `eshu_dp_workflow_claim_facts_emitted_total` | collector chokepoint |
| collector.stream (streaming read) | go/internal/collector/git_source_stream.go:325 | `eshu_dp_collector_observe_duration_seconds`, `eshu_dp_facts_emitted_total` | collector chokepoint |
| bootstrap collector cycle | go/cmd/bootstrap-index/main.go (drainCollector) | `eshu_dp_bootstrap_pipeline_phase_seconds`, `eshu_dp_content_entity_emitted_total` | collector chokepoint |
| bootstrap pipeline overlap | go/cmd/bootstrap-index/main.go:420 | `eshu_dp_pipeline_overlap_seconds` | collector chokepoint |
| repo snapshot | go/internal/collector/git_source_processing.go:211 | `eshu_dp_repo_snapshot_duration_seconds`, `eshu_dp_repos_snapshotted_total`, `eshu_dp_files_parsed_total` | collector per-collector |
| snapshot stage timing | go/internal/collector/git_snapshot_native.go:330 | `eshu_dp_collector_snapshot_stage_duration_seconds` | collector per-collector |
| dataflow function fact mapping | go/internal/collector/git_snapshot_dataflow_function.go:31 | No-Observability-Change: covered by `eshu_dp_collector_snapshot_stage_duration_seconds`, `eshu_dp_facts_emitted_total`, and `eshu_dp_generation_fact_count`; this file maps already-parser-bounded `dataflow_functions` rows into one streamed fact per function and emits no metric of its own | collector per-collector |
| gcp typed-depth extractor registry | go/internal/collector/gcpcloud/extractor.go | No-Observability-Change: pure in-process per-asset-type extractor registry that emits no metric of its own; extraction outcomes are observed by the collector-local `eshu_dp_gcp_cloud_attribute_extractions_total` counter (recorded in `go/internal/collector/gcpcloud/gcpruntime/source.go`) and resource/edge emission by `eshu_dp_gcp_cloud_facts_emitted_total` | collector gcp |
| gcp bigquery table extractor | go/internal/collector/gcpcloud/extractor_bigquery_table.go | No-Observability-Change: pure in-process BigQuery Table typed-depth extractor that emits no metric of its own; covered by `eshu_dp_gcp_cloud_attribute_extractions_total` and `eshu_dp_gcp_cloud_facts_emitted_total` | collector gcp |
| gcp subnetwork extractor | go/internal/collector/gcpcloud/extractor_subnetwork.go | No-Observability-Change: pure in-process compute Subnetwork typed-depth extractor that emits no metric of its own; covered by `eshu_dp_gcp_cloud_attribute_extractions_total` and `eshu_dp_gcp_cloud_facts_emitted_total` | collector gcp |
| gcp vpc network extractor | go/internal/collector/gcpcloud/extractor_compute_network.go | No-Observability-Change: pure in-process VPC Network typed-depth extractor that emits no metric of its own; covered by `eshu_dp_gcp_cloud_attribute_extractions_total` and `eshu_dp_gcp_cloud_facts_emitted_total` | collector gcp |
| gcp iam service account extractor | go/internal/collector/gcpcloud/extractor_service_account.go | No-Observability-Change: pure in-process IAM Service Account typed-depth extractor that emits no metric of its own; covered by `eshu_dp_gcp_cloud_attribute_extractions_total` and `eshu_dp_gcp_cloud_facts_emitted_total` | collector gcp |
| gcp disk extractor | go/internal/collector/gcpcloud/extractor_disk.go | No-Observability-Change: pure in-process compute Persistent Disk typed-depth extractor that emits no metric of its own; covered by `eshu_dp_gcp_cloud_attribute_extractions_total` and `eshu_dp_gcp_cloud_facts_emitted_total` | collector gcp |
| gcp artifact registry docker image extractor | go/internal/collector/gcpcloud/extractor_artifact_registry_docker_image.go | No-Observability-Change: pure in-process Artifact Registry DockerImage typed-depth extractor that emits no metric of its own; covered by `eshu_dp_gcp_cloud_attribute_extractions_total` and `eshu_dp_gcp_cloud_facts_emitted_total` | collector gcp |
| gcp bigquery dataset extractor | go/internal/collector/gcpcloud/extractor_bigquery_dataset.go | No-Observability-Change: pure in-process BigQuery Dataset typed-depth extractor that emits no metric of its own; covered by `eshu_dp_gcp_cloud_attribute_extractions_total` and `eshu_dp_gcp_cloud_facts_emitted_total` | collector gcp |
| gcp secret manager secret extractor | go/internal/collector/gcpcloud/extractor_secret_manager_secret.go | No-Observability-Change: pure in-process Secret Manager Secret typed-depth extractor that emits no metric of its own; covered by `eshu_dp_gcp_cloud_attribute_extractions_total` and `eshu_dp_gcp_cloud_facts_emitted_total` | collector gcp |
| gcp firewall extractor | go/internal/collector/gcpcloud/extractor_firewall.go | No-Observability-Change: pure in-process compute Firewall typed-depth extractor that emits no metric of its own; covered by `eshu_dp_gcp_cloud_attribute_extractions_total` and `eshu_dp_gcp_cloud_facts_emitted_total` | collector gcp |
| delta baseline fallback | go/internal/collector/git_selection_baseline.go:175 | `eshu_dp_collector_delta_baseline_fallback_total` | collector per-collector |
| file parse | go/internal/collector/git_snapshot_native.go | `eshu_dp_file_parse_duration_seconds`, `eshu_dp_scip_snapshot_attempts_total`, `eshu_dp_scip_process_wait_seconds` | collector per-collector |
| scope assign | go/internal/collector/git_source_processing.go:35 | `eshu_dp_scope_assign_duration_seconds` | collector per-collector |
| discovery | go/internal/collector/git_selection_discovery.go:375 | `eshu_dp_discovery_dirs_skipped_total`, `eshu_dp_discovery_files_skipped_total`, `eshu_dp_repository_basename_collision_total` | collector discovery |
| large repo classification | go/internal/collector/git_source_stream.go:141 | `eshu_dp_large_repo_classifications_total`, `eshu_dp_large_repo_semaphore_wait_seconds` | collector scheduling |
| dedicated large-lane scheduler | go/internal/collector/git_source_scheduler.go | `No-Observability-Change: eshu_dp_large_repo_semaphore_wait_seconds and eshu_dp_large_repo_classifications_total (emitted via git_source_stream.go and git_source_scheduler.go) cover giant start order and concurrency; large repo semaphore acquired/released logs record per-giant wait and hold duration` | collector scheduling |
| Terraform state (collector family) | go/internal/collector/tfstateruntime/metrics.go:14-148 | `eshu_dp_tfstate_snapshots_observed_total`, `eshu_dp_tfstate_resources_emitted_total`, `eshu_dp_tfstate_outputs_emitted_total`, `eshu_dp_tfstate_modules_emitted_total`, `eshu_dp_tfstate_warnings_emitted_total`, `eshu_dp_tfstate_redactions_applied_total`, `eshu_dp_tfstate_s3_conditional_get_not_modified_total`, `eshu_dp_tfstate_discovery_candidates_total`, `eshu_dp_tfstate_snapshot_bytes`, `eshu_dp_tfstate_parse_duration_seconds`, `eshu_dp_tfstate_claim_wait_seconds`, `eshu_dp_tfstate_schema_resolver_entries` | collector Terraform state |
| OCI registry | go/internal/collector/ociregistry/*.go | `eshu_dp_oci_registry_api_calls_total`, `eshu_dp_oci_registry_tags_observed_total`, `eshu_dp_oci_registry_manifests_observed_total`, `eshu_dp_oci_registry_referrers_observed_total`, `eshu_dp_oci_registry_scan_duration_seconds` | collector OCI |
| Kubernetes live | go/internal/collector/kuberneteslive/*.go | `eshu_dp_kubernetes_api_calls_total`, `eshu_dp_kubernetes_resources_listed_total`, `eshu_dp_kubernetes_facts_emitted_total`, `eshu_dp_kubernetes_warnings_total`, `eshu_dp_kubernetes_list_duration_seconds` | collector Kubernetes |
| Secrets/IAM source | go/internal/collector/secretsiam/*.go | `eshu_dp_secrets_iam_source_api_calls_total`, `eshu_dp_secrets_iam_source_facts_emitted_total`, `eshu_dp_secrets_iam_partial_scope_total`, `eshu_dp_secrets_iam_source_redactions_total`, `eshu_dp_secrets_iam_source_scope_freshness_seconds` | collector secrets/IAM |
| Package registry | go/internal/collector/packageregistry/*.go | `eshu_dp_package_registry_requests_total`, `eshu_dp_package_registry_facts_emitted_total`, `eshu_dp_package_registry_rate_limited_total`, `eshu_dp_package_registry_parse_failures_total`, `eshu_dp_package_registry_observe_duration_seconds`, `eshu_dp_package_registry_generation_lag_seconds` | collector package registry |
| Vulnerability intelligence | go/internal/collector/vulnerabilityintelligence/*.go | `eshu_dp_vulnerability_intelligence_observations_total`, `eshu_dp_vulnerability_intelligence_facts_emitted_total`, `eshu_dp_vulnerability_intelligence_rate_limited_total`, `eshu_dp_vulnerability_intelligence_fetch_duration_seconds` | collector vulnerability |
| Security alerts | go/internal/collector/securityalerts/*.go | `eshu_dp_security_alert_provider_requests_total`, `eshu_dp_security_alert_facts_emitted_total`, `eshu_dp_security_alert_rate_limited_total`, `eshu_dp_security_alert_fetch_duration_seconds` | collector security alerts |
| CI/CD run | go/internal/collector/cicdrun/*.go | `eshu_dp_ci_cd_run_provider_requests_total`, `eshu_dp_ci_cd_run_facts_emitted_total`, `eshu_dp_ci_cd_run_rate_limited_total`, `eshu_dp_ci_cd_run_partial_generations_total`, `eshu_dp_ci_cd_run_fetch_duration_seconds` | collector CI/CD |
| PagerDuty | go/internal/collector/pagerduty/*.go | `eshu_dp_pagerduty_provider_requests_total`, `eshu_dp_pagerduty_facts_emitted_total`, `eshu_dp_pagerduty_rate_limited_total`, `eshu_dp_pagerduty_fetch_duration_seconds`, `eshu_dp_pagerduty_generation_lag_seconds`, `eshu_dp_pagerduty_config_resources_observed_total`, `eshu_dp_pagerduty_config_drift_candidates_total`, `eshu_dp_pagerduty_config_partial_failures_total`, `eshu_dp_pagerduty_config_redactions_total` | collector PagerDuty |
| Jira | go/internal/collector/jira/*.go | `eshu_dp_jira_provider_requests_total`, `eshu_dp_jira_facts_emitted_total`, `eshu_dp_jira_rate_limited_total`, `eshu_dp_jira_fetch_duration_seconds` | collector Jira |
| Grafana | go/internal/collector/grafana/*.go | `eshu_dp_grafana_provider_requests_total`, `eshu_dp_grafana_facts_emitted_total`, `eshu_dp_grafana_rate_limited_total`, `eshu_dp_grafana_retries_total`, `eshu_dp_grafana_redactions_total`, `eshu_dp_grafana_fetch_duration_seconds` | collector Grafana |
| Prometheus/Mimir | go/internal/collector/prometheusmimir/*.go | `eshu_dp_prometheus_mimir_provider_requests_total`, `eshu_dp_prometheus_mimir_facts_emitted_total`, `eshu_dp_prometheus_mimir_rate_limited_total`, `eshu_dp_prometheus_mimir_retries_total`, `eshu_dp_prometheus_mimir_redactions_total`, `eshu_dp_prometheus_mimir_stale_total`, `eshu_dp_prometheus_mimir_fetch_duration_seconds` | collector Prometheus/Mimir |
| Loki | go/internal/collector/loki/*.go | `eshu_dp_loki_provider_requests_total`, `eshu_dp_loki_facts_emitted_total`, `eshu_dp_loki_rate_limited_total`, `eshu_dp_loki_retries_total`, `eshu_dp_loki_redactions_total`, `eshu_dp_loki_high_cardinality_rejected_total`, `eshu_dp_loki_stale_total`, `eshu_dp_loki_fetch_duration_seconds` | collector Loki |
| Tempo | go/internal/collector/tempo/*.go | `eshu_dp_tempo_provider_requests_total`, `eshu_dp_tempo_facts_emitted_total`, `eshu_dp_tempo_rate_limited_total`, `eshu_dp_tempo_retries_total`, `eshu_dp_tempo_redactions_total`, `eshu_dp_tempo_high_cardinality_rejected_total`, `eshu_dp_tempo_stale_total`, `eshu_dp_tempo_fetch_duration_seconds` | collector Tempo |
| Scanner worker | go/internal/collector/scannerworker/service_telemetry.go:18 | `eshu_dp_scanner_worker_claims_total`, `eshu_dp_scanner_worker_retries_total`, `eshu_dp_scanner_worker_dead_letters_total`, `eshu_dp_scanner_worker_facts_emitted_total`, `eshu_dp_scanner_worker_queue_wait_seconds`, `eshu_dp_scanner_worker_scan_duration_seconds`, `eshu_dp_scanner_worker_target_count`, `eshu_dp_scanner_worker_result_count`, `eshu_dp_scanner_worker_cpu_seconds`, `eshu_dp_scanner_worker_memory_bytes` | collector scanner worker |
| Service catalog | go/internal/reducer/service_catalog_correlation_guardrails.go:134 | `eshu_dp_service_catalog_correlations_total`, `eshu_dp_service_catalog_correlation_guardrails_total` | collector service catalog |
| Confluence | go/internal/collector/confluence/*.go | `eshu_dp_confluence_http_requests_total`, `eshu_dp_confluence_permission_denied_pages_total`, `eshu_dp_confluence_documents_observed_total`, `eshu_dp_confluence_sections_emitted_total`, `eshu_dp_confluence_links_emitted_total`, `eshu_dp_confluence_sync_failures_total`, `eshu_dp_confluence_fetch_duration_seconds` | collector Confluence |
| AWS cloud | go/internal/collector/awscloud/awsruntime/source.go:177 | `eshu_dp_aws_api_calls_total`, `eshu_dp_aws_throttle_total`, `eshu_dp_aws_assumerole_failed_total`, `eshu_dp_aws_budget_exhausted_total`, `eshu_dp_aws_pagination_checkpoint_events_total`, `eshu_dp_aws_resources_emitted_total`, `eshu_dp_aws_relationships_emitted_total`, `eshu_dp_aws_tag_observations_emitted_total`, `eshu_dp_aws_freshness_events_total`, `eshu_dp_aws_org_access_skipped_total`, `eshu_dp_aws_scan_status_stale_fence_total`, `eshu_dp_aws_scan_duration_seconds`, `eshu_dp_aws_claim_concurrency` | collector AWS cloud |
| Cassette replay source | go/internal/replay/cassette/source.go | `No-Observability-Change: collector.Service poll-loop metrics (eshu_dp_facts_emitted_total, eshu_dp_fact_batches_committed_total) cover cassette replay throughput; the cassette Source adds no new instruments` | replay cassette |
| Cassette replay format | go/internal/replay/cassette/format.go | `No-Observability-Change: cassette file loading is a startup-path read; no runtime metrics needed` | replay cassette |
| Cassette replay README | go/internal/replay/cassette/README.md | `No-Observability-Change: documentation file, no runtime stage` | replay cassette |
| Cassette replay AGENTS | go/internal/replay/cassette/AGENTS.md | `No-Observability-Change: agent instructions file, no runtime stage` | replay cassette |
| Documentation export | go/internal/collector/documentationexport/*.go | `No-Observability-Change: git-source collector metrics (FactsEmitted, RepoSnapshotDuration, CollectorSnapshotStageDuration) cover documentation export throughput` | collector documentation |
| Diagram preflight | go/internal/collector/diagrampreflight/*.go | `No-Observability-Change: git-source collector metrics cover diagram preflight throughput` | collector preflight |
| Archive preflight | go/internal/collector/archivepreflight/*.go | `No-Observability-Change: git-source collector metrics cover archive preflight throughput` | collector preflight |
| Export manifest preflight | go/internal/collector/exportmanifestpreflight/*.go | `No-Observability-Change: git-source collector metrics cover export-manifest preflight throughput` | collector preflight |
| Image preflight | go/internal/collector/imagepreflight/*.go | `No-Observability-Change: git-source collector metrics cover image preflight throughput` | collector preflight |
| Media preflight | go/internal/collector/mediapreflight/*.go | `No-Observability-Change: git-source collector metrics cover media preflight throughput` | collector preflight |
| PDF preflight | go/internal/collector/pdfpreflight/*.go | `No-Observability-Change: git-source collector metrics cover PDF preflight throughput` | collector preflight |
| OOXML preflight | go/internal/collector/ooxmlpreflight/*.go | `No-Observability-Change: git-source collector metrics cover OOXML preflight throughput` | collector preflight |
| Extension host | go/internal/collector/extensionhost/*.go | `No-Observability-Change: collector.claimed_run metrics (WorkflowClaimRunDuration, WorkflowClaimFactsEmitted) cover extension-host runs` | collector extension host |
| Contract test (data) | go/internal/collector/contracttest/contract_data.go | `No-Observability-Change: generated contract data file with no runtime execution path; the Contract Source of Truth CI gate covers staleness detection` | collector contracttest |
| Contract test (generator) | go/internal/collector/contracttest/gen/main.go | `No-Observability-Change: build-time code generator invoked via scripts/generate-contracttest.sh; no runtime metrics needed` | collector contracttest |

<!-- eshu:metric:section=parser-language-sub-packages -->
## Parser Language Sub-Packages

Every parser emits per-file facts through the shared `FactEmitDuration` and
`FileParseDuration` surfaces at the parser entry point
(`go/internal/collector/git_snapshot_native.go`). Per-language structured
events land through the `evidence_facts_discovered_total` counter when the
parser emits evidence facts.

| stage | file:line | required metric name(s) | category |
| --- | --- | --- | --- |
| parser entry (all languages) | go/internal/collector/git_snapshot_parse_partitions.go:237 | `eshu_dp_file_parse_duration_seconds`, `eshu_dp_files_parsed_total`, `eshu_dp_fact_emit_duration_seconds`, `eshu_dp_facts_emitted_total` | parser chokepoint |
| evidence discovery | go/internal/parser/evidence_discovery.go | `eshu_dp_evidence_facts_discovered_total` | parser evidence |
| Go parser | go/internal/parser/golang/*.go | `eshu_dp_file_parse_duration_seconds`, `eshu_dp_facts_emitted_total` | parser language |
| JavaScript parser | go/internal/parser/javascript/*.go | `eshu_dp_file_parse_duration_seconds`, `eshu_dp_facts_emitted_total` | parser language |
| TypeScript JSX parser | go/internal/parser/javascript/*.go (TSX branch) | `eshu_dp_file_parse_duration_seconds`, `eshu_dp_facts_emitted_total` | parser language |
| Python parser | go/internal/parser/python/*.go | `eshu_dp_file_parse_duration_seconds`, `eshu_dp_facts_emitted_total` | parser language |
| Java parser | go/internal/parser/java/*.go | `eshu_dp_file_parse_duration_seconds`, `eshu_dp_facts_emitted_total` | parser language |
| Kotlin parser | go/internal/parser/kotlin/*.go | `eshu_dp_file_parse_duration_seconds`, `eshu_dp_facts_emitted_total` | parser language |
| Scala parser | go/internal/parser/scala/*.go | `eshu_dp_file_parse_duration_seconds`, `eshu_dp_facts_emitted_total` | parser language |
| Groovy parser | go/internal/parser/groovy/*.go | `eshu_dp_file_parse_duration_seconds`, `eshu_dp_facts_emitted_total` | parser language |
| C# parser | go/internal/parser/csharp/*.go | `eshu_dp_file_parse_duration_seconds`, `eshu_dp_facts_emitted_total` | parser language |
| C parser | go/internal/parser/c/*.go | `eshu_dp_file_parse_duration_seconds`, `eshu_dp_facts_emitted_total` | parser language |
| C++ parser | go/internal/parser/cpp/*.go | `eshu_dp_file_parse_duration_seconds`, `eshu_dp_facts_emitted_total` | parser language |
| Rust parser | go/internal/parser/rust/*.go | `eshu_dp_file_parse_duration_seconds`, `eshu_dp_facts_emitted_total` | parser language |
| Ruby parser | go/internal/parser/ruby/*.go | `eshu_dp_file_parse_duration_seconds`, `eshu_dp_facts_emitted_total` | parser language |
| PHP parser | go/internal/parser/php/*.go | `eshu_dp_file_parse_duration_seconds`, `eshu_dp_facts_emitted_total` | parser language |
| Perl parser | go/internal/parser/perl/*.go | `eshu_dp_file_parse_duration_seconds`, `eshu_dp_facts_emitted_total` | parser language |
| Elixir parser | go/internal/parser/elixir/*.go | `eshu_dp_file_parse_duration_seconds`, `eshu_dp_facts_emitted_total` | parser language |
| Haskell parser | go/internal/parser/haskell/*.go | `eshu_dp_file_parse_duration_seconds`, `eshu_dp_facts_emitted_total` | parser language |
| Dart parser | go/internal/parser/dart/*.go | `eshu_dp_file_parse_duration_seconds`, `eshu_dp_facts_emitted_total` | parser language |
| Swift parser | go/internal/parser/swift/*.go | `eshu_dp_file_parse_duration_seconds`, `eshu_dp_facts_emitted_total` | parser language |
| SQL parser | go/internal/parser/sql/*.go | `eshu_dp_file_parse_duration_seconds`, `eshu_dp_facts_emitted_total` | parser language |
| dbt SQL parser | go/internal/parser/dbtsql/*.go | `eshu_dp_file_parse_duration_seconds`, `eshu_dp_facts_emitted_total` | parser language |
| Maven parser | go/internal/parser/maven/*.go | `eshu_dp_file_parse_duration_seconds`, `eshu_dp_facts_emitted_total` | parser language |
| Gradle parser | go/internal/parser/gradle/*.go | `eshu_dp_file_parse_duration_seconds`, `eshu_dp_facts_emitted_total` | parser language |
| HCL (Terraform) parser | go/internal/parser/hcl/*.go | `eshu_dp_file_parse_duration_seconds`, `eshu_dp_facts_emitted_total` | parser language |
| CloudFormation parser | go/internal/parser/cloudformation/*.go | `eshu_dp_file_parse_duration_seconds`, `eshu_dp_facts_emitted_total` | parser language |
| Dockerfile parser | go/internal/parser/dockerfile/*.go | `eshu_dp_file_parse_duration_seconds`, `eshu_dp_facts_emitted_total` | parser language |
| JSON parser | go/internal/parser/json/*.go | `eshu_dp_file_parse_duration_seconds`, `eshu_dp_facts_emitted_total` | parser language |
| YAML parser | go/internal/parser/yaml/*.go | `eshu_dp_file_parse_duration_seconds`, `eshu_dp_facts_emitted_total` | parser language |
| Go module lockfile parser | go/internal/parser/gomod/*.go | `eshu_dp_file_parse_duration_seconds`, `eshu_dp_facts_emitted_total` | parser language |
| Node lockfile parser | go/internal/parser/nodelockfile/*.go | `eshu_dp_file_parse_duration_seconds`, `eshu_dp_facts_emitted_total` | parser language |
| Python dependency parser | go/internal/parser/pythondep/*.go | `eshu_dp_file_parse_duration_seconds`, `eshu_dp_facts_emitted_total` | parser language |
| interproc evidence | go/internal/parser/interproc/*.go | `eshu_dp_file_parse_duration_seconds`, `eshu_dp_facts_emitted_total` | parser interproc |
| taint evidence | go/internal/parser/taint/*.go | `eshu_dp_file_parse_duration_seconds`, `eshu_dp_facts_emitted_total` | parser taint |
| value flow | go/internal/parser/valueflow/*.go | `eshu_dp_file_parse_duration_seconds`, `eshu_dp_facts_emitted_total` | parser value flow |
| shared helpers | go/internal/parser/shared/*.go | `No-Observability-Change: parser entry metrics cover shared helper throughput` | parser shared |
| summary parser | go/internal/parser/summary/*.go | `No-Observability-Change: parser entry metrics cover summary throughput` | parser summary |
| data flow emit | go/internal/parser/dataflowemit/*.go | `No-Observability-Change: parser entry metrics cover dataflow emit throughput` | parser data flow |
| golden audit | go/internal/parser/goldenaudit/*.go | `No-Observability-Change: parser entry metrics cover golden audit throughput` | parser audit |
| CFG (control flow graph) | go/internal/parser/cfg/*.go | `No-Observability-Change: parser entry metrics cover CFG throughput` | parser CFG |

<!-- eshu:metric:section=queue-domains -->
## Queue Domains

The Postgres `fact_work_items` table holds two stages (`projector`, `reducer`);
a separate `semantic_extraction_jobs` table holds provider semantic jobs.
Each domain has a depth/age gauge pair sourced from the queue observer.

| stage | file:line | required metric name(s) | category |
| --- | --- | --- | --- |
| reducer queue | go/internal/storage/postgres/queue_observer.go:112 | `eshu_dp_queue_depth`, `eshu_dp_queue_oldest_age_seconds`, `eshu_dp_queue_source_depth`, `eshu_dp_queue_source_oldest_age_seconds`, `eshu_dp_workflow_family_queue_depth` | queue domain |
| projector queue | go/internal/storage/postgres/queue_observer.go:112 (stage=projector) | `eshu_dp_queue_depth`, `eshu_dp_queue_oldest_age_seconds`, `eshu_dp_queue_source_depth`, `eshu_dp_queue_source_oldest_age_seconds`, `eshu_dp_workflow_family_queue_depth` | queue domain |
| semantic extraction queue | go/internal/storage/postgres/queue_observer.go:57 (semanticQueueDepthQuery) | `eshu_dp_semantic_extraction_queue_events_total`, `eshu_dp_semantic_extraction_budget_tokens_total`, `eshu_dp_semantic_extraction_budget_cost_micros_total` | queue domain |
| worker pool active | go/internal/telemetry/instruments.go:3617 | `eshu_dp_worker_pool_active` | queue runtime |
| workflow claim lease age | go/internal/collector/claimed_service_backpressure_metrics.go:78 | `eshu_dp_workflow_claim_lease_age_seconds` | queue runtime |
| go runtime memory limit | go/cmd/ingester/main.go:61 | `eshu_dp_gomemlimit_bytes` | queue runtime |
| reducer graph-write-timeout retrying | go/internal/storage/postgres/queue_observer.go:168 | `eshu_dp_queue_depth`, `eshu_dp_graph_write_backpressure_engaged_total` | queue runtime |

<!-- eshu:metric:section=api-lifecycle -->
## API Lifecycle

The API binary exposes lifecycle metrics covering graceful shutdown behavior.
Each row maps a lifecycle stage to the metric that diagnoses it at 3 AM.

| stage | file:line | required metric name(s) | category |
| --- | --- | --- | --- |
| api shutdown | go/cmd/api/main.go:100 | `eshu_dp_shutdown_duration_seconds` | api lifecycle |

<!-- eshu:metric:section=graph-write-statement-metadata -->
## Graph-Write Statement Metadata

The cypher package tags every batched statement with phase metadata so the
backpressure executor and failure classifier can preserve phase ordering and
diagnose failure causes. Each row maps a canonical phase to its metric.

| stage | file:line | required metric name(s) | category |
| --- | --- | --- | --- |
| entities (canonical nodes) | go/internal/storage/cypher/phase_group_metadata.go:26 | `eshu_dp_canonical_nodes_written_total`, `eshu_dp_canonical_phase_duration_seconds` | cypher phase |
| entity_containment | go/internal/storage/cypher/phase_group_metadata.go:29 | `eshu_dp_canonical_edges_written_total`, `eshu_dp_canonical_phase_duration_seconds` | cypher phase |
| directories | go/internal/storage/cypher/phase_group_metadata.go:31 | `eshu_dp_canonical_nodes_written_total`, `eshu_dp_canonical_phase_duration_seconds` | cypher phase |
| files | go/internal/storage/cypher/phase_group_metadata.go:33 | `eshu_dp_canonical_nodes_written_total`, `eshu_dp_canonical_phase_duration_seconds` | cypher phase |
| retract (pre-upsert) | go/internal/storage/cypher/canonical_node_writer.go | `eshu_dp_canonical_retract_duration_seconds` | cypher phase |
| entity_retract | go/internal/storage/cypher/canonical_node_writer.go | `eshu_dp_canonical_retract_duration_seconds`, `eshu_dp_canonical_phase_duration_seconds` | cypher phase |
| repository_cleanup | go/internal/storage/cypher/canonical_node_writer.go | `eshu_dp_canonical_retract_duration_seconds` | cypher phase |
| repository | go/internal/storage/cypher/canonical_node_writer.go | `eshu_dp_canonical_nodes_written_total`, `eshu_dp_canonical_phase_duration_seconds` | cypher phase |
| terraform_state | go/internal/storage/cypher/canonical_node_writer.go | `eshu_dp_canonical_nodes_written_total`, `eshu_dp_canonical_phase_duration_seconds` | cypher phase |
| oci_registry | go/internal/storage/cypher/canonical_node_writer.go | `eshu_dp_canonical_nodes_written_total`, `eshu_dp_canonical_phase_duration_seconds` | cypher phase |
| modules | go/internal/storage/cypher/canonical_node_writer.go | `eshu_dp_canonical_nodes_written_total`, `eshu_dp_canonical_phase_duration_seconds` | cypher phase |
| structural_edges | go/internal/storage/cypher/canonical_node_writer.go | `eshu_dp_canonical_edges_written_total`, `eshu_dp_canonical_phase_duration_seconds` | cypher phase |

<!-- eshu:metric:section=mcp-api-routes -->
## MCP / API Routes

Every query API and MCP read route emits two metrics through one middleware
(`go/internal/query/request_metrics.go:75`). One row covers the entire route
catalog; per-route variants share the same `route` label dimension.

| stage | file:line | required metric name(s) | category |
| --- | --- | --- | --- |
| HTTP API/MCP per-route latency | go/internal/query/request_metrics.go:75 | `eshu_dp_api_request_duration_seconds`, `eshu_dp_api_request_errors_total` | query surface |
| HTTP API/MCP per-route span | go/internal/query/handler_tracing.go:16 | `eshu_dp_api_request_duration_seconds` (parent), `query.*` span | query surface |
| Cloud resources (list, inventory) | go/internal/query/cloud_resources.go:58 | `eshu_dp_api_request_duration_seconds`, `eshu_dp_api_request_errors_total`, `eshu_dp_cloud_resource_list_duration_seconds`, `eshu_dp_cloud_resource_list_errors_total` | query cloud |
| IaC resources | go/internal/query/iac_resources.go | `eshu_dp_iac_resource_list_duration_seconds`, `eshu_dp_iac_resource_list_errors_total`, `eshu_dp_api_request_duration_seconds` | query IaC |
| Dependencies | go/internal/query/dependencies.go | `eshu_dp_dependency_list_duration_seconds`, `eshu_dp_dependency_list_errors_total`, `eshu_dp_api_request_duration_seconds` | query dependencies |
| Documentation findings/facts | go/internal/query/documentation_*.go | `eshu_dp_api_request_duration_seconds`, `eshu_dp_api_request_errors_total` | query docs |
| Evidence citations | go/internal/query/evidence_citation.go | `eshu_dp_api_request_duration_seconds`, `eshu_dp_api_request_errors_total` | query evidence |
| Status and admin | go/internal/query/admin.go:182 | `eshu_dp_api_request_duration_seconds`, `eshu_dp_api_request_errors_total` | query admin |
| Auth (sessions, OIDC, SAML) | go/internal/query/browser_session*.go | `eshu_dp_api_request_duration_seconds`, `eshu_dp_api_request_errors_total` | query auth |
| OIDC login rate-limit | go/internal/query/oidc_rate_limiter.go | `eshu_dp_oidc_login_throttled_total` | query auth |
| Search hybrid degradation | go/internal/query/semantic_search_telemetry.go | `eshu_dp_search_hybrid_degraded_total` | query search |
| Component extensions | go/internal/query/component_extensions.go:149 | `eshu_dp_api_request_duration_seconds`, `eshu_dp_api_request_errors_total` | query extensions |
| Ask (CLI/MCP) | go/internal/query/ask_handler.go:159 | `eshu_dp_api_request_duration_seconds`, `eshu_dp_api_request_errors_total` | query ask |
| Capabilities | go/internal/query/capabilities.go:47 | `eshu_dp_api_request_duration_seconds`, `eshu_dp_api_request_errors_total` | query capabilities |
| Code (search, structure, dead-code, call-graph) | go/internal/query/code.go:25 | `eshu_dp_api_request_duration_seconds`, `eshu_dp_api_request_errors_total` | query code |
| Content (files, entities, search) | go/internal/query/content_handler.go:42 | `eshu_dp_api_request_duration_seconds`, `eshu_dp_api_request_errors_total` | query content |
| Repositories, ingester, bundles | go/internal/query/repositories*.go | `eshu_dp_api_request_duration_seconds`, `eshu_dp_api_request_errors_total` | query repository |
| Replatforming (rollups, plan, ownership) | go/internal/query/replatforming*.go | `eshu_dp_api_request_duration_seconds`, `eshu_dp_api_request_errors_total` | query replatforming |
| Service/workload context | go/internal/query/service_*.go | `eshu_dp_api_request_duration_seconds`, `eshu_dp_api_request_errors_total` | query service |
| Collector readiness | go/internal/query/collector_*.go | `eshu_dp_api_request_duration_seconds`, `eshu_dp_api_request_errors_total` | query collector |
| Supply-chain impact, vulnerability | go/internal/query/supply_chain*.go | `eshu_dp_api_request_duration_seconds`, `eshu_dp_api_request_errors_total` | query supply-chain |
| Investigation workflows | go/internal/query/investigation_workflows*.go | `eshu_dp_api_request_duration_seconds`, `eshu_dp_api_request_errors_total` | query investigation |
| Webhooks | go/cmd/webhook-listener/handler.go:368 | `eshu_dp_webhook_requests_total`, `eshu_dp_webhook_trigger_decisions_total`, `eshu_dp_webhook_store_operations_total`, `eshu_dp_webhook_request_duration_seconds`, `eshu_dp_webhook_store_duration_seconds` | webhook surface |
| Infra relationships | go/internal/query/infra_relationship*.go | `eshu_dp_api_request_duration_seconds`, `eshu_dp_api_request_errors_total` | query infra |

<!-- eshu:metric:section=otel-span-names -->
## OTEL Span Names

Span name constants live in `go/internal/telemetry/contract.go` and the
`contract_*.go` sibling files. Every span emits a duration histogram at the
parent level (where applicable) and is paired with one of the metric
counters above. The table groups spans by category so a maintainer can find
the right name when adding a new stage.

| stage | file:line | required metric name(s) | category |
| --- | --- | --- | --- |
| collector.observe | go/internal/telemetry/contract.go:222 | `eshu_dp_collector_observe_duration_seconds` | span collector |
| collector.stream | go/internal/telemetry/contract.go:223 | `eshu_dp_collector_observe_duration_seconds` | span collector |
| collector.claimed_run | go/internal/telemetry/contract_collector_run.go:69 | `eshu_dp_workflow_claim_run_duration_seconds` | span collector |
| collector.snapshot_stage | go/internal/telemetry/contract_collector_stage.go:59 | `eshu_dp_collector_snapshot_stage_duration_seconds` | span collector |
| bootstrap.collector_cycle | go/internal/telemetry/contract_bootstrap_ingestion.go:24 | `eshu_dp_bootstrap_pipeline_phase_seconds` | span bootstrap |
| scope.assign | go/internal/telemetry/contract.go:224 | `eshu_dp_scope_assign_duration_seconds` | span scope |
| fact.emit | go/internal/telemetry/contract.go:225 | `eshu_dp_fact_emit_duration_seconds` | span fact |
| projector.run | go/internal/telemetry/contract.go:226 | `eshu_dp_projector_run_duration_seconds` | span projector |
| reducer_intent.enqueue | go/internal/telemetry/contract.go:227 | `eshu_dp_reducer_intents_enqueued_total` | span reducer |
| reducer.run | go/internal/telemetry/contract.go:228 | `eshu_dp_reducer_run_duration_seconds` | span reducer |
| reducer.batch_claim | go/internal/telemetry/contract.go:229 | `eshu_dp_reducer_batch_claim_size` | span reducer |
| reducer.eshu_search_index_write | go/internal/telemetry/contract.go:234 | `eshu_dp_search_index_write_duration_seconds` | span reducer |
| reducer.drift_evidence_load | go/internal/telemetry/contract.go:242 | `eshu_dp_postgres_query_duration_seconds` | span reducer |
| reducer.aws_runtime_drift_evidence_load | go/internal/telemetry/contract.go:246 | `eshu_dp_postgres_query_duration_seconds` | span reducer |
| reducer.multi_cloud_runtime_drift_evidence_load | go/internal/telemetry/contract.go:252 | `eshu_dp_postgres_query_duration_seconds` | span reducer |
| reducer.aws_relationship_materialization | go/internal/telemetry/contract.go:258 | `eshu_dp_aws_relationship_edges_total` | span reducer |
| reducer.gcp_relationship_materialization | go/internal/telemetry/contract.go:265 | `eshu_dp_gcp_relationship_edges_total` | span reducer |
| reducer.observability_coverage_materialization | go/internal/telemetry/contract.go:272 | `eshu_dp_observability_coverage_edges_total` | span reducer |
| reducer.iam_can_assume_materialization | go/internal/telemetry/contract.go:279 | `eshu_dp_iam_can_assume_edges_total` | span reducer |
| reducer.kubernetes_correlation_materialization | go/internal/telemetry/contract.go:287 | `eshu_dp_kubernetes_correlation_edges_total` | span reducer |
| reducer.s3_logs_to_materialization | go/internal/telemetry/contract.go:294 | `eshu_dp_s3_logs_to_edges_total` | span reducer |
| reducer.rds_posture_materialization | go/internal/telemetry/contract.go:301 | `eshu_dp_postgres_query_duration_seconds` | span reducer |
| reducer.ec2_uses_profile_materialization | go/internal/telemetry/contract.go:310 | `eshu_dp_ec2_uses_profile_edges_total` | span reducer |
| reducer.iam_instance_profile_role_materialization | go/internal/telemetry/contract.go:318 | `eshu_dp_iam_instance_profile_role_edges_total` | span reducer |
| reducer.ec2_internet_exposure_materialization | go/internal/telemetry/contract.go:325 | `eshu_dp_ec2_internet_exposure_decisions_total` | span reducer |
| reducer.ec2_block_device_kms_posture_materialization | go/internal/telemetry/contract.go:334 | `eshu_dp_ec2_block_device_kms_posture_decisions_total` | span reducer |
| reducer.s3_internet_exposure_materialization | go/internal/telemetry/contract.go:341 | `eshu_dp_s3_internet_exposure_decisions_total` | span reducer |
| reducer.security_group_reachability_materialization | go/internal/telemetry/contract.go:350 | `eshu_dp_security_group_reachability_edges_total` | span reducer |
| reducer.iam_escalation_materialization | go/internal/telemetry/contract.go:360 | `eshu_dp_iam_escalation_edges_total` | span reducer |
| reducer.iam_can_perform_materialization | go/internal/telemetry/contract.go:373 | `eshu_dp_iam_can_perform_edges_total` | span reducer |
| reducer.secrets_iam_graph_projection | go/internal/telemetry/contract.go:376 | `eshu_dp_secrets_iam_graph_nodes_written_total` | span reducer |
| canonical.write | go/internal/telemetry/contract.go:377 | `eshu_dp_canonical_write_duration_seconds` | span canonical |
| canonical.projection | go/internal/telemetry/contract.go:378 | `eshu_dp_canonical_projection_duration_seconds` | span canonical |
| canonical.retract | go/internal/telemetry/contract.go:379 | `eshu_dp_canonical_retract_duration_seconds` | span canonical |
| ingestion.evidence_discovery | go/internal/telemetry/contract.go:381 | `eshu_dp_evidence_facts_discovered_total` | span ingestion |
| iac_reachability.materialize | go/internal/telemetry/contract.go:382 | `eshu_dp_iac_reachability_materialization_duration_seconds` | span IaC |
| reducer.sql_relationship_materialization | go/internal/telemetry/contract.go:383 | `eshu_dp_postgres_query_duration_seconds` | span reducer |
| reducer.inheritance_materialization | go/internal/telemetry/contract.go:384 | `eshu_dp_postgres_query_duration_seconds` | span reducer |
| reducer.cross_repo_resolution | go/internal/telemetry/contract.go:385 | `eshu_dp_cross_repo_resolution_duration_seconds` | span reducer |
| reducer.code_import_repo_edge | go/internal/telemetry/contract.go:386 | `eshu_dp_code_import_repo_edges_total` | span reducer |
| shared_acceptance.lookup | go/internal/telemetry/contract.go:387 | `eshu_dp_shared_acceptance_lookup_duration_seconds` | span shared acceptance |
| shared_acceptance.upsert | go/internal/telemetry/contract.go:388 | `eshu_dp_shared_acceptance_upsert_duration_seconds` | span shared acceptance |
| query.* (handler spans) | go/internal/telemetry/contract.go:389-470, contract_z_observability_coverage.go:10 | `eshu_dp_api_request_duration_seconds` | span query |
| tfstate.collector.* (claim/parse/emit) | go/internal/telemetry/contract.go:491-496 | `eshu_dp_tfstate_snapshots_observed_total`, `eshu_dp_tfstate_resources_emitted_total`, `eshu_dp_tfstate_outputs_emitted_total`, `eshu_dp_tfstate_modules_emitted_total`, `eshu_dp_tfstate_warnings_emitted_total`, `eshu_dp_tfstate_redactions_applied_total`, `eshu_dp_tfstate_s3_conditional_get_not_modified_total`, `eshu_dp_tfstate_parse_duration_seconds` | span tfstate |
| webhook.handle / webhook.store | go/internal/telemetry/contract.go:497-498 | `eshu_dp_webhook_request_duration_seconds`, `eshu_dp_webhook_store_duration_seconds` | span webhook |
| oci_registry.scan / oci_registry.api_call | go/internal/telemetry/contract.go:499-500 | `eshu_dp_oci_registry_scan_duration_seconds` | span OCI |
| kubernetes_live.snapshot / kubernetes_live.api_call | go/internal/telemetry/contract.go:501-502 | `eshu_dp_kubernetes_list_duration_seconds` | span Kubernetes |
| vault_live.snapshot | go/internal/telemetry/contract.go:503 | `No-Observability-Change: Vault snapshot metrics share SecretsIAMSource* counters and span is parent-only` | span vault |
| vault_live.api_call | go/cmd/collector-vault-live/service.go:166 | `eshu_dp_vault_request_total` | span vault |
| package_registry.observe / package_registry.fetch | go/internal/telemetry/contract.go:504-505 | `eshu_dp_package_registry_observe_duration_seconds` | span package registry |
| aws.collector.claim.process | go/internal/telemetry/contract.go:506 | `eshu_dp_aws_scan_duration_seconds` | span AWS |
| aws.credentials.assume_role | go/internal/telemetry/contract.go:507 | `eshu_dp_aws_assumerole_failed_total` | span AWS |
| aws.service.scan / aws.service.pagination.page | go/internal/telemetry/contract.go:508-509 | `eshu_dp_aws_api_calls_total` | span AWS |
| postgres.exec / postgres.query | go/internal/telemetry/contract.go:512-513 | `eshu_dp_postgres_query_duration_seconds` | span dependency |
| neo4j.execute | go/internal/telemetry/contract.go:514 | `eshu_dp_neo4j_query_duration_seconds` | span dependency |

<!-- eshu:metric:section=structured-log-keys -->
## Structured Log Keys

Log key constants live in `go/internal/telemetry/contract.go:519-623`. Phase
constants live in `go/internal/telemetry/logging.go:166-175`. Every log line
MUST carry `LogKeyPipelinePhase` (`pipeline_phase`) so an operator can filter
on phase at 3 AM.

| stage | file:line | required metric name(s) | category |
| --- | --- | --- | --- |
| pipeline_phase (always) | go/internal/telemetry/logging.go:178 | `eshu_dp_api_request_duration_seconds` (parent) | log phase |
| phase: discovery | go/internal/telemetry/logging.go:167 | `eshu_dp_discovery_dirs_skipped_total`, `eshu_dp_discovery_files_skipped_total` | log phase |
| phase: parsing | go/internal/telemetry/logging.go:168 | `eshu_dp_file_parse_duration_seconds`, `eshu_dp_files_parsed_total` | log phase |
| phase: emission | go/internal/telemetry/logging.go:169 | `eshu_dp_fact_emit_duration_seconds`, `eshu_dp_facts_emitted_total`, `eshu_dp_facts_committed_total` | log phase |
| phase: projection | go/internal/telemetry/logging.go:170 | `eshu_dp_projector_run_duration_seconds`, `eshu_dp_projector_stage_duration_seconds` | log phase |
| phase: reduction | go/internal/telemetry/logging.go:171 | `eshu_dp_reducer_run_duration_seconds`, `eshu_dp_reducer_executions_total` | log phase |
| phase: shared | go/internal/telemetry/logging.go:172 | `eshu_dp_shared_projection_cycles_total` | log phase |
| phase: query | go/internal/telemetry/logging.go:173 | `eshu_dp_api_request_duration_seconds` | log phase |
| phase: serve | go/internal/telemetry/logging.go:174 | `eshu_dp_api_request_duration_seconds` | log phase |
| scope_id, scope_kind, generation_id, source_system | go/internal/telemetry/contract.go:520-523 | (no metric; structured log fields) | log key |
| collector_kind, domain, partition_key | go/internal/telemetry/contract.go:524-526 | `eshu_dp_workflow_claim_run_duration_seconds` (parent) | log key |
| failure_class | go/internal/telemetry/contract.go:528 | `eshu_dp_workflow_claim_retries_total`, `eshu_dp_workflow_claim_provider_throttle_total` | log key |
| request_id | go/internal/telemetry/contract.go:527 | `eshu_dp_api_request_duration_seconds` (parent) | log key |
| acceptance.* (scope, unit, source_run, generation, stale_count) | go/internal/telemetry/contract.go:531-535 | `eshu_dp_shared_acceptance_rows`, `eshu_dp_shared_projection_stale_intents_total` | log key |
| resource.fingerprint, resource.identity_kind, resource.type | go/internal/telemetry/contract.go:540-547 | (no metric; safe log fields) | log key |
| drift.depth, drift.prior_config_addresses, drift.state_only_addresses, drift.addresses_promoted_to_removed_from_config | go/internal/telemetry/contract.go:553-569 | `eshu_dp_correlation_drift_detected_total` | log key |
| drift.multi_element.prefix, multi_element.count, multi_element.source | go/internal/telemetry/contract.go:578-591 | `eshu_dp_drift_schema_unknown_composite_total` | log key |
| drift.resource_type, attribute_key, path, error, reason | go/internal/telemetry/contract.go:598-622 | `eshu_dp_drift_schema_unknown_composite_total` | log key |

## Diff Semantics

- The CI coverage script (X2) reads this doc as a sequence of
  `stage | file:line | metric | category` rows. It parses every line that
  starts with `|` after a section header.
- For each row, the script asserts at least one of:
  1. The metric column lists a registered instrument name from
     `go/internal/telemetry/instruments.go` (X2 grep's the `meter.Int64Counter`,
     `meter.Float64Histogram`, etc., declarations for that exact name).
  2. The metric column contains the literal `No-Observability-Change:` token
     followed by a justification naming the existing signal.
- For each row whose `file:line` references a Go file, the script asserts
  that the file exists and the line range contains a recording call
  (`Instruments.*.Record`, `Instruments.*.Add`, `s.Instruments.*.Record`,
  `s.Instruments.*.Add`, `tracer.Start`, or `slog.*`).
- For each row whose category is `cypher phase`, the script asserts that the
  phase string is registered in `go/internal/storage/cypher/phase_group_metadata.go`.
- A new pipeline stage added to the source tree without a corresponding row
  in this doc fails the X2 + X3 gate. The error message names the missing
  row's expected location.
- Maintainers update this doc in the same PR that adds the new stage. The
  doc is hand-authored; X2 does not auto-generate rows from source code.

## Markers

- `No-Observability-Change:` is a literal string the script grep-parses for.
  It appears in the metric column when the stage does not need a new metric
  because an existing one already diagnoses it.
- The marker is per-stage: one marker per row that does not have a registered
  metric name. The script treats the absence of a registered metric AND the
  absence of a marker as a gate failure.
- The marker is NOT a "I forgot". It is a documented decision that names the
  existing signal and why it covers the stage. Example:
  `No-Observability-Change: git-source collector metrics (FactsEmitted, RepoSnapshotDuration) cover preflight throughput`.
- The marker language matches the `No-Observability-Change:` evidence marker
  in the per-PR commit message policy at
  `docs/internal/agent-guide.md:143`. PR authors cite the same row in their
  commit message.

## Regeneration Note

- This doc is hand-authored. There is no auto-generation from
  `go/internal/telemetry/instruments.go` or any other source file.
- Each row is one stage. The table is the source of truth for X2.
- To add a new stage:
  1. Register the metric in `go/internal/telemetry/instruments.go` (or
     confirm an existing instrument already diagnoses the path).
  2. Add a row to the appropriate section of this doc, including the
     `file:line` citation to the Go file that records the metric.
  3. Run `wc -l docs/public/observability/telemetry-coverage.md` to confirm
     the doc stays under the 500-line file rule.
  4. Run the docs build:
     `uv run --with mkdocs --with mkdocs-material --with pymdown-extensions \
     mkdocs build --strict --clean --config-file docs/mkdocs.yml`.
  5. Run `git diff --check` to confirm no whitespace errors.
- To remove a stage, delete the row and remove the matching metric from
  `go/internal/telemetry/instruments.go` in the same PR. X2 will fail
  otherwise because the metric is still registered.

## Linked Work

- [#3633](https://github.com/eshu-hq/eshu/issues/3633) (closed 2026-06-23) —
  root-cause class: defined-but-never-registered instruments. The historical
  precedent that motivated this contract.
- [#3680](https://github.com/eshu-hq/eshu/issues/3680) (open, 2026-06-24,
  closed at the time of #3694 sign-off) — per-collector envelope telemetry.
  The in-flight adoption that adds the per-collector rows above.
- [`go/internal/telemetry/instruments.go`](https://github.com/eshu-hq/eshu/blob/main/go/internal/telemetry/instruments.go)
  — metric source of truth. Every metric name in the tables above resolves to
  a `meter.*(...)` declaration here.
- [`go/internal/telemetry/contract.go`](https://github.com/eshu-hq/eshu/blob/main/go/internal/telemetry/contract.go)
  — dimension keys, span names, log keys. Every span name in the OTEL Span
  Names section resolves to a `Span*` constant here or in the sibling
  `contract_*.go` files.
- [`docs/public/reference/telemetry/index.md`](https://github.com/eshu-hq/eshu/blob/main/docs/public/reference/telemetry/index.md)
  — public operator contract. This coverage doc extends the public contract
  by enumerating every stage, not just the most-used signals.
- [`docs/internal/agent-guide.md:120-146`](https://github.com/eshu-hq/eshu/blob/main/docs/internal/agent-guide.md)
  — five evidence markers policy. The marker language in the doc and PR
  commits comes from this section.

## Flow Affected

`reducer -> graph write -> telemetry contract`. This is the flow the contract
covers. New reducer projection stages, new shared-edge writers, and new
graph-write statement phases all flow through this contract; the X2 script
is the machine-enforced gate.

<!-- eshu:metric:section=histogram-buckets -->
## Histogram Bucket Boundaries

Each documented bucket set maps a short name to the exact boundary values used
in `go/internal/telemetry/instruments.go`. The X2 verifier asserts that every
`WithExplicitBucketBoundaries(...)` call in instruments.go matches a documented
set, and every documented set has a matching variable in the code.

| set_name | boundary_values |
| --- | --- |
| collector-seconds | 0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60 |
| workflow-claim-wait-seconds | 0, 0.1, 0.5, 1, 5, 10, 30, 60, 300, 900, 1800, 3600 |
| tfstate-snapshot-bytes | 1024, 10240, 102400, 1048576, 10485760, 52428800, 104857600 |
| tfstate-parse-seconds | 0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10 |
| oci-registry-scan-seconds | 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120 |
| dependency-list-seconds | 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10 |
| fetch-duration-seconds | 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60 |
| queue-wait-seconds | 0.001, 0.01, 0.1, 1, 5, 10, 30, 60, 300, 900, 1800, 3600, 21600 |
| scanner-worker-scan-seconds | 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300, 600, 1200 |
| scanner-worker-count | 1, 10, 100, 1000, 10000, 100000 |
| scanner-worker-cpu-seconds | 0.01, 0.1, 1, 10, 30, 60, 120, 300, 600, 1800 |
| scanner-worker-memory-bytes | 1048576, 16777216, 67108864, 268435456, 1073741824, 2147483648, 4294967296, 8589934592, 17179869184 |
| aws-scan-seconds | 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300 |
| scope-assign-seconds | 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30 |
| fact-emit-seconds | 0.1, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300 |
| projector-run-seconds | 0.1, 0.5, 1, 2.5, 5, 10, 30, 60, 120 |
| projector-stage-seconds | 0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120 |
| reducer-run-seconds | 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300, 900 |
| retention-duration-seconds | 0.001, 0.01, 0.1, 1, 5, 10, 30, 60, 300, 900 |
| retention-batch-count | 1, 2, 4, 8, 16, 32, 64, 100 |
| retention-age-seconds | 3600, 21600, 43200, 86400, 259200, 604800, 1209600, 2592000, 7776000 |
| postgres-query-seconds | 0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5 |
| canonical-phase-seconds | 0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5 |
| acceptance-lookup-seconds | 0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30 |
| acceptance-prefetch-count | 1, 2, 4, 8, 16, 32, 64, 128, 256, 512 |
| shared-projection-processing-seconds | 0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60 |
| collector-stage-seconds | 0.005, 0.025, 0.1, 0.5, 1, 2.5, 5, 10, 30, 60, 120 |
| scip-process-wait-seconds | 0, 0.01, 0.05, 0.1, 0.5, 1, 5, 10, 30, 60 |
| generation-fact-count | 10, 50, 100, 500, 1000, 5000, 10000, 50000, 100000, 300000 |
| large-repo-semaphore-seconds | 0, 0.1, 0.5, 1, 5, 10, 30, 60, 120, 300 |
| batch-claim-count | 1, 4, 8, 16, 32, 64, 128 |
| neo4j-batch-count | 1, 10, 50, 100, 250, 500, 1000 |
| shared-edge-statement-count | 1, 2, 4, 8, 16, 32, 64, 128 |
| code-call-edge-seconds | 0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5 |
| cross-repo-resolution-seconds | 0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30 |
| bootstrap-phase-seconds | 1, 5, 15, 30, 60, 120, 300, 600, 1200, 1800, 3600 |
| workflow-claim-run-seconds | 0.1, 0.5, 1, 5, 15, 30, 60, 120, 300, 600, 1200, 1800 |
| pipeline-overlap-seconds | 1, 5, 10, 30, 60, 120, 300, 600, 1800 |
| shutdown-duration-seconds | 0.5, 1, 2.5, 5, 10, 30, 60 |
| deferred-backfill-partition-workers-count | 1, 2, 4, 8, 16, 32 |
