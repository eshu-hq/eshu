# #6693 mapping: status, generations, workflow and recovery

Part of the [storage/postgres target tree](../6693-postgres-target-tree.md). Destinations covered: `status`, `generation`, `workflow`, `recovery`, `admission`, `collector`, `db`, `cicd`, `decisions`, `maintenance`.

Paths are relative to `go/internal/storage/postgres/`. Each line reads `current -> new`.

### `admission/` (3 non-test, 4 test)

```text
admission_decisions.go -> admission/decisions.go
admission_decisions_validation.go -> admission/validation.go
schema_admission_decisions.go -> admission/schema.go
```

<details><summary>Tests</summary>

```text
admission_decisions_evidence_test.go -> admission/decisions_evidence_test.go
admission_decisions_test.go -> admission/decisions_test.go
admission_decisions_test_helpers_test.go -> admission/decisions_test_helpers_test.go
admission_decisions_validation_test.go -> admission/validation_test.go
```

</details>

### `cicd/` (1 non-test, 1 test)

```text
cicd_run_watermark.go -> cicd/watermark.go
```

<details><summary>Tests</summary>

```text
cicd_run_watermark_test.go -> cicd/watermark_test.go
```

</details>

### `collector/` (4 non-test, 2 test)

```text
collector_evidence_summary.go -> collector/evidence_summary.go
collector_generation_dead_letter.go -> collector/dead_letter.go
collector_generation_dead_letter_status.go -> collector/dead_letter_status.go
status_collector_evidence.go -> collector/evidence_status.go
```

<details><summary>Tests</summary>

```text
collector_evidence_summary_test.go -> collector/evidence_summary_test.go
collector_generation_dead_letter_test.go -> collector/dead_letter_test.go
```

</details>

### `db/` (3 non-test, 2 test)

```text
instrumented.go -> db/instrumented.go
instrumented_transaction.go -> db/instrumented_transaction.go
read_snapshot.go -> db/read_snapshot.go
```

<details><summary>Tests</summary>

```text
instrumented_test.go -> db/instrumented_test.go
instrumented_transaction_test.go -> db/instrumented_transaction_test.go
```

</details>

### `decisions/` (1 non-test, 1 test)

```text
decisions.go -> decisions/decisions.go
```

<details><summary>Tests</summary>

```text
decisions_test.go -> decisions/decisions_test.go
```

</details>

### `generation/` (11 non-test, 21 test)

```text
generation_liveness.go -> generation/liveness.go
generation_liveness_sql.go -> generation/liveness_sql.go
generation_retention.go -> generation/retention.go
generation_retention_schema.go -> generation/retention_schema.go
generation_retention_sql.go -> generation/retention_sql.go
graph_projection_phase_repair_queue.go -> generation/graph_projection_phase_repair_queue.go
graph_projection_phase_state.go -> generation/graph_projection_phase_state.go
latest_generation_cte.go -> generation/latest_cte.go
poison_liveness.go -> generation/poison_liveness.go
poison_liveness_sql.go -> generation/poison_liveness_sql.go
repo_scope_resolver.go -> generation/repo_scope_resolver.go
```

<details><summary>Tests</summary>

