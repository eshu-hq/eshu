# Telemetry Coverage: Span Names, Log Keys, And Histogram Buckets

Part of the [Telemetry Coverage Contract](telemetry-coverage.md). This part holds the OTEL span name table, the structured log key table, and the histogram bucket boundary table the gate compares with `go/internal/telemetry/instruments.go`. The column contract and the rules for adding a row are on that index page.

<!-- eshu:metric:section=otel-span-names -->
## OTEL Span Names

Span name constants live in `go/internal/telemetry/contract.go` and the per-family
files under `go/internal/telemetry/contract/`. Every span emits a duration histogram at the
parent level (where applicable) and is paired with one of the metric
counters above. The table groups spans by category so a maintainer can find
the right name when adding a new stage.

| stage | file:line | required metric name(s) | category |
| --- | --- | --- | --- |
| collector.observe | go/internal/telemetry/contract.go | `eshu_dp_collector_observe_duration_seconds` | span collector |
| collector.stream | go/internal/telemetry/contract.go | `eshu_dp_collector_observe_duration_seconds` | span collector |
| collector.claimed_run | go/internal/telemetry/contract/collector_run.go (`SpanCollectorClaimedRun`) | `eshu_dp_workflow_claim_run_duration_seconds` | span collector |
| collector.snapshot_stage | go/internal/telemetry/contract/collector_stage.go (`SpanCollectorSnapshotStage`) | `eshu_dp_collector_snapshot_stage_duration_seconds` | span collector |
| bootstrap.collector_cycle | go/internal/telemetry/contract/bootstrap_ingestion.go (`SpanBootstrapCollectorCycle`) | `eshu_dp_bootstrap_pipeline_phase_seconds` | span bootstrap |
| scope.assign | go/internal/telemetry/contract.go | `eshu_dp_scope_assign_duration_seconds` | span scope |
| fact.emit | go/internal/telemetry/contract.go | `eshu_dp_fact_emit_duration_seconds` | span fact |
| projector.run | go/internal/telemetry/contract.go | `eshu_dp_projector_run_duration_seconds` | span projector |
| reducer_intent.enqueue | go/internal/telemetry/contract.go | `eshu_dp_reducer_intents_enqueued_total` | span reducer |
| reducer.run | go/internal/telemetry/contract.go | `eshu_dp_reducer_run_duration_seconds` | span reducer |
| reducer.batch_claim | go/internal/telemetry/contract.go | `eshu_dp_reducer_batch_claim_size` | span reducer |
| reducer.eshu_search_index_write | go/internal/telemetry/contract.go | `eshu_dp_search_index_write_duration_seconds` | span reducer |
| reducer.drift_evidence_load | go/internal/telemetry/contract.go | `eshu_dp_postgres_query_duration_seconds` | span reducer |
| reducer.aws_runtime_drift_evidence_load | go/internal/telemetry/contract.go | `eshu_dp_postgres_query_duration_seconds` | span reducer |
| reducer.multi_cloud_runtime_drift_evidence_load | go/internal/telemetry/contract.go | `eshu_dp_postgres_query_duration_seconds` | span reducer |
| reducer.aws_relationship_materialization | go/internal/telemetry/contract.go | `eshu_dp_aws_relationship_edges_total` | span reducer |
| reducer.aws_cloud_image_materialization | go/internal/telemetry/contract.go | `eshu_dp_aws_cloud_image_edges_total` | span reducer |
| reducer.gcp_relationship_materialization | go/internal/telemetry/contract.go | `eshu_dp_gcp_relationship_edges_total` | span reducer |
| reducer.observability_coverage_materialization | go/internal/telemetry/contract.go | `eshu_dp_observability_coverage_edges_total` | span reducer |
| reducer.iam_can_assume_materialization | go/internal/telemetry/contract.go | `eshu_dp_iam_can_assume_edges_total` | span reducer |
| reducer.kubernetes_correlation_materialization | go/internal/telemetry/contract.go | `eshu_dp_kubernetes_correlation_edges_total` | span reducer |
| reducer.crossplane_satisfied_by_materialization | go/internal/telemetry/contract.go | `eshu_dp_crossplane_satisfied_by_edges_total` | span reducer |
| reducer.s3_logs_to_materialization | go/internal/telemetry/contract.go | `eshu_dp_s3_logs_to_edges_total` | span reducer |
| reducer.rds_posture_materialization | go/internal/telemetry/contract.go | `eshu_dp_postgres_query_duration_seconds` | span reducer |
| reducer.ec2_instance_identity_materialization | go/internal/telemetry/contract.go | `eshu_dp_postgres_query_duration_seconds` | span reducer |
| reducer.ec2_uses_profile_materialization | go/internal/telemetry/contract.go | `eshu_dp_ec2_uses_profile_edges_total` | span reducer |
| reducer.iam_instance_profile_role_materialization | go/internal/telemetry/contract.go | `eshu_dp_iam_instance_profile_role_edges_total` | span reducer |
| reducer.ec2_internet_exposure_materialization | go/internal/telemetry/contract.go | `eshu_dp_ec2_internet_exposure_decisions_total` | span reducer |
| reducer.ec2_block_device_kms_posture_materialization | go/internal/telemetry/contract.go | `eshu_dp_ec2_block_device_kms_posture_decisions_total` | span reducer |
| reducer.s3_internet_exposure_materialization | go/internal/telemetry/contract.go | `eshu_dp_s3_internet_exposure_decisions_total` | span reducer |
| reducer.security_group_reachability_materialization | go/internal/telemetry/contract.go | `eshu_dp_security_group_reachability_edges_total` | span reducer |
| reducer.iam_escalation_materialization | go/internal/telemetry/contract.go | `eshu_dp_iam_escalation_edges_total` | span reducer |
| reducer.iam_can_perform_materialization | go/internal/telemetry/contract.go | `eshu_dp_iam_can_perform_edges_total` | span reducer |
| reducer.secrets_iam_graph_projection | go/internal/telemetry/contract.go | `eshu_dp_secrets_iam_graph_nodes_written_total` | span reducer |
| canonical.write | go/internal/telemetry/contract.go | `eshu_dp_canonical_write_duration_seconds` | span canonical |
| canonical.projection | go/internal/telemetry/contract.go | `eshu_dp_canonical_projection_duration_seconds` | span canonical |
| canonical.retract | go/internal/telemetry/contract.go | `eshu_dp_canonical_projection_duration_seconds` | span canonical |
| ingestion.evidence_discovery | go/internal/telemetry/contract.go | `eshu_dp_evidence_facts_discovered_total` | span ingestion |
| ingestion.commit.claim_fence | go/internal/telemetry/contract/ingestion_claim_fence.go (`SpanIngestionCommitClaimFence`) | `eshu_dp_ingestion_claim_fence_rejections_total`, `eshu_dp_ingestion_commit_pin_slack_seconds` | span ingestion |
| iac_reachability.materialize | go/internal/telemetry/contract.go | `eshu_dp_iac_reachability_materialization_duration_seconds` | span IaC |
| reducer.sql_relationship_materialization | go/internal/telemetry/contract.go | `eshu_dp_postgres_query_duration_seconds` | span reducer |
| reducer.inheritance_materialization | go/internal/telemetry/contract.go | `eshu_dp_postgres_query_duration_seconds` | span reducer |
| reducer.cross_repo_resolution | go/internal/telemetry/contract.go | `eshu_dp_cross_repo_resolution_duration_seconds` | span reducer |
| reducer.code_import_repo_edge | go/internal/telemetry/contract.go | `eshu_dp_code_import_repo_edges_total` | span reducer |
| shared_acceptance.lookup | go/internal/telemetry/contract.go | `eshu_dp_shared_acceptance_lookup_duration_seconds` | span shared acceptance |
| shared_acceptance.upsert | go/internal/telemetry/contract.go | `eshu_dp_shared_acceptance_upsert_duration_seconds` | span shared acceptance |
| query.* (handler spans) | go/internal/telemetry/contract.go (`SpanQuery*` block), go/internal/telemetry/contract/z_observability_coverage.go | `eshu_dp_api_request_duration_seconds` | span query |
| mcp.router.find | go/internal/telemetry/contract/mcp_router.go (`SpanMCPRouterFind`) | `eshu_dp_mcp_router_find_total` | span mcp |
| mcp.router.call | go/internal/telemetry/contract/mcp_router.go (`SpanMCPRouterCall`) | `eshu_dp_mcp_router_call_total` | span mcp |
| tfstate.collector.* (claim/parse/emit) | go/internal/telemetry/contract.go | `eshu_dp_tfstate_snapshots_observed_total`, `eshu_dp_tfstate_resources_emitted_total`, `eshu_dp_tfstate_outputs_emitted_total`, `eshu_dp_tfstate_modules_emitted_total`, `eshu_dp_tfstate_warnings_emitted_total`, `eshu_dp_tfstate_redactions_applied_total`, `eshu_dp_tfstate_s3_conditional_get_not_modified_total`, `eshu_dp_tfstate_parse_duration_seconds` | span tfstate |
| webhook.handle / webhook.store | go/internal/telemetry/contract.go, go/internal/webhook/intake/spans.go | `eshu_dp_webhook_request_duration_seconds`, `eshu_dp_webhook_store_duration_seconds` | span webhook |
| oci_registry.scan / oci_registry.api_call | go/internal/telemetry/contract.go | `eshu_dp_oci_registry_scan_duration_seconds` | span OCI |
| kubernetes_live.snapshot / kubernetes_live.api_call | go/internal/telemetry/contract.go | `eshu_dp_kubernetes_list_duration_seconds` | span Kubernetes |
| vault_live.snapshot | go/internal/telemetry/contract.go | `No-Observability-Change: Vault snapshot metrics share SecretsIAMSource* counters and span is parent-only` | span vault |
| package_registry.observe / package_registry.fetch | go/internal/telemetry/contract.go | `eshu_dp_package_registry_observe_duration_seconds` | span package registry |
| aws.collector.claim.process | go/internal/telemetry/contract.go | `eshu_dp_aws_scan_duration_seconds` | span AWS |
| aws.credentials.assume_role | go/internal/telemetry/contract.go | `eshu_dp_aws_assumerole_failed_total` | span AWS |
| aws.service.scan / aws.service.pagination.page | go/internal/telemetry/contract.go | `eshu_dp_aws_api_calls_total` | span AWS |
| postgres.exec / postgres.query | go/internal/telemetry/contract.go | `eshu_dp_postgres_query_duration_seconds` | span dependency |
| neo4j.execute | go/internal/telemetry/contract.go | `eshu_dp_neo4j_query_duration_seconds` | span dependency |
| oidcbearer.resolve | go/internal/oidcbearer/telemetry.go | `eshu_dp_oidc_bearer_validation_total` | span query |
| media.transcript_engine.run | go/internal/telemetry/contract.go (`SpanMediaTranscriptEngineRun`) | `eshu_dp_media_transcript_engine_duration_seconds` | span collector |

