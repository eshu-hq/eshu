# #6693 mapping: ingestion, relationships, intents, locks and scope

Part of the [storage/postgres target tree](../6693-postgres-target-tree.md). Destinations covered: `ingestion`, `relationship`, `intent`, `lock`, `scope`.

Paths are relative to `go/internal/storage/postgres/`. Each line reads `current -> new`.

### `ingestion/` (31 non-test, 78 test)

```text
deferred_backfill_partition_memo.go -> ingestion/deferred_backfill_partition_memo.go
deferred_maintenance_barrier.go -> ingestion/deferred_maintenance_barrier.go
deferred_maintenance_barrier_stall.go -> ingestion/deferred_maintenance_barrier_stall.go
drift_catchup_lister.go -> ingestion/drift_catchup_lister.go
drift_enqueue.go -> ingestion/drift_enqueue.go
drift_runtime_trigger.go -> ingestion/drift_runtime_trigger.go
generation_freshness.go -> ingestion/generation_freshness.go
generation_projected_commit.go -> ingestion/generation_projected_commit.go
iac_reachability_materializer.go -> ingestion/iac_reachability_materializer.go
ingestion.go -> ingestion/store.go
ingestion_backfill.go -> ingestion/backfill.go
ingestion_backfill_argocd_config.go -> ingestion/backfill_argocd_config.go
ingestion_backfill_deferred_facts.go -> ingestion/backfill_deferred_facts.go
ingestion_backfill_deferred_predicate.go -> ingestion/backfill_deferred_predicate.go
ingestion_backfill_deferred_regex.go -> ingestion/backfill_deferred_regex.go
ingestion_backfill_generation_guard.go -> ingestion/backfill_generation_guard.go
ingestion_backfill_partition_memo_fingerprint.go -> ingestion/backfill_partition_memo_fingerprint.go
ingestion_backfill_partition_memo_gate.go -> ingestion/backfill_partition_memo_gate.go
ingestion_backfill_per_commit.go -> ingestion/backfill_per_commit.go
ingestion_backfill_pool.go -> ingestion/backfill_pool.go
ingestion_backfill_scoped_facts.go -> ingestion/backfill_scoped_facts.go
ingestion_backfill_scoped_load.go -> ingestion/backfill_scoped_load.go
ingestion_catalog_cache.go -> ingestion/catalog_cache.go
ingestion_catalog_parse.go -> ingestion/catalog_parse.go
ingestion_flux_cross_repo_telemetry.go -> ingestion/flux_cross_repo_telemetry.go
ingestion_queries.go -> ingestion/queries.go
ingestion_reopen_code_import.go -> ingestion/reopen_code_import.go
ingestion_reopen_correlation.go -> ingestion/reopen_correlation.go
ingestion_reopen_deployment_mapping.go -> ingestion/reopen_deployment_mapping.go
ingestion_reopen_partition_memo_gate.go -> ingestion/reopen_partition_memo_gate.go
ingestion_stream_error.go -> ingestion/stream_error.go
```

<details><summary>Tests</summary>

