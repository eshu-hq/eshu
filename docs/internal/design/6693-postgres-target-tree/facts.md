# #6693 mapping: facts

Part of the [storage/postgres target tree](../6693-postgres-target-tree.md). Destinations covered: `facts`.

Paths are relative to `go/internal/storage/postgres/`. Each line reads `current -> new`.

### `facts/` (36 non-test, 69 test)

```text
code_function_source_loader.go -> facts/function_source_loader.go
code_function_summary_loader.go -> facts/function_summary_loader.go
code_interproc_evidence_loader.go -> facts/interproc_evidence_loader.go
code_taint_evidence_loader.go -> facts/taint_evidence_loader.go
container_image_identity_support_fact_loader.go -> facts/container_image_identity_support.go
fact_records_scope_generation_enum.go -> facts/scope_generation_enum.go
facts.go -> facts/store.go
facts_active_cicd_run_correlation.go -> facts/active_cicd_run_correlation.go
facts_active_cicd_workflow_image.go -> facts/active_cicd_workflow_image.go
facts_active_code_call_symbols.go -> facts/active_code_call_symbols.go
facts_active_container_image_ci.go -> facts/active_container_image_ci.go
facts_active_container_image_identity.go -> facts/active_container_image_identity.go
facts_active_container_image_identity_warnings.go -> facts/active_container_image_identity_warnings.go
facts_active_container_image_slsa.go -> facts/active_container_image_slsa.go
facts_active_crossplane_xrd.go -> facts/active_crossplane_xrd.go
facts_active_jvm_reachability.go -> facts/active_jvm_reachability.go
facts_active_package_manifest_dependency.go -> facts/active_package_manifest_dependency.go
facts_active_package_ownership.go -> facts/active_package_ownership.go
facts_active_repository.go -> facts/active_repository.go
facts_active_sbom_attestation_attachment.go -> facts/active_sbom_attestation_attachment.go
facts_active_security_alert_reconciliation.go -> facts/active_security_alert_reconciliation.go
facts_active_supply_chain_impact.go -> facts/active_supply_chain_impact.go
facts_active_supply_chain_impact_identity_paging.go -> facts/active_supply_chain_impact_identity_paging.go
facts_cicd_run_history.go -> facts/cicd_run_history.go
facts_cicd_run_history_keys.go -> facts/cicd_run_history_keys.go
facts_filtered.go -> facts/filtered.go
facts_streaming.go -> facts/streaming.go
identity_epoch_cache.go -> facts/identity_epoch_cache.go   # container-image identity epochs, only FactStore reads it
incident_routing_evidence_loader.go -> facts/incident_routing_evidence.go
installed_advisory_targets.go -> facts/installed_advisory_targets.go
installed_advisory_targets_os_package_envelope.go -> facts/installed_advisory_targets_os_package.go
owned_package_targets.go -> facts/owned_package_targets.go
reducer_input_invalid_facts.go -> facts/reducer_input_invalid.go   # U1: facts/ (per-fact ledger, no claim or lease)
secrets_iam_trust_chain_anchor_decode.go -> facts/secrets_iam_trust_chain_anchors.go
secrets_iam_trust_chain_evidence_loader.go -> facts/secrets_iam_trust_chain_evidence.go
service_vulnerability_advisory_loader.go -> facts/service_vulnerability_advisories.go
```

<details><summary>Tests</summary>

