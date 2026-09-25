# #6693 mapping: cloud, terraform state, freshness and supply chain

Part of the [storage/postgres target tree](../6693-postgres-target-tree.md). Destinations covered: `cloud`, `terraform`, `freshness`, `vulnerability`, `container`, `crossplane`, `supply`, `service`, `incident`.

Paths are relative to `go/internal/storage/postgres/`. Each line reads `current -> new`.

### `cloud/aws/` (2 non-test, 2 test)

```text
aws_pagination_checkpoint.go -> cloud/aws/pagination_checkpoint.go
aws_scan_status.go -> cloud/aws/scan_status.go
```

<details><summary>Tests</summary>

```text
aws_pagination_checkpoint_test.go -> cloud/aws/pagination_checkpoint_test.go
aws_scan_status_test.go -> cloud/aws/scan_status_test.go
```

</details>

### `cloud/aws/drift/` (9 non-test, 14 test)

```text
aws_cloud_runtime_drift_admission_beginner.go -> cloud/aws/drift/admission_beginner.go
aws_cloud_runtime_drift_evidence.go -> cloud/aws/drift/evidence.go
aws_cloud_runtime_drift_evidence_payload.go -> cloud/aws/drift/evidence_payload.go
aws_cloud_runtime_drift_evidence_sql.go -> cloud/aws/drift/evidence_sql.go
aws_cloud_runtime_drift_fencing_token.go -> cloud/aws/drift/fencing_token.go
aws_cloud_runtime_drift_findings.go -> cloud/aws/drift/findings.go
aws_cloud_runtime_drift_readiness.go -> cloud/aws/drift/readiness.go
aws_cloud_runtime_drift_selectors.go -> cloud/aws/drift/selectors.go
aws_cloud_runtime_drift_value_attributes.go -> cloud/aws/drift/value_attributes.go
```

<details><summary>Tests</summary>

```text
aws_cloud_runtime_drift_admission_live_test.go -> cloud/aws/drift/runtime_admission_live_test.go   # external test package: imports root
aws_cloud_runtime_drift_evidence_test.go -> cloud/aws/drift/evidence_test.go
aws_cloud_runtime_drift_findings_roundtrip_test.go -> cloud/aws/drift/findings_roundtrip_test.go
aws_cloud_runtime_drift_findings_test.go -> cloud/aws/drift/findings_test.go
aws_cloud_runtime_drift_identity_test.go -> cloud/aws/drift/runtime_identity_test.go
aws_cloud_runtime_drift_lambda_completeness_live_test.go -> cloud/aws/drift/runtime_lambda_completeness_live_test.go   # external test package + export_test.go shim: imports root
aws_cloud_runtime_drift_readiness_deterministic_live_test.go -> cloud/aws/drift/readiness_deterministic_live_test.go   # external test package: imports root
aws_cloud_runtime_drift_readiness_live_test.go -> cloud/aws/drift/readiness_live_test.go   # external test package: imports root
aws_cloud_runtime_drift_truncation_test.go -> cloud/aws/drift/runtime_truncation_test.go
aws_cloud_runtime_drift_value_attributes_array_marker_test.go -> cloud/aws/drift/value_attributes_array_marker_test.go
aws_cloud_runtime_drift_value_attributes_redaction_test.go -> cloud/aws/drift/value_attributes_redaction_test.go
aws_cloud_runtime_drift_value_attributes_test.go -> cloud/aws/drift/value_attributes_test.go
aws_cloud_runtime_drift_value_completeness_test.go -> cloud/aws/drift/runtime_value_completeness_test.go
replatforming_selectors_test.go -> cloud/aws/drift/replatforming_selectors_test.go
```

</details>

### `cloud/inventory/` (12 non-test, 12 test)

```text
cloud_identity_policy_evidence.go -> cloud/inventory/identity_policy_evidence.go
cloud_identity_policy_evidence_sql.go -> cloud/inventory/identity_policy_evidence_sql.go
cloud_inventory_evidence.go -> cloud/inventory/evidence.go
cloud_inventory_evidence_gcp_project_id.go -> cloud/inventory/gcp_project_id.go
cloud_inventory_evidence_sql.go -> cloud/inventory/evidence_sql.go
cloud_inventory_identity_decode.go -> cloud/inventory/identity_decode.go
cloud_resource_change_evidence.go -> cloud/inventory/resource_change_evidence.go
cloud_resource_change_evidence_sql.go -> cloud/inventory/resource_change_evidence_sql.go
cloud_resource_liveness.go -> cloud/inventory/resource_liveness.go
cloud_tag_evidence.go -> cloud/inventory/tag_evidence.go
cloud_tag_evidence_sql.go -> cloud/inventory/tag_evidence_sql.go
factschema_decode_cloud_tag_evidence.go -> cloud/inventory/tag_evidence_decode.go
```

