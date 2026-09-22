# #6693 mapping: work queues

Part of the [storage/postgres target tree](../6693-postgres-target-tree.md). Destinations covered: `queue`.

Paths are relative to `go/internal/storage/postgres/`. Each line reads `current -> new`.

### `queue/` (2 non-test, 1 test)

```text
failure_metadata.go -> queue/failure_metadata.go
retry_backoff.go -> queue/backoff.go
```

<details><summary>Tests</summary>

```text
retry_backoff_test.go -> queue/backoff_test.go
```

</details>

### `queue/projector/` (7 non-test, 19 test)

```text
projected_source_edge_store.go -> queue/projector/source_edge.go
projector_queue.go -> queue/projector/projector.go
projector_queue_claim_sql.go -> queue/projector/claim_sql.go
projector_queue_config_state_drift_trigger_hook.go -> queue/projector/config_state_drift_trigger_hook.go
projector_queue_crossplane_redrive_hook.go -> queue/projector/crossplane_redrive_hook.go
projector_queue_scan.go -> queue/projector/scan.go
projector_queue_sql.go -> queue/projector/sql.go
```

<details><summary>Tests</summary>

```text
claim_reclaim_test.go -> queue/projector/claim_reclaim_test.go   # spans queue/projector=57% queue/reducer=43%
projected_source_edge_store_test.go -> queue/projector/source_edge_test.go   # external test package: imports root
projector_queue_ack_lock_timeout_test.go -> queue/projector/ack_lock_timeout_test.go
projector_queue_ack_scope_wait_live_test.go -> queue/projector/ack_scope_wait_live_test.go   # external test package: imports root
projector_queue_attempt_fence_live_test.go -> queue/projector/attempt_fence_live_test.go   # external test package: imports root
projector_queue_attempt_reclaim_contention_live_test.go -> queue/projector/attempt_reclaim_contention_live_test.go   # external test package: imports root
projector_queue_config_state_drift_trigger_hook_latency_test.go -> queue/projector/config_state_drift_trigger_hook_latency_test.go
projector_queue_config_state_drift_trigger_hook_test.go -> queue/projector/config_state_drift_trigger_hook_test.go
projector_queue_crossplane_redrive_hook_test.go -> queue/projector/crossplane_redrive_hook_test.go
projector_queue_heartbeat_scope_lock_live_test.go -> queue/projector/heartbeat_scope_lock_live_test.go   # external test package: imports root
projector_queue_heartbeat_test.go -> queue/projector/heartbeat_test.go
projector_queue_ingestion_lock_order_live_test.go -> queue/projector/ingestion_lock_order_live_test.go   # external test package: imports root
projector_queue_lifecycle_test.go -> queue/projector/lifecycle_test.go
projector_queue_supersession_live_test.go -> queue/projector/supersession_live_test.go   # external test package: imports root
projector_queue_supersession_test.go -> queue/projector/supersession_test.go
proof_domain_cloud_asset_test.go -> queue/projector/proof_domain_cloud_asset_test.go   # external test package: imports facts, ingestion
proof_domain_support_projector_test.go -> queue/projector/proof_domain_support_test.go   # external test package: imports facts
proof_domain_test.go -> queue/projector/proof_domain_test.go   # external test package: imports facts, ingestion, status
work_queue_test.go -> queue/projector/work_test.go
```

</details>

### `queue/reducer/` (13 non-test, 98 test)

```text
queue_observer.go -> queue/reducer/observer.go
reducer_generation_filter_sql.go -> queue/reducer/generation_filter_sql.go
reducer_graph_drain.go -> queue/reducer/graph_drain.go
reducer_queue.go -> queue/reducer/reducer.go
reducer_queue_ack.go -> queue/reducer/ack.go
reducer_queue_batch.go -> queue/reducer/batch.go
reducer_queue_batch_query.go -> queue/reducer/batch_query.go
reducer_queue_claim_query.go -> queue/reducer/claim_query.go
reducer_queue_conflict.go -> queue/reducer/conflict.go
reducer_queue_helpers.go -> queue/reducer/helpers.go
reducer_queue_readiness_sql.go -> queue/reducer/readiness_sql.go
reducer_queue_replay.go -> queue/reducer/replay.go
reducer_queue_validation.go -> queue/reducer/validation.go
```