```text
container_image_identity_read_snapshot_test.go -> facts/container_image_identity_read_snapshot_test.go
container_image_identity_support_fact_loader_test.go -> facts/container_image_identity_support_test.go
container_image_identity_support_facts_postgres_live_test.go -> facts/container_image_identity_support_postgres_live_test.go   # external test package: imports root
container_image_identity_support_pagination_collation_postgres_live_test.go -> facts/container_image_identity_support_pagination_collation_postgres_live_test.go   # external test package + export_test.go shim: imports root; follows its private symbols, not its name
container_image_identity_support_pagination_prefix_postgres_live_test.go -> facts/container_image_identity_support_pagination_prefix_postgres_live_test.go   # external test package + export_test.go shim: imports root
container_image_identity_support_snapshot_cutover_postgres_live_test.go -> facts/container_image_identity_support_snapshot_cutover_postgres_live_test.go   # external test package + export_test.go shim: imports root; follows its private symbols, not its name
container_image_identity_support_snapshot_postgres_live_test.go -> facts/container_image_identity_support_snapshot_postgres_live_test.go   # external test package + export_test.go shim: imports root; follows its private symbols, not its name
container_image_identity_support_store_schema_test.go -> facts/container_image_identity_support_store_schema_test.go   # external test package + export_test.go shim: imports root; follows its private symbols, not its name
documentation_facts_write_index_live_test.go -> facts/documentation_write_index_live_test.go   # external test package + export_test.go shim: imports root
documentation_query_plan_shim_binding_test.go -> facts/documentation_query_plan_shim_binding_test.go
fact_records_keyset_index_live_test.go -> facts/records_keyset_index_live_test.go   # external test package + export_test.go shim: imports root; follows its private symbols, not its name
fact_records_scope_generation_enum_integration_test.go -> facts/scope_generation_enum_integration_test.go   # external test package: imports root
fact_records_scope_generation_enum_test.go -> facts/scope_generation_enum_test.go
facts_active_cicd_run_correlation_test.go -> facts/active_cicd_run_correlation_test.go
facts_active_cicd_workflow_image_live_test.go -> facts/active_cicd_workflow_image_live_test.go   # external test package: imports root
facts_active_cicd_workflow_image_test.go -> facts/active_cicd_workflow_image_test.go
facts_active_code_call_symbols_live_test.go -> facts/active_code_call_symbols_live_test.go   # external test package: imports root
facts_active_code_call_symbols_test.go -> facts/active_code_call_symbols_test.go
facts_active_container_image_ci_owner_live_test.go -> facts/active_container_image_ci_owner_live_test.go   # external test package: imports root
facts_active_container_image_ci_test.go -> facts/active_container_image_ci_test.go
facts_active_container_image_identity_test.go -> facts/active_container_image_identity_test.go
facts_active_container_image_slsa_test.go -> facts/active_container_image_slsa_test.go
facts_active_jvm_reachability_test.go -> facts/active_jvm_reachability_test.go
facts_active_package_manifest_dependency_test.go -> facts/active_package_manifest_dependency_test.go
facts_active_repository_test.go -> facts/active_repository_test.go
facts_active_sbom_attestation_attachment_test.go -> facts/active_sbom_attestation_attachment_test.go
facts_active_supply_chain_impact_file_kind_gate_live_test.go -> facts/active_supply_chain_impact_file_kind_gate_live_test.go   # external test package + export_test.go shim: imports root
facts_active_supply_chain_impact_file_kind_gate_test.go -> facts/active_supply_chain_impact_file_kind_gate_test.go
facts_active_supply_chain_impact_identity_paging_test.go -> facts/active_supply_chain_impact_identity_paging_test.go
facts_active_supply_chain_impact_row_cap_live_test.go -> facts/active_supply_chain_impact_row_cap_live_test.go   # external test package + export_test.go shim: imports root
facts_active_supply_chain_impact_row_cap_test.go -> facts/active_supply_chain_impact_row_cap_test.go
facts_active_supply_chain_impact_scope_live_helpers_test.go -> facts/active_supply_chain_impact_scope_live_helpers_test.go
facts_active_supply_chain_impact_scope_normalize_live_test.go -> facts/active_supply_chain_impact_scope_normalize_live_test.go   # external test package: imports root
facts_active_supply_chain_impact_scope_normalize_test.go -> facts/active_supply_chain_impact_scope_normalize_test.go
facts_active_supply_chain_impact_test.go -> facts/active_supply_chain_impact_test.go   # external test package + export_test.go shim: imports root
facts_advisory_evidence_indexes_test.go -> facts/store_advisory_evidence_indexes_test.go   # external test package: imports root
facts_cicd_run_history_latest_snapshot_live_test.go -> facts/cicd_run_history_latest_snapshot_live_test.go   # external test package: imports root
facts_cicd_run_history_live_test.go -> facts/cicd_run_history_live_test.go   # external test package: imports root
facts_cicd_run_history_scale_live_test.go -> facts/cicd_run_history_scale_live_test.go   # external test package: imports root
facts_cicd_run_history_scale_theory_live_test.go -> facts/cicd_run_history_scale_theory_live_test.go
facts_cicd_run_history_snapshot_window_live_test.go -> facts/cicd_run_history_snapshot_window_live_test.go   # external test package: imports root
facts_cicd_run_history_test.go -> facts/cicd_run_history_test.go
facts_dedup_test.go -> facts/store_dedup_test.go
facts_documentation_google_workspace_test.go -> facts/store_documentation_google_workspace_test.go
facts_documentation_media_transcript_test.go -> facts/store_documentation_media_transcript_test.go
facts_documentation_ocr_test.go -> facts/store_documentation_ocr_test.go
facts_documentation_structured_diagram_test.go -> facts/store_documentation_structured_diagram_test.go
facts_documentation_test.go -> facts/store_documentation_test.go
facts_filtered_keyset_test.go -> facts/filtered_keyset_test.go
facts_filtered_test.go -> facts/filtered_test.go
facts_incident_runtime_indexes_test.go -> facts/store_incident_runtime_indexes_test.go   # external test package: imports root
facts_test.go -> facts/store_test.go
identity_epoch_cache_concurrency_test.go -> facts/identity_epoch_cache_concurrency_test.go
identity_epoch_cache_contract_test.go -> facts/identity_epoch_cache_contract_test.go
identity_epoch_cache_fingerprint_test.go -> facts/identity_epoch_cache_fingerprint_test.go
identity_epoch_cache_live_test.go -> facts/identity_epoch_cache_live_test.go   # external test package + export_test.go shim: imports root
identity_epoch_cache_test.go -> facts/identity_epoch_cache_test.go
incident_routing_evidence_loader_test.go -> facts/incident_routing_evidence_test.go
installed_advisory_targets_os_package_envelope_test.go -> facts/installed_advisory_targets_os_package_test.go
installed_advisory_targets_test.go -> facts/installed_advisory_targets_test.go
owned_package_targets_test.go -> facts/owned_package_targets_test.go
proof_domain_retirement_test.go -> facts/proof_domain_retirement_test.go   # external test package + export_test.go shim: imports ingestion
proof_domain_tx_harness_test.go -> facts/proof_domain_tx_harness_test.go   # spans facts=50% facts/payload=50%
reducer_input_invalid_facts_live_test.go -> facts/reducer_input_invalid_live_test.go   # external test package: imports root
reducer_input_invalid_facts_test.go -> facts/reducer_input_invalid_test.go
secrets_iam_trust_chain_evidence_loader_test.go -> facts/secrets_iam_trust_chain_evidence_test.go
service_vulnerability_advisory_loader_test.go -> facts/service_vulnerability_advisories_test.go
supply_chain_suppression_sql_proof_live_test.go -> facts/supply_chain_suppression_sql_proof_live_test.go   # external test package + export_test.go shim: imports root; follows its private symbols, not its name
vulnerability_sql_schema_lockstep_test.go -> facts/vulnerability_sql_schema_lockstep_test.go
```