<details><summary>Tests</summary>

```text
cloud_identity_policy_evidence_test.go -> cloud/inventory/identity_policy_evidence_test.go
cloud_inventory_admission_integration_test.go -> cloud/inventory/admission_integration_test.go
cloud_inventory_evidence_account_id_test.go -> cloud/inventory/evidence_account_id_test.go
cloud_inventory_evidence_attribute_allowlist_test.go -> cloud/inventory/evidence_attribute_allowlist_test.go
cloud_inventory_evidence_bench_test.go -> cloud/inventory/evidence_bench_test.go
cloud_inventory_evidence_test.go -> cloud/inventory/evidence_test.go
cloud_resource_change_evidence_test.go -> cloud/inventory/resource_change_evidence_test.go
cloud_resource_liveness_live_test.go -> cloud/inventory/resource_liveness_live_test.go   # spans graph/owner=64% cloud/inventory=36%
cloud_resource_liveness_plan_live_test.go -> cloud/inventory/resource_liveness_plan_live_test.go
cloud_resource_liveness_test.go -> cloud/inventory/resource_liveness_test.go
cloud_tag_evidence_bench_test.go -> cloud/inventory/tag_evidence_bench_test.go
cloud_tag_evidence_test.go -> cloud/inventory/tag_evidence_test.go
```

</details>

### `cloud/multi/` (5 non-test, 5 test)

```text
cloud_runtime_drift_aggregate_findings.go -> cloud/multi/runtime_drift_aggregate_findings.go
multi_cloud_runtime_drift_evidence.go -> cloud/multi/evidence.go
multi_cloud_runtime_drift_evidence_sql.go -> cloud/multi/evidence_sql.go
multi_cloud_runtime_drift_findings.go -> cloud/multi/findings.go
multi_cloud_runtime_drift_identity.go -> cloud/multi/identity.go
```

<details><summary>Tests</summary>

```text
cloud_runtime_drift_aggregate_findings_test.go -> cloud/multi/runtime_drift_aggregate_findings_test.go
multi_cloud_runtime_drift_evidence_casing_test.go -> cloud/multi/evidence_casing_test.go
multi_cloud_runtime_drift_evidence_test.go -> cloud/multi/evidence_test.go
multi_cloud_runtime_drift_findings_test.go -> cloud/multi/findings_test.go
multi_cloud_runtime_drift_value_attributes_test.go -> cloud/multi/runtime_drift_value_attributes_test.go
```

</details>

### `container/image/` (7 non-test, 11 test)

```text
container_image_identity_beginner.go -> container/image/beginner.go
container_image_identity_claimed_execer.go -> container/image/claimed_execer.go
container_image_identity_cutover.go -> container/image/cutover.go
container_image_identity_held_support.go -> container/image/held_support.go
container_image_identity_scope_state.go -> container/image/scope_state.go
schema_container_image_identity_claim_epoch.go -> container/image/claim_epoch_schema.go
schema_container_image_identity_cutover.go -> container/image/cutover_schema.go
```

<details><summary>Tests</summary>

```text
container_image_identity_beginner_test.go -> container/image/beginner_test.go
container_image_identity_cutover_migration_behavior_live_test.go -> container/image/cutover_migration_behavior_live_test.go
cross_scope_completion_concurrency_postgres_live_test.go -> container/image/cross_scope_completion_concurrency_postgres_live_test.go   # follows its DB harness, not its name: opens its database only through the container-image ACK capability harness; deferred from scope/completion/ step 22
cross_scope_completion_snapshot_postgres_live_test.go -> container/image/cross_scope_completion_snapshot_postgres_live_test.go   # follows its DB harness, not its name (see above); deferred from scope/completion/ step 22
container_image_identity_cutover_migration_rerun_live_test.go -> container/image/cutover_migration_rerun_live_test.go   # external test package: imports root
container_image_identity_cutover_test.go -> container/image/cutover_test.go
container_image_identity_cutover_work_item_lock_live_test.go -> container/image/cutover_work_item_lock_live_test.go   # external test package: imports root
container_image_identity_held_support_postgres_live_test.go -> container/image/held_support_postgres_live_test.go   # external test package: imports root
container_image_identity_v3_migration_helpers_postgres_live_test.go -> container/image/identity_v3_migration_helpers_postgres_live_test.go   # external test package: imports root
reducer_fact_batch_insert_fence_live_test.go -> container/image/reducer_fact_batch_insert_fence_live_test.go   # external test package: imports root
schema_container_image_identity_cutover_test.go -> container/image/cutover_schema_test.go   # external test package: imports root
```

</details>

### `crossplane/` (5 non-test, 5 test)