```text
facts_cicd_run_history_unpublished_predecessor_live_test.go -> generation/facts_cicd_run_history_unpublished_predecessor_live_test.go   # external test package + export_test.go shim: imports facts, root; follows its private symbols, not its name
generation_liveness_integration_helpers_test.go -> generation/liveness_integration_helpers_test.go
generation_liveness_integration_test.go -> generation/liveness_integration_test.go   # external test package: imports root
generation_liveness_recovery_inflight_fixture_test.go -> generation/liveness_recovery_inflight_fixture_test.go
generation_liveness_repo_dependency_integration_test.go -> generation/liveness_repo_dependency_integration_test.go   # external test package: imports root
generation_liveness_shared_backlog_fixture_test.go -> generation/liveness_shared_backlog_fixture_test.go
generation_liveness_test.go -> generation/liveness_test.go
generation_liveness_write_time_race_test.go -> generation/liveness_write_time_race_test.go   # external test package: imports root
generation_retention_bench_test.go -> generation/retention_bench_test.go
generation_retention_infra_count_test.go -> generation/retention_infra_count_test.go   # external test package + export_test.go shim: imports root
generation_retention_integration_test.go -> generation/retention_integration_test.go   # external test package + export_test.go shim: imports root
generation_retention_skip_cap_test.go -> generation/retention_skip_cap_test.go
generation_retention_test.go -> generation/retention_test.go
graph_projection_phase_repair_queue_test.go -> generation/graph_projection_phase_repair_queue_test.go
graph_projection_phase_state_test.go -> generation/graph_projection_phase_state_test.go   # external test package: imports root
latest_generation_cte_integration_test.go -> generation/latest_cte_integration_test.go
poison_liveness_integration_helpers_test.go -> generation/poison_liveness_integration_helpers_test.go
poison_liveness_integration_test.go -> generation/poison_liveness_integration_test.go   # external test package + export_test.go shim: imports root
poison_liveness_write_time_race_test.go -> generation/poison_liveness_write_time_race_test.go   # external test package: imports root
projector_stranded_retry_recovery_test.go -> generation/projector_stranded_retry_recovery_test.go   # external test package: imports root
reducer_queue_supersede_readiness_test.go -> generation/reducer_queue_supersede_readiness_test.go   # external test package + export_test.go shim: imports root; follows its private symbols, not its name
```

</details>

### `maintenance/` (1 non-test, 1 test)

```text
status_requests.go -> maintenance/requests.go
```

<details><summary>Tests</summary>

```text
status_requests_test.go -> maintenance/requests_test.go   # external test package + export_test.go shim: imports root (BootstrapDefinitions); found in the maintenance/ move
```

</details>

### `recovery/` (1 non-test, 7 test)

```text
recovery.go -> recovery/recovery.go
```

<details><summary>Tests</summary>

```text
recovery_refinalize_all_scopes_test.go -> recovery/refinalize_all_scopes_test.go
recovery_refinalize_claim_fence_live_test.go -> recovery/refinalize_claim_fence_live_test.go   # external test package: imports root
recovery_refinalize_inflight_fence_test.go -> recovery/refinalize_inflight_fence_test.go   # external test package + export_test.go shim: imports root
recovery_refinalize_rebuild_guard_live_test.go -> recovery/refinalize_rebuild_guard_live_test.go   # external test package: imports root
recovery_refinalize_rebuild_reset_core_test.go -> recovery/refinalize_rebuild_reset_core_test.go   # external test package: imports root
recovery_replay_limit_test.go -> recovery/replay_limit_test.go
recovery_test.go -> recovery/recovery_test.go
```

</details>

### `status/` (17 non-test, 20 test)

```text
changed_since.go -> status/changed_since.go
changed_since_sql.go -> status/changed_since_sql.go
generation_lifecycle.go -> status/generation_lifecycle.go
generation_lifecycle_sql.go -> status/generation_lifecycle_sql.go
service_changed_since.go -> status/service_changed_since.go
service_changed_since_sql.go -> status/service_changed_since_sql.go
status.go -> status/status.go
status_active_work_summary.go -> status/active_work_summary.go
status_aws_cloud.go -> status/aws_cloud.go
status_aws_freshness.go -> status/aws_freshness.go
status_blockage.go -> status/blockage.go
status_operations.go -> status/operations.go
status_producer_activity.go -> status/producer_activity.go
status_queries.go -> status/queries.go
status_read_telemetry.go -> status/read_telemetry.go
status_registry.go -> status/registry.go
workflow_status.go -> status/workflow.go
```

<details><summary>Tests</summary>