```text
aws_cloud_no_plaintext_persistence_test.go -> ingestion/aws_cloud_no_plaintext_persistence_test.go
deferred_backfill_partition_memo_test.go -> ingestion/deferred_backfill_partition_memo_test.go
deferred_maintenance_barrier_join_only_test.go -> ingestion/deferred_maintenance_barrier_join_only_test.go
deferred_maintenance_barrier_quiet_fleet_test.go -> ingestion/deferred_maintenance_barrier_quiet_fleet_test.go
deferred_maintenance_barrier_reentry_test.go -> ingestion/deferred_maintenance_barrier_reentry_test.go
deferred_maintenance_barrier_stall_test.go -> ingestion/deferred_maintenance_barrier_stall_test.go
deferred_maintenance_concurrency_test.go -> ingestion/deferred_maintenance_concurrency_test.go
drift_catchup_lister_test.go -> ingestion/drift_catchup_lister_test.go
drift_enqueue_test.go -> ingestion/drift_enqueue_test.go
generation_freshness_test.go -> ingestion/generation_freshness_test.go
generation_projected_commit_test.go -> ingestion/generation_projected_commit_test.go
generation_reconciliation_test.go -> ingestion/generation_reconciliation_test.go
ingestion_backfill_argocd_config_test.go -> ingestion/backfill_argocd_config_test.go
ingestion_backfill_bench_test.go -> ingestion/backfill_bench_test.go
ingestion_backfill_concurrency_test.go -> ingestion/backfill_concurrency_test.go
ingestion_backfill_deferred_facts_hoist_compare_test.go -> ingestion/backfill_deferred_facts_hoist_compare_test.go
ingestion_backfill_deferred_facts_hoist_test.go -> ingestion/backfill_deferred_facts_hoist_test.go   # external test package + export_test.go shim: imports root
ingestion_backfill_deferred_regex_test.go -> ingestion/backfill_deferred_regex_test.go
ingestion_backfill_deferred_scope_test.go -> ingestion/backfill_deferred_scope_test.go
ingestion_backfill_fanin_publication_test.go -> ingestion/backfill_fanin_publication_test.go   # external test package + export_test.go shim: imports root
ingestion_backfill_fanin_recovery_test.go -> ingestion/backfill_fanin_recovery_test.go   # external test package + export_test.go shim: imports root
ingestion_backfill_fanin_telemetry_test.go -> ingestion/backfill_fanin_telemetry_test.go
ingestion_backfill_flux_cross_repo_ordering_live_test.go -> ingestion/backfill_flux_cross_repo_ordering_live_test.go   # external test package: imports root
ingestion_backfill_gcp_test.go -> ingestion/backfill_gcp_test.go
ingestion_backfill_generation_guard_test.go -> ingestion/backfill_generation_guard_test.go
ingestion_backfill_load_concurrency_test.go -> ingestion/backfill_load_concurrency_test.go
ingestion_backfill_partition_integration_test.go -> ingestion/backfill_partition_integration_test.go   # external test package + export_test.go shim: imports root
ingestion_backfill_partition_memo_argocd_signal_test.go -> ingestion/backfill_partition_memo_argocd_signal_test.go   # external test package + export_test.go shim: imports root
ingestion_backfill_partition_memo_determinism_test.go -> ingestion/backfill_partition_memo_determinism_test.go   # external test package + export_test.go shim: imports root
ingestion_backfill_partition_memo_fingerprint_test.go -> ingestion/backfill_partition_memo_fingerprint_test.go
ingestion_backfill_partition_memo_integration_test.go -> ingestion/backfill_partition_memo_integration_test.go   # external test package + export_test.go shim: imports root
ingestion_backfill_partition_memo_proof_helpers_test.go -> ingestion/backfill_partition_memo_proof_helpers_test.go
ingestion_backfill_relationship_family_binary_proof_contract_test.go -> ingestion/backfill_relationship_family_binary_proof_contract_test.go
ingestion_backfill_relationship_family_binary_proof_helpers_test.go -> ingestion/backfill_relationship_family_binary_proof_helpers_test.go   # external test package + export_test.go shim: imports root
ingestion_backfill_relationship_family_binary_proof_inputs_test.go -> ingestion/backfill_relationship_family_binary_proof_inputs_test.go
ingestion_backfill_relationship_family_binary_proof_manifest_test.go -> ingestion/backfill_relationship_family_binary_proof_manifest_test.go
ingestion_backfill_relationship_family_guard_test.go -> ingestion/backfill_relationship_family_guard_test.go   # external test package + export_test.go shim: imports root
ingestion_backfill_relationship_family_index_migration_live_test.go -> ingestion/backfill_relationship_family_index_migration_live_test.go   # external test package: imports root
ingestion_backfill_relationship_family_index_odu_live_test.go -> ingestion/backfill_relationship_family_index_odu_live_test.go
ingestion_backfill_relationship_family_index_write_tax_live_test.go -> ingestion/backfill_relationship_family_index_write_tax_live_test.go
ingestion_backfill_relationship_family_retained_live_test.go -> ingestion/backfill_relationship_family_retained_live_test.go   # external test package + export_test.go shim: imports root
ingestion_backfill_scoped_facts_test.go -> ingestion/backfill_scoped_facts_test.go
ingestion_backfill_span_attributes_test.go -> ingestion/backfill_span_attributes_test.go
ingestion_backfill_task_telemetry_test.go -> ingestion/backfill_task_telemetry_test.go
ingestion_backfill_test.go -> ingestion/backfill_test.go
ingestion_catalog_cache_bench_test.go -> ingestion/catalog_cache_bench_test.go
ingestion_catalog_cache_remote_url_test.go -> ingestion/catalog_cache_remote_url_test.go
ingestion_catalog_cache_test.go -> ingestion/catalog_cache_test.go
ingestion_catalog_merge_test.go -> ingestion/store_catalog_merge_test.go
ingestion_derived_evidence_fencing_proof_test.go -> ingestion/store_derived_evidence_fencing_proof_test.go   # external test package: imports root
ingestion_flux_cross_repo_evidence_integration_test.go -> ingestion/store_flux_cross_repo_evidence_integration_test.go
ingestion_flux_cross_repo_telemetry_test.go -> ingestion/flux_cross_repo_telemetry_test.go
ingestion_freshness_test.go -> ingestion/store_freshness_test.go
ingestion_gcp_relationship_test.go -> ingestion/store_gcp_relationship_test.go
ingestion_published_generation_recommit_live_test.go -> ingestion/store_published_generation_recommit_live_test.go   # external test package + export_test.go shim: imports root
ingestion_relationship_query_test.go -> ingestion/store_relationship_query_test.go
ingestion_reopen_bootstrap_nil_skipset_test.go -> ingestion/store_reopen_bootstrap_nil_skipset_test.go   # external test package: imports root
ingestion_reopen_correlation_cost_proof_test.go -> ingestion/reopen_correlation_cost_proof_test.go   # external test package: imports root
ingestion_reopen_correlation_failed_generation_test.go -> ingestion/reopen_correlation_failed_generation_test.go   # external test package: imports root
ingestion_reopen_correlation_maintenance_hermetic_test.go -> ingestion/reopen_correlation_maintenance_hermetic_test.go
ingestion_reopen_correlation_maintenance_test.go -> ingestion/reopen_correlation_maintenance_test.go   # external test package + export_test.go shim: imports root
ingestion_reopen_correlation_test.go -> ingestion/reopen_correlation_test.go
ingestion_reopen_partition_memo_gate_helpers_test.go -> ingestion/reopen_partition_memo_gate_helpers_test.go   # external test package: imports root
ingestion_reopen_partition_memo_gate_integration_test.go -> ingestion/reopen_partition_memo_gate_integration_test.go   # external test package: imports root
ingestion_reopen_partition_memo_gate_test.go -> ingestion/reopen_partition_memo_gate_test.go
ingestion_scope_source_key_live_test.go -> ingestion/store_scope_source_key_live_test.go   # external test package + export_test.go shim: imports root
ingestion_scope_source_key_test.go -> ingestion/store_scope_source_key_test.go
ingestion_scopes_active_state_snapshot_index_live_test.go -> ingestion/store_scopes_active_state_snapshot_index_live_test.go   # external test package: imports root
ingestion_stream_error_test.go -> ingestion/stream_error_test.go
ingestion_tx_lock_split_deadlock_test.go -> ingestion/store_tx_lock_split_deadlock_test.go   # external test package: imports root
ingestion_tx_lock_split_integration_test.go -> ingestion/store_tx_lock_split_integration_test.go   # external test package: imports root
proof_domain_go_collector_evidence_test.go -> ingestion/proof_domain_go_collector_evidence_test.go
proof_domain_retry_test.go -> ingestion/proof_domain_retry_test.go
proof_domain_terraform_test.go -> ingestion/proof_domain_terraform_test.go
relationship_family_index_schema_test.go -> ingestion/relationship_family_index_schema_test.go   # external test package + export_test.go shim: imports root; follows its private symbols, not its name
tfstate_no_plaintext_persistence_test.go -> ingestion/tfstate_no_plaintext_persistence_test.go
workflow_control_integration_helpers_test.go -> ingestion/workflow_control_integration_helpers_test.go   # external test package + export_test.go shim: imports root; follows its private symbols, not its name
workflow_run_reconciliation_integration_test.go -> ingestion/workflow_run_reconciliation_integration_test.go   # external test package + export_test.go shim: imports root; follows its private symbols, not its name
```