<details><summary>Tests</summary>

```text
aws_cloud_runtime_drift_elapsed_bound_queue_test.go -> queue/reducer/aws_cloud_runtime_drift_elapsed_bound_test.go
aws_cloud_runtime_drift_reopen_anchor_live_test.go -> queue/reducer/aws_cloud_runtime_drift_reopen_anchor_live_test.go   # external test package + export_test.go shim: imports root
container_image_identity_ack_array_theory_live_test.go -> queue/reducer/container_image_identity_ack_array_theory_live_test.go
container_image_identity_ack_attempt_fencing_live_test.go -> queue/reducer/container_image_identity_ack_attempt_fencing_live_test.go   # external test package: imports root
container_image_identity_ack_batch_exactness_live_test.go -> queue/reducer/container_image_identity_ack_batch_exactness_live_test.go   # external test package: imports root
container_image_identity_ack_capability_reset_live_test.go -> queue/reducer/container_image_identity_ack_capability_reset_live_test.go   # external test package: imports root
container_image_identity_ack_capability_test.go -> queue/reducer/container_image_identity_ack_capability_test.go
container_image_identity_ack_fixed_schema_performance_live_test.go -> queue/reducer/container_image_identity_ack_fixed_schema_performance_live_test.go   # external test package + export_test.go shim: imports root
container_image_identity_ack_ordering_postgres_live_test.go -> queue/reducer/container_image_identity_ack_ordering_postgres_live_test.go   # external test package: imports root
container_image_identity_claim_epoch_fencing_live_test.go -> queue/reducer/container_image_identity_claim_epoch_fencing_live_test.go   # external test package: imports root
container_image_identity_claim_fixed_schema_performance_live_test.go -> queue/reducer/container_image_identity_claim_fixed_schema_performance_live_test.go   # external test package: imports root
container_image_identity_claim_latch_performance_live_test.go -> queue/reducer/container_image_identity_claim_latch_performance_live_test.go   # external test package + export_test.go shim: imports root
container_image_identity_claim_latch_service_live_test.go -> queue/reducer/container_image_identity_claim_latch_service_live_test.go   # external test package: imports root
container_image_identity_claim_legacy_performance_helpers_live_test.go -> queue/reducer/container_image_identity_claim_legacy_performance_helpers_live_test.go
container_image_identity_claim_performance_helpers_live_test.go -> queue/reducer/container_image_identity_claim_performance_helpers_live_test.go
container_image_identity_claim_trigger_live_test.go -> queue/reducer/container_image_identity_claim_trigger_live_test.go   # external test package + export_test.go shim: imports root
container_image_identity_cloud_reopen_ordering_postgres_live_test.go -> queue/reducer/container_image_identity_cloud_reopen_ordering_postgres_live_test.go   # external test package: imports ingestion, root
container_image_identity_failure_fencing_live_test.go -> queue/reducer/container_image_identity_failure_fencing_live_test.go   # external test package: imports root
container_image_identity_failure_fencing_test.go -> queue/reducer/container_image_identity_failure_fencing_test.go
cross_scope_completion_ack_atomic_postgres_live_test.go -> queue/reducer/cross_scope_completion_ack_atomic_postgres_live_test.go   # external test package: imports root
cross_scope_completion_ack_horizon_postgres_live_test.go -> queue/reducer/cross_scope_completion_ack_horizon_postgres_live_test.go   # external test package: imports root
cross_scope_completion_performance_live_test.go -> queue/reducer/cross_scope_completion_performance_live_test.go   # external test package + export_test.go shim: imports root
cross_scope_completion_postgres_live_test.go -> queue/reducer/cross_scope_completion_postgres_live_test.go   # external test package: imports root
cross_scope_completion_scale_postgres_live_test.go -> queue/reducer/cross_scope_completion_scale_postgres_live_test.go   # external test package: imports root
cross_scope_completion_workflow_image_postgres_live_test.go -> queue/reducer/cross_scope_completion_workflow_image_postgres_live_test.go   # external test package: imports root
provenance_edge_identity_upgrade_postgres_live_test.go -> queue/reducer/provenance_edge_identity_upgrade_postgres_live_test.go   # external test package: imports root
queue_observer_graph_write_timeout_test.go -> queue/reducer/observer_graph_write_timeout_test.go
queue_observer_test.go -> queue/reducer/observer_test.go   # external test package + export_test.go shim: imports root
readiness_wait_supersession_live_test.go -> queue/reducer/readiness_wait_supersession_live_test.go   # external test package: imports root
recovery_claim_token_fence_live_test.go -> queue/reducer/recovery_claim_token_fence_live_test.go   # external test package + export_test.go shim: imports recovery, root; follows its private symbols, not its name
recovery_refinalize_rebuild_reset_live_helpers_test.go -> queue/reducer/recovery_refinalize_rebuild_reset_live_helpers_test.go   # external test package + export_test.go shim: imports root; follows its private symbols, not its name
reducer_graph_drain_quiescence_live_test.go -> queue/reducer/graph_drain_quiescence_live_test.go   # external test package: imports root
reducer_graph_drain_test.go -> queue/reducer/graph_drain_test.go
reducer_queue_ack_eligible_epq_postgres_live_test.go -> queue/reducer/ack_eligible_epq_postgres_live_test.go
reducer_queue_ack_fanout_contention_probe_test.go -> queue/reducer/ack_fanout_contention_probe_test.go
reducer_queue_ack_fanout_telemetry_test.go -> queue/reducer/ack_fanout_telemetry_test.go   # external test package: imports root
reducer_queue_ack_fk_compatibility_postgres_live_test.go -> queue/reducer/ack_fk_compatibility_postgres_live_test.go
reducer_queue_ack_lock_order_postgres_live_test.go -> queue/reducer/ack_lock_order_postgres_live_test.go
reducer_queue_aws_cloud_runtime_drift_readiness_test.go -> queue/reducer/aws_cloud_runtime_drift_readiness_test.go
reducer_queue_aws_readiness_test.go -> queue/reducer/aws_readiness_test.go
reducer_queue_basic_test.go -> queue/reducer/basic_test.go
reducer_queue_batch_ack_reclaim_test.go -> queue/reducer/batch_ack_reclaim_test.go
reducer_queue_batch_ack_split_bench_test.go -> queue/reducer/batch_ack_split_bench_test.go
reducer_queue_batch_latency_gate_test.go -> queue/reducer/batch_latency_gate_test.go
reducer_queue_batch_lock_recheck_test.go -> queue/reducer/batch_lock_recheck_test.go
reducer_queue_batch_rank_once_derive_test.go -> queue/reducer/batch_rank_once_derive_test.go
reducer_queue_batch_rank_once_diff_test.go -> queue/reducer/batch_rank_once_diff_test.go
reducer_queue_batch_rank_once_fixtures_query_test.go -> queue/reducer/batch_rank_once_fixtures_query_test.go
reducer_queue_batch_rank_once_shape_test.go -> queue/reducer/batch_rank_once_shape_test.go
reducer_queue_batch_test.go -> queue/reducer/batch_test.go
reducer_queue_claim_domains_test.go -> queue/reducer/claim_domains_test.go
reducer_queue_claim_readiness_bench_test.go -> queue/reducer/claim_readiness_bench_test.go
reducer_queue_clock_seam_test.go -> queue/reducer/clock_seam_test.go   # external test package + export_test.go shim: imports root
reducer_queue_cloud_admission_readiness_test.go -> queue/reducer/cloud_admission_readiness_test.go
reducer_queue_conflict_claim_proof_test.go -> queue/reducer/conflict_claim_proof_test.go   # external test package + export_test.go shim: imports root
reducer_queue_contention_gate_test.go -> queue/reducer/contention_gate_test.go   # external test package + export_test.go shim: imports root
reducer_queue_contention_holder_test.go -> queue/reducer/contention_holder_test.go   # external test package + export_test.go shim: imports root
reducer_queue_cross_scope_readiness_gate_coverage_test.go -> queue/reducer/cross_scope_readiness_gate_coverage_test.go
reducer_queue_cross_scope_readiness_live_test.go -> queue/reducer/cross_scope_readiness_live_test.go   # external test package: imports root
reducer_queue_cross_scope_readiness_test.go -> queue/reducer/cross_scope_readiness_test.go
reducer_queue_ec2_block_device_kms_posture_readiness_test.go -> queue/reducer/ec2_block_device_kms_posture_readiness_test.go
reducer_queue_ec2_instance_identity_readiness_test.go -> queue/reducer/ec2_instance_identity_readiness_test.go
reducer_queue_ec2_instance_node_readiness_test.go -> queue/reducer/ec2_instance_node_readiness_test.go
reducer_queue_ec2_internet_exposure_readiness_test.go -> queue/reducer/ec2_internet_exposure_readiness_test.go
reducer_queue_ec2_uses_profile_readiness_test.go -> queue/reducer/ec2_uses_profile_readiness_test.go
reducer_queue_gcp_relationship_readiness_test.go -> queue/reducer/gcp_relationship_readiness_test.go
reducer_queue_heartbeat_test.go -> queue/reducer/heartbeat_test.go
reducer_queue_iam_can_assume_readiness_test.go -> queue/reducer/iam_can_assume_readiness_test.go
reducer_queue_iam_instance_profile_role_readiness_test.go -> queue/reducer/iam_instance_profile_role_readiness_test.go
reducer_queue_iam_permission_readiness_test.go -> queue/reducer/iam_permission_readiness_test.go
reducer_queue_kubernetes_correlation_readiness_test.go -> queue/reducer/kubernetes_correlation_readiness_test.go
reducer_queue_minimal_claim_schema_test.go -> queue/reducer/minimal_claim_schema_test.go
reducer_queue_observability_coverage_readiness_test.go -> queue/reducer/observability_coverage_readiness_test.go
reducer_queue_payload_test.go -> queue/reducer/payload_test.go
reducer_queue_platform_graph_partition_bench_test.go -> queue/reducer/platform_graph_partition_bench_test.go
reducer_queue_platform_graph_partition_test.go -> queue/reducer/platform_graph_partition_test.go
reducer_queue_rds_posture_readiness_test.go -> queue/reducer/rds_posture_readiness_test.go
reducer_queue_readiness_claim_gate_test.go -> queue/reducer/readiness_claim_gate_test.go
reducer_queue_readiness_enrollment_behavior_test.go -> queue/reducer/readiness_enrollment_behavior_test.go
reducer_queue_readiness_enrollment_test.go -> queue/reducer/readiness_enrollment_test.go
reducer_queue_readiness_lookup_test.go -> queue/reducer/readiness_lookup_test.go
reducer_queue_replay_test.go -> queue/reducer/replay_test.go
reducer_queue_resource_conflict_test.go -> queue/reducer/resource_conflict_test.go
reducer_queue_resource_node_fencing_test.go -> queue/reducer/resource_node_fencing_test.go
reducer_queue_s3_external_principal_grant_readiness_test.go -> queue/reducer/s3_external_principal_grant_readiness_test.go
reducer_queue_s3_internet_exposure_readiness_test.go -> queue/reducer/s3_internet_exposure_readiness_test.go
reducer_queue_s3_logs_to_readiness_test.go -> queue/reducer/s3_logs_to_readiness_test.go
reducer_queue_secrets_iam_readiness_test.go -> queue/reducer/secrets_iam_readiness_test.go
reducer_queue_security_group_reachability_readiness_test.go -> queue/reducer/security_group_reachability_readiness_test.go
reducer_queue_semantic_claim_limit_test.go -> queue/reducer/semantic_claim_limit_test.go
reducer_queue_supersession_test.go -> queue/reducer/supersession_test.go
reducer_queue_test.go -> queue/reducer/reducer_test.go
reducer_queue_test_helpers_test.go -> queue/reducer/test_helpers_test.go
reducer_queue_workload_cloud_relationship_readiness_test.go -> queue/reducer/workload_cloud_relationship_readiness_test.go
reducer_queue_workload_replay_live_test.go -> queue/reducer/workload_replay_live_test.go   # external test package: imports root
reducer_queue_workload_replay_schedule_test.go -> queue/reducer/workload_replay_schedule_test.go
reducer_queue_workload_replay_test.go -> queue/reducer/workload_replay_test.go
value_flow_refresh_fanout_live_test.go -> queue/reducer/value_flow_refresh_fanout_live_test.go   # external test package: imports root
```

</details>