```text
changed_since_multiset_live_test.go -> status/changed_since_multiset_live_test.go
changed_since_test.go -> status/changed_since_test.go
generation_lifecycle_grant_test.go -> status/generation_lifecycle_grant_test.go
generation_lifecycle_test.go -> status/generation_lifecycle_test.go
proof_domain_active_work_test.go -> status/proof_domain_active_work_test.go
service_changed_since_incidents_test.go -> status/service_changed_since_incidents_test.go
service_changed_since_test.go -> status/service_changed_since_test.go
service_changed_since_vulnerabilities_test.go -> status/service_changed_since_vulnerabilities_test.go
status_active_generation_index_bench_test.go -> status/active_generation_index_bench_test.go   # external test package + export_test.go shim: imports root
status_aws_cloud_test.go -> status/aws_cloud_test.go
status_aws_freshness_test.go -> status/aws_freshness_test.go
status_blockage_plan_test.go -> status/blockage_plan_test.go
status_blockage_test.go -> status/blockage_test.go
status_inactive_generation_test.go -> status/inactive_generation_test.go
status_instrumented_test.go -> status/instrumented_test.go
status_operations_generation_state_test.go -> status/operations_generation_state_test.go
status_operations_test.go -> status/operations_test.go
status_producer_activity_test.go -> status/producer_activity_test.go
status_readiness_test.go -> status/readiness_test.go   # spans root=50% status=50%
workflow_status_test.go -> status/workflow_test.go
```

</details>

### `workflow/` (10 non-test, 20 test)

```text
workflow_backpressure_status.go -> workflow/backpressure_status.go
workflow_control.go -> workflow/control.go
workflow_control_helpers.go -> workflow/control_helpers.go
workflow_control_open_targets.go -> workflow/control_open_targets.go
workflow_control_release_sql.go -> workflow/control_release_sql.go
workflow_control_schema_sql.go -> workflow/control_schema_sql.go
workflow_control_sql.go -> workflow/control_sql.go
workflow_coordinator_state.go -> workflow/coordinator_state.go
workflow_family_queue_depth.go -> workflow/family_queue_depth.go
workflow_run_reconciliation.go -> workflow/run_reconciliation.go
```

<details><summary>Tests</summary>

```text
ingestion_tenant_grants_test.go -> workflow/ingestion_tenant_grants_test.go   # external test package + export_test.go shim: imports ingestion; follows its private symbols, not its name
workflow_backpressure_status_test.go -> workflow/backpressure_status_test.go
workflow_control_claim_lifecycle_integration_test.go -> workflow/control_claim_lifecycle_integration_test.go
workflow_control_claims_test.go -> workflow/control_claims_test.go
workflow_control_dead_letter_integration_test.go -> workflow/control_dead_letter_integration_test.go   # external test package: imports root
workflow_control_failure_test.go -> workflow/control_failure_test.go
workflow_control_guarded_terminal_test.go -> workflow/control_guarded_terminal_test.go
workflow_control_identity_test.go -> workflow/control_identity_test.go
workflow_control_open_targets_lock_live_test.go -> workflow/control_open_targets_lock_live_test.go   # external test package + export_test.go shim: imports root
workflow_control_open_targets_lock_test.go -> workflow/control_open_targets_lock_test.go
workflow_control_reconcile_integration_test.go -> workflow/control_reconcile_integration_test.go   # external test package: imports root
workflow_control_release_test.go -> workflow/control_release_test.go
workflow_control_resolved_identity_integration_test.go -> workflow/control_resolved_identity_integration_test.go
workflow_control_schema_test.go -> workflow/control_schema_test.go
workflow_control_tenant_boundary_test.go -> workflow/control_tenant_boundary_test.go
workflow_control_test.go -> workflow/control_test.go
workflow_coordinator_state_hosted_collectors_test.go -> workflow/coordinator_state_hosted_collectors_test.go
workflow_coordinator_state_test.go -> workflow/coordinator_state_test.go
workflow_family_queue_depth_test.go -> workflow/family_queue_depth_test.go
workflow_run_reconciliation_test.go -> workflow/run_reconciliation_test.go
```

</details>