</details>

### `intent/` (11 non-test, 20 test)

```text
code_call_intent_writer.go -> intent/code_call_writer.go
repo_dependency_acceptance_gate.go -> intent/repo_dependency_gate.go
shared_intent_acceptance_writer.go -> intent/acceptance_writer.go
shared_intents.go -> intent/store.go
shared_intents_continuation.go -> intent/continuation.go
shared_intents_first_projection.go -> intent/first_projection.go
shared_intents_history.go -> intent/history.go
shared_intents_partition_candidates.go -> intent/partition_candidates.go
shared_intents_upsert.go -> intent/upsert.go
shared_projection_acceptance.go -> intent/projection_acceptance.go
shared_projection_unroutable_intents.go -> intent/projection_unroutable.go
```

<details><summary>Tests</summary>

```text
code_call_intent_writer_test.go -> intent/code_call_writer_test.go
repo_dependency_acceptance_gate_expiry_test.go -> intent/repo_dependency_gate_expiry_test.go   # external test package + export_test.go shim: imports relationship, root
shared_intents_claim_expiry_binding_test.go -> intent/store_claim_expiry_binding_test.go
shared_intents_claim_lease_rows_error_test.go -> intent/store_claim_lease_rows_error_test.go
shared_intents_code_call_fence_integration_test.go -> intent/store_code_call_fence_integration_test.go   # external test package: imports root
shared_intents_continuation_test.go -> intent/continuation_test.go
shared_intents_first_projection_test.go -> intent/first_projection_test.go   # external test package + export_test.go shim: imports root
shared_intents_history_test.go -> intent/history_test.go
shared_intents_lease_rescale_test.go -> intent/store_lease_rescale_test.go   # external test package + export_test.go shim: imports root
shared_intents_partition_candidates_test.go -> intent/partition_candidates_test.go
shared_intents_partition_test.go -> intent/store_partition_test.go
shared_intents_refresh_fence_live_test.go -> intent/store_refresh_fence_live_test.go   # external test package: imports root
shared_intents_refresh_fence_test.go -> intent/store_refresh_fence_test.go
shared_intents_schema_partition_test.go -> intent/store_schema_partition_test.go
shared_intents_test.go -> intent/store_test.go
shared_projection_acceptance_rowcount_test.go -> intent/projection_acceptance_rowcount_test.go
shared_projection_acceptance_test.go -> intent/projection_acceptance_test.go
shared_projection_lease_blocked_claim_expiry_proof_test.go -> intent/shared_projection_lease_blocked_claim_expiry_proof_test.go   # external test package: imports root
shared_projection_lease_heartbeat_proof_test.go -> intent/shared_projection_lease_heartbeat_proof_test.go   # external test package: imports root
shared_projection_unroutable_intents_test.go -> intent/projection_unroutable_test.go
```