```text
crossplane_satisfied_by_redrive_ledger.go -> crossplane/ledger.go
crossplane_satisfied_by_redrive_query.go -> crossplane/query.go
crossplane_satisfied_by_redrive_state.go -> crossplane/state.go
crossplane_satisfied_by_redrive_sweep.go -> crossplane/sweep.go
crossplane_satisfied_by_redrive_sweep_batch.go -> crossplane/sweep_batch.go
```

<details><summary>Tests</summary>

```text
crossplane_satisfied_by_redrive_behavior_live_test.go -> crossplane/satisfied_by_redrive_behavior_live_test.go   # external test package + export_test.go shim: imports facts, root, status
crossplane_satisfied_by_redrive_catchup_live_test.go -> crossplane/satisfied_by_redrive_catchup_live_test.go   # external test package + export_test.go shim: imports root, status
crossplane_satisfied_by_redrive_ledger_live_test.go -> crossplane/ledger_live_test.go   # external test package: imports facts, root, status
crossplane_satisfied_by_redrive_live_test.go -> crossplane/satisfied_by_redrive_live_test.go   # external test package: imports root
crossplane_satisfied_by_redrive_query_plan_live_test.go -> crossplane/query_plan_live_test.go
```

</details>

### `freshness/` (2 non-test, 4 test)

```text
repository_freshness.go -> freshness/repository.go
repository_freshness_sql.go -> freshness/repository_sql.go
```

<details><summary>Tests</summary>

```text
repository_freshness_db_integration_schema_test.go -> freshness/repository_db_integration_schema_test.go   # external test package: imports root
repository_freshness_db_integration_test.go -> freshness/repository_db_integration_test.go   # external test package: imports root
repository_freshness_test.go -> freshness/repository_test.go
repository_freshness_webhook_query_test.go -> freshness/repository_webhook_query_test.go
```

</details>

### `freshness/aws/` (3 non-test, 2 test)

```text
aws_freshness_schema_sql.go -> freshness/aws/schema_sql.go
aws_freshness_sql.go -> freshness/aws/sql.go
aws_freshness_store.go -> freshness/aws/store.go
```

<details><summary>Tests</summary>

```text
aws_freshness_claim_lease_integration_test.go -> freshness/aws/claim_lease_integration_test.go   # external test package: imports root
aws_freshness_store_test.go -> freshness/aws/store_test.go
```

</details>

### `freshness/gcp/` (3 non-test, 2 test)

```text
gcp_freshness_schema_sql.go -> freshness/gcp/schema_sql.go
gcp_freshness_sql.go -> freshness/gcp/sql.go
gcp_freshness_store.go -> freshness/gcp/store.go
```

<details><summary>Tests</summary>

```text
gcp_freshness_claim_lease_integration_test.go -> freshness/gcp/claim_lease_integration_test.go   # external test package: imports root
gcp_freshness_store_test.go -> freshness/gcp/store_test.go
```

</details>

### `freshness/incident/` (3 non-test, 1 test)

```text
incident_freshness_schema_sql.go -> freshness/incident/schema_sql.go
incident_freshness_sql.go -> freshness/incident/sql.go
incident_freshness_store.go -> freshness/incident/store.go
```

<details><summary>Tests</summary>

```text
incident_freshness_store_test.go -> freshness/incident/store_test.go
```

</details>

### `incident/` (1 non-test, 1 test)

```text
incident_repository_correlation_loader.go -> incident/repository_correlation_loader.go
```

<details><summary>Tests</summary>

```text
incident_repository_correlation_loader_test.go -> incident/repository_correlation_loader_test.go
```

</details>

### `service/` (4 non-test, 3 test)

```text
service_catalog_id_resolver.go -> service/catalog_id_resolver.go
service_documentation_evidence.go -> service/documentation_evidence.go
service_incident_evidence_loader.go -> service/incident_evidence_loader.go
service_materialization_beginner.go -> service/materialization_beginner.go
```

<details><summary>Tests</summary>

```text
service_catalog_id_resolver_test.go -> service/catalog_id_resolver_test.go
service_documentation_evidence_test.go -> service/documentation_evidence_test.go
service_incident_evidence_loader_test.go -> service/incident_evidence_loader_test.go
```

</details>

### `supply/chain/impact/` (2 non-test, 4 test)

```text
supply_chain_impact_canonical_winners_store.go -> supply/chain/impact/canonical_winners.go
vulnerability_suppression_store.go -> supply/chain/impact/suppression.go
```

<details><summary>Tests</summary>

```text
supply_chain_impact_canonical_winners_store_test.go -> supply/chain/impact/canonical_winners_test.go
vulnerability_suppression_lineage_index_schema_test.go -> supply/chain/impact/vulnerability_suppression_lineage_index_schema_test.go   # external test package + export_test.go shim: imports root; follows its private symbols, not its name
vulnerability_suppression_store_live_test.go -> supply/chain/impact/suppression_live_test.go   # external test package + export_test.go shim: imports root
vulnerability_suppression_store_mixed_failure_live_test.go -> supply/chain/impact/suppression_mixed_failure_live_test.go   # external test package + export_test.go shim: imports root
```