</details>

### `facts/payload/` (1 non-test, 2 test)

```text
facts_payload.go -> facts/payload/json.go
```

<details><summary>Tests</summary>

```text
facts_payload_test.go -> facts/payload/json_test.go
proof_domain_terraform_support_test.go -> facts/payload/proof_domain_terraform_support_test.go
```

</details>

### `facts/schema/` (7 non-test, 8 test)

```text
schema_fact_records.go -> facts/schema/records.go
schema_fact_records_code_flow_indexes.go -> facts/schema/code_flow_indexes.go
schema_fact_records_documentation.go -> facts/schema/documentation_indexes.go
schema_fact_records_incident_indexes.go -> facts/schema/incident_indexes.go
schema_fact_records_incident_runtime_indexes.go -> facts/schema/incident_runtime_indexes.go
schema_fact_records_incident_workitem_indexes.go -> facts/schema/incident_work_item_indexes.go
schema_fact_records_vulnerability_indexes.go -> facts/schema/vulnerability_indexes.go
```

<details><summary>Tests</summary>

```text
documentation_findings_unfiltered_index_test.go -> facts/schema/documentation_findings_unfiltered_index_test.go   # spans root=50% facts/schema=50%; follows its private symbols, not its name
documentation_index_schema_test.go -> facts/schema/documentation_index_test.go   # external test package + export_test.go shim: imports root; follows its private symbols, not its name
facts_code_flow_indexes_test.go -> facts/schema/code_flow_indexes_test.go   # spans facts/schema=100%; follows its private symbols, not its name
facts_incident_context_indexes_test.go -> facts/schema/incident_context_indexes_test.go   # spans facts/schema=100%; follows its private symbols, not its name
facts_incident_workitem_indexes_test.go -> facts/schema/incident_workitem_indexes_test.go   # spans facts/schema=100%; follows its private symbols, not its name
schema_fact_indexes_test.go -> facts/schema/indexes_test.go   # external test package + export_test.go shim: imports root; follows its private symbols, not its name
supply_chain_impact_finding_id_index_schema_test.go -> facts/schema/supply_chain_impact_finding_id_index_test.go   # external test package + export_test.go shim: imports root; follows its private symbols, not its name
supply_chain_runtime_filter_indexes_test.go -> facts/schema/supply_chain_runtime_filter_indexes_test.go
```

</details>