</details>

### `lock/` (3 non-test, 3 test)

```text
deferred_maintenance_lock.go -> lock/deferred_maintenance.go
package_registry_identity_locker.go -> lock/package_registry_identity.go
platform_graph_locker.go -> lock/platform_graph.go
```

<details><summary>Tests</summary>

```text
package_registry_identity_locker_test.go -> lock/package_registry_identity_test.go
platform_graph_locker_test.go -> lock/platform_graph_test.go
shared_intent_acceptance_writer_test.go -> lock/shared_intent_acceptance_writer_test.go   # external test package + export_test.go shim: imports intent; follows its private symbols, not its name
```

</details>

### `relationship/` (6 non-test, 9 test)

```text
accepted_generation.go -> relationship/accepted_generation.go
relationship_evidence_batch.go -> relationship/evidence_batch.go
relationship_reference_keys.go -> relationship/reference_keys.go
relationship_schema.go -> relationship/schema.go
relationship_store.go -> relationship/store.go
relationship_store_resolved.go -> relationship/resolved.go
```

<details><summary>Tests</summary>

```text
accepted_generation_test.go -> relationship/accepted_generation_test.go
relationship_evidence_upsert_streaming_delta_bench_test.go -> relationship/evidence_upsert_streaming_delta_bench_test.go
relationship_generation_active_test.go -> relationship/generation_active_test.go
relationship_generations_complete_test.go -> relationship/generations_complete_test.go
relationship_reference_keys_test.go -> relationship/reference_keys_test.go
relationship_store_batch_test.go -> relationship/store_batch_test.go
relationship_store_generation_test.go -> relationship/store_generation_test.go
relationship_store_nullable_test.go -> relationship/store_nullable_test.go
relationship_store_test.go -> relationship/store_test.go
```

</details>

### `scope/completion/` (4 non-test, 6 test)

```text
cross_scope_completion_fanout.go -> scope/completion/fanout.go
cross_scope_completion_queue.go -> scope/completion/queue.go
cross_scope_producer_readiness.go -> scope/completion/producer_readiness.go
scope_quiescence.go -> scope/completion/quiescence.go
```

<details><summary>Tests</summary>

```text
cross_scope_completion_concurrency_postgres_live_test.go -> scope/completion/cross_concurrency_postgres_live_test.go   # external test package + export_test.go shim: imports root
cross_scope_completion_snapshot_postgres_live_test.go -> scope/completion/cross_snapshot_postgres_live_test.go   # external test package: imports root
cross_scope_producer_readiness_test.go -> scope/completion/producer_readiness_test.go
reducer_queue_ack_scale_plan_test.go -> scope/completion/reducer_queue_ack_scale_plan_test.go   # external test package + export_test.go shim: imports root; follows its private symbols, not its name
scope_quiescence_live_test.go -> scope/completion/quiescence_live_test.go   # external test package: imports root
scope_quiescence_test.go -> scope/completion/quiescence_test.go
```

</details>
