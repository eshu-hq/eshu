# #6693 mapping: root, deletions and the undecided file

Part of the [storage/postgres target tree](../6693-postgres-target-tree.md). Destinations covered: root, DELETE, UNDECIDED.

Paths are relative to `go/internal/storage/postgres/`. Each line reads `current -> new`.

### `root (stays)` (4 non-test, 104 test)

```text
adapters.go -> adapters.go
doc.go -> doc.go
schema.go -> schema.go
schema_bootstrap_lock.go -> schema_bootstrap_lock.go
```

<details><summary>Tests</summary>

```text
aws_bindings_test.go -> aws_bindings_test.go   # no production references
aws_cloud_runtime_drift_admission_live_helpers_test.go -> aws_cloud_runtime_drift_admission_live_helpers_test.go
aws_cloud_runtime_drift_value_collapse_live_test.go -> aws_cloud_runtime_drift_value_collapse_live_test.go   # spans root=50% cloud/aws/drift=50%
cloud_resource_owner_page_index_live_test.go -> cloud_resource_owner_page_index_live_test.go
cloud_resource_owner_page_index_schema_test.go -> cloud_resource_owner_page_index_schema_test.go
code_reachability_index_replay_live_test.go -> code_reachability_index_replay_live_test.go   # spans root=100%; follows its private symbols, not its name
collector_evidence_summary_schema_test.go -> collector_evidence_summary_schema_test.go
container_image_identity_ack_capability_live_helpers_test.go -> container_image_identity_ack_capability_live_helpers_test.go   # no production references
container_image_identity_ack_capability_live_test.go -> container_image_identity_ack_capability_live_test.go   # follows its private symbols, not its name
container_image_identity_ack_legacy_performance_live_test.go -> container_image_identity_ack_legacy_performance_live_test.go   # no production references
container_image_identity_ack_ordering_helpers_postgres_live_test.go -> container_image_identity_ack_ordering_helpers_postgres_live_test.go   # no production references
container_image_identity_ack_performance_helpers_live_test.go -> container_image_identity_ack_performance_helpers_live_test.go   # no production references
container_image_identity_ack_performance_live_test.go -> container_image_identity_ack_performance_live_test.go   # no production references
container_image_identity_cloud_reopen_ordering_postgres_live_helpers_test.go -> container_image_identity_cloud_reopen_ordering_postgres_live_helpers_test.go
container_image_identity_cutover_backfill_live_test.go -> container_image_identity_cutover_backfill_live_test.go   # spans root=100%; follows its private symbols, not its name
container_image_identity_cutover_migration_live_test.go -> container_image_identity_cutover_migration_live_test.go   # spans root=100%; follows its private symbols, not its name
container_image_identity_fence_live_helpers_test.go -> container_image_identity_fence_live_helpers_test.go
container_image_identity_legacy_cleanup_live_test.go -> container_image_identity_legacy_cleanup_live_test.go   # spans root=50% container/image=50%
container_image_identity_marker_lock_performance_live_test.go -> container_image_identity_marker_lock_performance_live_test.go   # no production references
container_image_identity_v3_capability_test.go -> container_image_identity_v3_capability_test.go   # SPLIT: reads private symbols of generation, queue/reducer, recovery
container_image_identity_v3_migration_live_test.go -> container_image_identity_v3_migration_live_test.go
content_entities_k8s_select_partial_index_live_test.go -> content_entities_k8s_select_partial_index_live_test.go
content_entities_k8s_select_partial_index_schema_test.go -> content_entities_k8s_select_partial_index_schema_test.go
content_entity_name_index_schema_test.go -> content_entity_name_index_schema_test.go
content_search_index_finalize_live_test.go -> content_search_index_finalize_live_test.go
content_search_index_finalize_test.go -> content_search_index_finalize_test.go
cross_scope_completion_index_lifecycle_postgres_live_test.go -> cross_scope_completion_index_lifecycle_postgres_live_test.go
cross_scope_completion_schema_test.go -> cross_scope_completion_schema_test.go
cross_scope_completion_upgrade_postgres_live_test.go -> cross_scope_completion_upgrade_postgres_live_test.go
deferred_maintenance_barrier_test.go -> deferred_maintenance_barrier_test.go   # SPLIT: reads private symbols of ingestion, lock
deferred_maintenance_lock_fakes_test.go -> deferred_maintenance_lock_fakes_test.go   # SPLIT: reads private symbols of ingestion, lock
deferred_maintenance_lock_test.go -> deferred_maintenance_lock_test.go   # SPLIT: reads private symbols of ingestion, lock
documentation_findings_index_restart_live_test.go -> documentation_findings_index_restart_live_test.go
drift_runtime_trigger_test.go -> drift_runtime_trigger_test.go   # SPLIT: reads private symbols of ingestion, queue/reducer
eshu_search_index_partition_contention_live_test.go -> eshu_search_index_partition_contention_live_test.go   # spans root=100%; follows its private symbols, not its name
eshu_search_index_partition_live_test.go -> eshu_search_index_partition_live_test.go   # spans root=100%; follows its private symbols, not its name
eshu_search_index_term_copy_bench_live_test.go -> eshu_search_index_term_copy_bench_live_test.go   # spans root=100%; follows its private symbols, not its name
eshu_search_index_term_copy_test.go -> eshu_search_index_term_copy_test.go   # SPLIT: reads private symbols of db, root
facts_active_container_image_identity_warnings_index_lifecycle_live_test.go -> facts_active_container_image_identity_warnings_index_lifecycle_live_test.go   # spans root=100%; follows its private symbols, not its name
facts_active_container_image_identity_warnings_test.go -> facts_active_container_image_identity_warnings_test.go   # SPLIT: reads private symbols of facts, facts/schema
facts_cross_batch_fencing_proof_test.go -> facts_cross_batch_fencing_proof_test.go   # SPLIT: reads private symbols of facts, facts/schema
fips_md5_queries_test.go -> fips_md5_queries_test.go   # SPLIT: reads private symbols of identity/admin, status
freshness_claim_lease_migration_backfill_integration_test.go -> freshness_claim_lease_migration_backfill_integration_test.go   # spans root=43% freshness/aws=29% freshness/gcp=29%
generation_prior_live_test.go -> generation_prior_live_test.go
graph_node_owner_store_test.go -> graph_node_owner_store_test.go   # SPLIT: reads private symbols of graph/owner, lock
iac_inventory_index_schema_test.go -> iac_inventory_index_schema_test.go
identity_admin_fake_rows_test.go -> identity_admin_fake_rows_test.go   # no production references
identity_epoch_index_replay_live_test.go -> identity_epoch_index_replay_live_test.go
identity_provider_config_fake_db_test.go -> identity_provider_config_fake_db_test.go   # SPLIT: reads private symbols of identity/provider, identity/saml
incident_routing_sql_schema_lockstep_test.go -> incident_routing_sql_schema_lockstep_test.go   # SPLIT: reads private symbols of incident, service
infra_resource_entities_fence_test.go -> infra_resource_entities_fence_test.go
infra_resource_entities_schema_test.go -> infra_resource_entities_schema_test.go
ingestion_backfill_relationship_family_binary_proof_test.go -> ingestion_backfill_relationship_family_binary_proof_test.go   # SPLIT: reads private symbols of generation, ingestion, relationship, root
ingestion_backfill_relationship_family_index_write_tax_helpers_test.go -> ingestion_backfill_relationship_family_index_write_tax_helpers_test.go   # SPLIT: reads private symbols of facts, ingestion, root
ingestion_backfill_relationship_family_odu_proof_helpers_test.go -> ingestion_backfill_relationship_family_odu_proof_helpers_test.go   # SPLIT: reads private symbols of ingestion, root
ingestion_backfill_shared_partition_dupkey_test.go -> ingestion_backfill_shared_partition_dupkey_test.go   # SPLIT: reads private symbols of generation, ingestion
ingestion_latest_generation_cte_test.go -> ingestion_latest_generation_cte_test.go   # SPLIT: reads private symbols of facts, freshness, generation, ingestion
ingestion_test.go -> ingestion_test.go   # SPLIT: reads private symbols of facts, ingestion
ingestion_tx_lock_split_helpers_test.go -> ingestion_tx_lock_split_helpers_test.go   # SPLIT: reads private symbols of ingestion, lock
migration_order_test.go -> migration_order_test.go
package_registry_sql_schema_lockstep_test.go -> package_registry_sql_schema_lockstep_test.go   # SPLIT: reads private symbols of facts, status
pool_exhaustion_test.go -> pool_exhaustion_test.go   # no production references
projector_queue_source_fairness_test.go -> projector_queue_source_fairness_test.go   # SPLIT: reads private symbols of queue/projector, queue/reducer
proof_domain_harness_test.go -> proof_domain_harness_test.go   # SPLIT: reads private symbols of collector, facts, status, vulnerability
proof_domain_retirement_support_test.go -> proof_domain_retirement_support_test.go   # no production references
proof_domain_state_test.go -> proof_domain_state_test.go   # no production references
proof_domain_support_test.go -> proof_domain_support_test.go   # no production references
provenance_edge_identity_upgrade_performance_live_test.go -> provenance_edge_identity_upgrade_performance_live_test.go
readiness_wait_cost_after_perf_test.go -> readiness_wait_cost_after_perf_test.go
readiness_wait_cost_perf_test.go -> readiness_wait_cost_perf_test.go
reducer_heartbeat_startup_window_proof_test.go -> reducer_heartbeat_startup_window_proof_test.go
reducer_queue_ack_fanout_plan_test.go -> reducer_queue_ack_fanout_plan_test.go   # SPLIT: reads private symbols of queue/reducer, scope/completion
reducer_queue_batch_ack_reclaim_live_test.go -> reducer_queue_batch_ack_reclaim_live_test.go   # follows its private symbols, not its name
reducer_queue_claim_bench_test.go -> reducer_queue_claim_bench_test.go   # SPLIT: reads private symbols of generation, queue/reducer
reducer_queue_domain_fairness_test.go -> reducer_queue_domain_fairness_test.go   # SPLIT: reads private symbols of generation, queue/reducer
schema_bootstrap_files_test.go -> schema_bootstrap_files_test.go
schema_documentation_target_test.go -> schema_documentation_target_test.go
schema_fact_records_sbom_test.go -> schema_fact_records_sbom_test.go
schema_index_replay_test.go -> schema_index_replay_test.go
schema_lock_timeout_integration_test.go -> schema_lock_timeout_integration_test.go
schema_migration_recovery_live_test.go -> schema_migration_recovery_live_test.go
schema_migration_tracking_live_test.go -> schema_migration_tracking_live_test.go
schema_order_test.go -> schema_order_test.go
schema_service_catalog_test.go -> schema_service_catalog_test.go
semantic_extraction_observability_test.go -> semantic_extraction_observability_test.go   # no production references
semantic_extraction_queue_test.go -> semantic_extraction_queue_test.go
shared_intents_generation_pending_index_live_test.go -> shared_intents_generation_pending_index_live_test.go   # spans root=100%; follows its private symbols, not its name
status_active_work_semantics_live_test.go -> status_active_work_semantics_live_test.go   # SPLIT: reads private symbols of queue/reducer, status
status_active_work_standalone_test.go -> status_active_work_standalone_test.go   # SPLIT: reads private symbols of queue/reducer, status
status_blockage_leases_live_test.go -> status_blockage_leases_live_test.go   # SPLIT: reads private symbols of queue/reducer, status
status_collector_evidence_test.go -> status_collector_evidence_test.go   # SPLIT: reads private symbols of collector, status
status_filtered_selection_test.go -> status_filtered_selection_test.go   # SPLIT: reads private symbols of collector, status
status_query_shape_test.go -> status_query_shape_test.go   # SPLIT: reads private symbols of collector, queue/reducer, status
status_read_telemetry_test.go -> status_read_telemetry_test.go   # SPLIT: reads private symbols of db, status
status_test.go -> status_test.go   # SPLIT: reads private symbols of collector, queue/reducer, status, vulnerability
supply_chain_impact_canonical_winners_schema_test.go -> supply_chain_impact_canonical_winners_schema_test.go
supply_chain_impact_winners_materialization_schema_test.go -> supply_chain_impact_winners_materialization_schema_test.go
supply_chain_suppression_migration_live_test.go -> supply_chain_suppression_migration_live_test.go
tenant_workspace_grants_test.go -> tenant_workspace_grants_test.go
vulnerability_suppression_advisory_e2e_live_test.go -> vulnerability_suppression_advisory_e2e_live_test.go
vulnerability_suppression_lineage_index_upgrade_live_test.go -> vulnerability_suppression_lineage_index_upgrade_live_test.go   # SPLIT: reads private symbols of root, supply/chain/impact
webhook_refresh_proof_integration_test.go -> webhook_refresh_proof_integration_test.go
webhook_trigger_store_test.go -> webhook_trigger_store_test.go   # no production references
work_queue_lifecycle_test.go -> work_queue_lifecycle_test.go   # SPLIT: reads private symbols of ingestion, queue/reducer
```

</details>

### DELETE (empty files) (9 non-test, 0 test)

```text
admin_replay_request_schema.go -> (delete)   # empty file: license header and package clause only
collector_evidence_summary_schema.go -> (delete)   # empty file: license header and package clause only
collector_generation_dead_letter_schema.go -> (delete)   # empty file: license header and package clause only
graph_schema_applications.go -> (delete)   # empty file: license header and package clause only
schema_fact_records_sbom.go -> (delete)   # empty file: license header and package clause only
schema_fact_records_service_catalog_indexes.go -> (delete)   # empty file: license header and package clause only
service_materialization_schema.go -> (delete)   # design notes only (#1943): fold into service/README.md in the service/ move, then delete
supply_chain_impact_canonical_winners_schema.go -> (delete)   # design notes only (#3389): fold into supply/chain/impact/README.md in that move, then delete
supply_chain_impact_winners_materialization_schema.go -> (delete)   # design notes only (#3389): fold into supply/chain/impact/README.md in that move, then delete
```