</details>

### `terraform/state/` (1 non-test, 1 test)

```text
tfstate_status.go -> terraform/state/status.go
```

<details><summary>Tests</summary>

```text
tfstate_status_test.go -> terraform/state/status_test.go
```

</details>

### `terraform/state/backend/` (5 non-test, 7 test)

```text
tfstate_backend_canonical.go -> terraform/state/backend/canonical.go
tfstate_backend_facts.go -> terraform/state/backend/facts.go
tfstate_backend_filter.go -> terraform/state/backend/filter.go
tfstate_backend_interpolation.go -> terraform/state/backend/interpolation.go
tfstate_backend_queries.go -> terraform/state/backend/queries.go
```

<details><summary>Tests</summary>

```text
tfstate_backend_canonical_local_test.go -> terraform/state/backend/canonical_local_test.go
tfstate_backend_canonical_repo_local_path_integration_test.go -> terraform/state/backend/canonical_repo_local_path_integration_test.go   # external test package: imports root
tfstate_backend_canonical_test.go -> terraform/state/backend/canonical_test.go
tfstate_backend_facts_test.go -> terraform/state/backend/facts_test.go
tfstate_backend_filter_test.go -> terraform/state/backend/filter_test.go
tfstate_backend_interpolation_test.go -> terraform/state/backend/interpolation_test.go
tfstate_drift_jsonb_null_integration_test.go -> terraform/state/backend/drift_jsonb_null_integration_test.go   # external test package: imports root
```

</details>

### `terraform/state/drift/` (10 non-test, 13 test)

```text
terraform_config_state_drift_findings.go -> terraform/state/drift/config_findings.go
tfstate_drift_evidence.go -> terraform/state/drift/evidence.go
tfstate_drift_evidence_config_row.go -> terraform/state/drift/config_row.go
tfstate_drift_evidence_helpers.go -> terraform/state/drift/helpers.go
tfstate_drift_evidence_module_confidence.go -> terraform/state/drift/module_confidence.go
tfstate_drift_evidence_module_prefix.go -> terraform/state/drift/module_prefix.go
tfstate_drift_evidence_pairing.go -> terraform/state/drift/pairing.go
tfstate_drift_evidence_prior_config.go -> terraform/state/drift/prior_config.go
tfstate_drift_evidence_sql.go -> terraform/state/drift/sql.go
tfstate_drift_evidence_state_row.go -> terraform/state/drift/collector_row.go
```

<details><summary>Tests</summary>

```text
terraform_config_state_drift_findings_test.go -> terraform/state/drift/config_findings_test.go
tfstate_drift_evidence_config_row_test.go -> terraform/state/drift/config_row_test.go
tfstate_drift_evidence_cross_package_encoding_test.go -> terraform/state/drift/evidence_cross_package_encoding_test.go
tfstate_drift_evidence_module_confidence_test.go -> terraform/state/drift/module_confidence_test.go
tfstate_drift_evidence_module_integration_test.go -> terraform/state/drift/evidence_module_integration_test.go
tfstate_drift_evidence_module_prefix_test.go -> terraform/state/drift/module_prefix_test.go
tfstate_drift_evidence_pairing_cardinality_test.go -> terraform/state/drift/pairing_cardinality_test.go
tfstate_drift_evidence_pairing_test.go -> terraform/state/drift/pairing_test.go
tfstate_drift_evidence_prior_config_ordering_live_test.go -> terraform/state/drift/prior_config_ordering_live_test.go   # external test package + export_test.go shim: imports root
tfstate_drift_evidence_prior_config_ordering_test.go -> terraform/state/drift/prior_config_ordering_test.go
tfstate_drift_evidence_prior_config_test.go -> terraform/state/drift/prior_config_test.go
tfstate_drift_evidence_state_row_test.go -> terraform/state/drift/collector_row_test.go
tfstate_drift_evidence_test.go -> terraform/state/drift/evidence_test.go
```

</details>

### `vulnerability/` (1 non-test, 1 test)

```text
vulnerability_source_state.go -> vulnerability/source_state.go   # N4: collector source state, not a freshness trigger store
```

<details><summary>Tests</summary>

```text
vulnerability_source_state_test.go -> vulnerability/source_state_test.go   # spans vulnerability=57% root=43%; external test package (package vulnerabilitystore_test): status.go's read call forces root to import this leaf, so an in-package test importing root for BootstrapDefinitions would cycle; exporting ReadVulnerabilitySourceStates left no private symbol to shim
```

</details>