<!-- eshu:metric:section=structured-log-keys -->
## Structured Log Keys

Log key constants live in `go/internal/telemetry/contract.go`. Phase
constants live in `go/internal/telemetry/logging.go`. Every log line
MUST carry `LogKeyPipelinePhase` (`pipeline_phase`) so an operator can filter
on phase at 3 AM.

| stage | file:line | required metric name(s) | category |
| --- | --- | --- | --- |
| pipeline_phase (always) | go/internal/telemetry/logging.go | `eshu_dp_api_request_duration_seconds` (parent) | log phase |
| phase: discovery | go/internal/telemetry/logging.go | `eshu_dp_discovery_dirs_skipped_total`, `eshu_dp_discovery_files_skipped_total` | log phase |
| phase: parsing | go/internal/telemetry/logging.go | `eshu_dp_file_parse_duration_seconds`, `eshu_dp_files_parsed_total` | log phase |
| phase: emission | go/internal/telemetry/logging.go | `eshu_dp_fact_emit_duration_seconds`, `eshu_dp_facts_emitted_total`, `eshu_dp_facts_committed_total` | log phase |
| phase: projection | go/internal/telemetry/logging.go | `eshu_dp_projector_run_duration_seconds`, `eshu_dp_projector_stage_duration_seconds` | log phase |
| phase: reduction | go/internal/telemetry/logging.go | `eshu_dp_reducer_run_duration_seconds`, `eshu_dp_reducer_executions_total` | log phase |
| phase: shared | go/internal/telemetry/logging.go | `eshu_dp_shared_projection_cycles_total` | log phase |
| phase: query | go/internal/telemetry/logging.go | `eshu_dp_api_request_duration_seconds` | log phase |
| phase: serve | go/internal/telemetry/logging.go | `eshu_dp_api_request_duration_seconds` | log phase |
| scope_id, scope_kind, generation_id, source_system | go/internal/telemetry/contract.go | (no metric; structured log fields) | log key |
| collector_kind, domain, partition_key | go/internal/telemetry/contract.go | `eshu_dp_workflow_claim_run_duration_seconds` (parent) | log key |
| failure_class | go/internal/telemetry/contract.go | `eshu_dp_workflow_claim_retries_total`, `eshu_dp_workflow_claim_provider_throttle_total` | log key |
| request_id | go/internal/telemetry/contract.go | `eshu_dp_api_request_duration_seconds` (parent) | log key |
| acceptance.* (scope, unit, source_run, generation, stale_count) | go/internal/telemetry/contract.go | `eshu_dp_shared_acceptance_rows`, `eshu_dp_shared_projection_stale_intents_total` | log key |
| resource.fingerprint, resource.identity_kind, resource.type | go/internal/telemetry/contract.go | (no metric; safe log fields) | log key |
| drift.depth, drift.prior_config_addresses, drift.state_only_addresses, drift.addresses_promoted_to_removed_from_config | go/internal/telemetry/contract.go | `eshu_dp_correlation_drift_detected_total` | log key |
| drift.multi_element.prefix, multi_element.count, multi_element.source | go/internal/telemetry/contract.go | `eshu_dp_drift_schema_unknown_composite_total` | log key |
| drift.resource_type, attribute_key, path, error, reason | go/internal/telemetry/contract.go | `eshu_dp_drift_schema_unknown_composite_total` | log key |

<!-- eshu:metric:section=histogram-buckets -->
## Histogram Bucket Boundaries

Each documented bucket set maps a short name to the exact boundary values in `go/internal/telemetry/instruments.go`. The X2 verifier asserts that every `WithExplicitBucketBoundaries(...)` call matches a documented set, and every documented set has a matching variable in the code.

| set_name | boundary_values |
| --- | --- |
| collector-seconds | 0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60 |
| workflow-claim-wait-seconds | 0, 0.1, 0.5, 1, 5, 10, 30, 60, 300, 900, 1800, 3600 |
| tfstate-snapshot-bytes | 1024, 10240, 102400, 1048576, 10485760, 52428800, 104857600 |
| repository-catalog-load-rows | 0, 10, 50, 100, 250, 500, 1000, 2500, 5000, 10000 |
| repository-catalog-load-payload-bytes | 1024, 10240, 102400, 1048576, 10485760, 52428800, 104857600, 268435456 |
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
| projector-ack-wait-seconds | 1, 2.5, 5, 10, 30, 60, 120, 180, 300, 600 |
| projector-stage-seconds | 0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120 |
| reducer-run-seconds | 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300, 900 |
| package-manifest-backfill-seconds | 0.001, 0.01, 0.1, 1, 5, 10, 30, 60, 300 |
| retention-duration-seconds | 0.001, 0.01, 0.1, 1, 5, 10, 30, 60, 300, 900 |
| retention-batch-count | 1, 2, 4, 8, 16, 32, 64, 100 |
| retention-age-seconds | 3600, 21600, 43200, 86400, 259200, 604800, 1209600, 2592000, 7776000 |
| postgres-query-seconds | 0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300 |
| file-parse-and-live-query-seconds | 0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5 |
| canonical-phase-seconds, code-call-edge-seconds | 0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5 |
| acceptance-lookup-seconds | 0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30 |
| shared-projection-processing-seconds | 0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60 |
| collector-stage-seconds | 0.005, 0.025, 0.1, 0.5, 1, 2.5, 5, 10, 30, 60, 120 |
| scip-process-wait-seconds | 0, 0.01, 0.05, 0.1, 0.5, 1, 5, 10, 30, 60 |
| generation-fact-count | 10, 50, 100, 500, 1000, 5000, 10000, 50000, 100000, 300000 |
| gcp-freshness-fanout-scope-count | 1, 2, 4, 8, 16, 32, 64 |
| large-repo-semaphore-seconds | 0, 0.1, 0.5, 1, 5, 10, 30, 60, 120, 300 |
| batch-claim-count | 1, 4, 8, 16, 32, 64, 128 |
| neo4j-batch-count | 1, 10, 50, 100, 250, 500, 1000 |
| shared-edge-statement-count | 1, 2, 4, 8, 16, 32, 64, 128 |
| cross-repo-resolution-seconds | 0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30 |
| bootstrap-phase-seconds | 1, 5, 15, 30, 60, 120, 300, 600, 1200, 1800, 3600 |
| workflow-claim-run-seconds | 0.1, 0.5, 1, 5, 15, 30, 60, 120, 300, 600, 1200, 1800 |
| pipeline-overlap-seconds | 1, 5, 10, 30, 60, 120, 300, 600, 1800 |
| shutdown-duration-seconds | 0.5, 1, 2.5, 5, 10, 30, 60 |
| relationship-breakdown-permit-wait-seconds | 0, 0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30 |
| deferred-backfill-partition-workers-count | 1, 2, 4, 8, 16, 32 |
| deferred-backfill-fact-count | 0, 10, 50, 100, 250, 500, 1000, 2500, 5000, 10000, 25000, 50000, 100000 |
| reducer-input-invalid-fact-write-batch-size | 0, 1, 2, 5, 10, 25, 50, 100, 250, 500 |
| query-input-invalid-facts-duration-seconds | 0, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10 |
| code-fingerprint-seconds | 0, 0.0001, 0.0005, 0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 5 |
| lock-only-gate-lock-wait-seconds | 0, 0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 5, 10 |
| webhook-claim-wait-seconds | 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300, 600 |
| ingestion-commit-pin-slack-seconds | 0, 1, 2.5, 5, 10, 20, 30, 40, 50, 60, 120 |
