# #6693 mapping: code analysis, search and content

Part of the [storage/postgres target tree](../6693-postgres-target-tree.md). Destinations covered: `code`, `search`, `content`, `graph`, `iac`.

Paths are relative to `go/internal/storage/postgres/`. Each line reads `current -> new`.

### `code/divergence/` (3 non-test, 3 test)

```text
code_drifted_evidence.go -> code/divergence/evidence.go
code_drifted_evidence_sql.go -> code/divergence/evidence_sql.go
code_drifted_findings.go -> code/divergence/findings.go
```

<details><summary>Tests</summary>

```text
code_drifted_evidence_test.go -> code/divergence/evidence_test.go
code_drifted_findings_live_test.go -> code/divergence/findings_live_test.go   # external test package + export_test.go shim: imports root
code_drifted_findings_test.go -> code/divergence/findings_test.go
```

</details>

### `code/flow/` (8 non-test, 10 test)

```text
code_value_flow_backfill_state_store.go -> code/flow/backfill_state.go
code_value_flow_stale_cleanup.go -> code/flow/stale_cleanup.go
function_graph_id_store.go -> code/flow/function_graph_id.go
function_source_store.go -> code/flow/function_source.go
summary_store.go -> code/flow/function_summary.go
value_flow_fixpoint_component_store.go -> code/flow/fixpoint_component.go
value_flow_program_loader.go -> code/flow/program_loader.go
value_flow_refresh_ack.go -> code/flow/refresh_ack.go
```

<details><summary>Tests</summary>

```text
code_value_flow_backfill_state_store_test.go -> code/flow/backfill_state_test.go   # external test package: imports root
code_value_flow_stale_cleanup_test.go -> code/flow/stale_cleanup_test.go
function_graph_id_store_test.go -> code/flow/function_graph_id_test.go
function_source_store_test.go -> code/flow/function_source_test.go
summary_store_replace_test.go -> code/flow/function_summary_replace_test.go
summary_store_test.go -> code/flow/function_summary_test.go   # external test package: imports root
value_flow_fixpoint_component_store_test.go -> code/flow/fixpoint_component_test.go   # external test package: imports root
value_flow_program_loader_test.go -> code/flow/program_loader_test.go
value_flow_refresh_ack_live_test.go -> code/flow/refresh_ack_live_test.go   # external test package: imports queue/reducer, root
value_flow_refresh_ack_test.go -> code/flow/refresh_ack_test.go
```

</details>

### `code/reachability/` (3 non-test, 4 test)

```text
code_reachability.go -> code/reachability/store.go
code_reachability_helpers.go -> code/reachability/helpers.go
code_reachability_loader.go -> code/reachability/loader.go
```

<details><summary>Tests</summary>

```text
code_reachability_route_liveness_live_test.go -> code/reachability/store_route_liveness_live_test.go   # external test package + export_test.go shim: imports root
code_reachability_sql_shape_test.go -> code/reachability/store_sql_shape_test.go
code_reachability_test.go -> code/reachability/store_test.go
code_reachability_upgrade_backfill_live_test.go -> code/reachability/store_upgrade_backfill_live_test.go   # external test package: imports root
```

</details>

### `code/taint/` (2 non-test, 2 test)

```text
code_interproc_projected_edge_store.go -> code/taint/interprocedural_edge.go
code_taint_evidence_projected_node_store.go -> code/taint/projected_node.go
```

<details><summary>Tests</summary>

```text
code_interproc_projected_edge_store_test.go -> code/taint/interprocedural_edge_test.go   # external test package: imports root
code_taint_evidence_projected_node_store_test.go -> code/taint/projected_node_test.go   # external test package: imports root
```

</details>

### `content/` (13 non-test, 15 test)

```text
content_search_index_lifecycle.go -> content/search_index_lifecycle.go
content_store.go -> content/store.go
content_store_writes.go -> content/store_writes.go
content_writer.go -> content/writer.go
content_writer_batch.go -> content/writer_batch.go
content_writer_delete_batch.go -> content/writer_delete_batch.go
content_writer_fingerprint.go -> content/writer_fingerprint.go
content_writer_reap.go -> content/writer_reap.go
content_writer_references.go -> content/writer_references.go
content_writer_repository_refs.go -> content/writer_repository_refs.go
content_writer_sql.go -> content/writer_sql.go
content_writer_upserts.go -> content/writer_upserts.go
schema_content_store.go -> content/schema.go
```

<details><summary>Tests</summary>

```text
content_store_test.go -> content/store_test.go
content_writer_batch_test.go -> content/writer_batch_test.go
content_writer_delete_batch_test.go -> content/writer_delete_batch_test.go
content_writer_fingerprint_test.go -> content/writer_fingerprint_test.go
content_writer_github_actions_retraction_test.go -> content/writer_github_actions_retraction_test.go
content_writer_infra_fence_bench_live_test.go -> content/writer_infra_fence_bench_live_test.go   # external test package + export_test.go shim: imports root
content_writer_infra_fence_test.go -> content/writer_infra_fence_test.go
content_writer_infra_inventory_test.go -> content/writer_infra_inventory_test.go
content_writer_purge_test.go -> content/writer_purge_test.go
content_writer_reap_5507_test.go -> content/writer_reap_5507_test.go
content_writer_reap_concurrency_test.go -> content/writer_reap_concurrency_test.go
content_writer_reap_test.go -> content/writer_reap_test.go
content_writer_repository_refs_test.go -> content/writer_repository_refs_test.go
content_writer_test.go -> content/writer_test.go
schema_test.go -> content/schema_test.go   # external test package + export_test.go shim: imports root; follows its private symbols, not its name
```

</details>

### `graph/` (1 non-test, 1 test)

```text
graph_endpoint_presence.go -> graph/endpoint_presence.go
```

<details><summary>Tests</summary>

```text
graph_endpoint_presence_test.go -> graph/endpoint_presence_test.go
```

</details>

### `graph/owner/` (2 non-test, 3 test)

```text
graph_node_owner_backfill_store.go -> graph/owner/backfill.go
graph_node_owner_store.go -> graph/owner/store.go   # needs the migrations leaf, see D2
```

<details><summary>Tests</summary>

```text
graph_node_owner_backfill_store_live_test.go -> graph/owner/backfill_live_test.go   # external test package: imports root
graph_node_owner_backfill_store_test.go -> graph/owner/backfill_test.go   # external test package + export_test.go shim: imports root
graph_node_owner_store_integration_test.go -> graph/owner/store_integration_test.go
```

</details>

### `iac/` (1 non-test, 1 test)

```text
iac_reachability.go -> iac/reachability.go
```

<details><summary>Tests</summary>

```text
iac_reachability_test.go -> iac/reachability_test.go   # external test package: imports ingestion
```

</details>

### `search/document/` (6 non-test, 7 test)

```text
eshu_search_document.go -> search/document/store.go
eshu_search_document_pending.go -> search/document/pending.go
eshu_search_document_projection_state.go -> search/document/projection_state.go
eshu_search_document_source_loader.go -> search/document/source_loader.go
eshu_search_vector_documents.go -> search/document/pending_vector.go   # methods on EshuSearchDocumentStore
eshu_search_vector_query_tuning.go -> search/document/vector_query_tuning.go   # used by the pending-document read and the vector scope state
```

<details><summary>Tests</summary>

```text
eshu_search_document_pending_test.go -> search/document/pending_test.go
eshu_search_document_projection_state_test.go -> search/document/projection_state_test.go
eshu_search_document_source_loader_integration_test.go -> search/document/source_loader_integration_test.go   # external test package: imports root
eshu_search_document_source_loader_streaming_test.go -> search/document/source_loader_streaming_test.go
eshu_search_document_test.go -> search/document/store_test.go
eshu_search_vector_documents_batch_test.go -> search/document/pending_vector_batch_test.go
eshu_search_vector_query_tuning_test.go -> search/document/vector_query_tuning_test.go
```

</details>

### `search/index/` (1 non-test, 4 test)

```text
eshu_search_index.go -> search/index/store.go
```

<details><summary>Tests</summary>

```text
eshu_search_index_bm25_partition_live_test.go -> search/index/store_bm25_partition_live_test.go
eshu_search_index_schema_test.go -> search/index/store_schema_test.go   # external test package: imports root
eshu_search_index_terms_doc_plan_live_test.go -> search/index/store_terms_doc_plan_live_test.go
eshu_search_index_test.go -> search/index/store_test.go
```

</details>

### `search/vector/` (10 non-test, 19 test)

```text
eshu_search_vector_build_ready.go -> search/vector/build_ready.go
eshu_search_vector_fenced_batch.go -> search/vector/fenced_batch.go
eshu_search_vector_metadata.go -> search/vector/metadata.go
eshu_search_vector_metadata_batch.go -> search/vector/metadata_batch.go
eshu_search_vector_metadata_validate.go -> search/vector/metadata_validate.go
eshu_search_vector_pending.go -> search/vector/pending.go
eshu_search_vector_scope_cursor.go -> search/vector/scope_cursor.go
eshu_search_vector_scope_state.go -> search/vector/scope_state.go
eshu_search_vector_scope_state_seed.go -> search/vector/scope_state_seed.go
eshu_search_vector_values.go -> search/vector/values.go
```

<details><summary>Tests</summary>

```text
eshu_search_vector_build_ready_test.go -> search/vector/build_ready_test.go   # external test package + export_test.go shim: imports root
eshu_search_vector_fenced_batch_live_test.go -> search/vector/fenced_batch_live_test.go   # external test package: imports root
eshu_search_vector_fenced_batch_test.go -> search/vector/fenced_batch_test.go
eshu_search_vector_metadata_test.go -> search/vector/metadata_test.go   # external test package: imports root
eshu_search_vector_pending_live_test.go -> search/vector/pending_live_test.go   # external test package + export_test.go shim: imports root
eshu_search_vector_pending_test.go -> search/vector/pending_test.go
eshu_search_vector_scope_state_cas_contention_live_test.go -> search/vector/scope_state_cas_contention_live_test.go   # external test package: imports root
eshu_search_vector_scope_state_cas_interleave_live_test.go -> search/vector/scope_state_cas_interleave_live_test.go
eshu_search_vector_scope_state_count_gate_live_test.go -> search/vector/scope_state_count_gate_live_test.go   # external test package + export_test.go shim: imports root
eshu_search_vector_scope_state_failed_generation_live_test.go -> search/vector/scope_state_failed_generation_live_test.go   # external test package: imports root
eshu_search_vector_scope_state_live_test.go -> search/vector/scope_state_live_test.go   # external test package + export_test.go shim: imports root
eshu_search_vector_scope_state_query_test.go -> search/vector/scope_state_query_test.go
eshu_search_vector_scope_state_schema_test.go -> search/vector/scope_state_schema_test.go   # external test package: imports root
eshu_search_vector_scope_state_seed_test.go -> search/vector/scope_state_seed_test.go
eshu_search_vector_scope_state_store_test.go -> search/vector/scope_state_store_test.go
eshu_search_vector_upsert_batch_scale_live_test.go -> search/vector/upsert_batch_scale_live_test.go   # external test package: imports root
eshu_search_vector_values_scan_live_test.go -> search/vector/values_scan_live_test.go   # external test package: imports root
eshu_search_vector_values_scan_test.go -> search/vector/values_scan_test.go
eshu_search_vector_values_test.go -> search/vector/values_test.go   # external test package: imports root
```

</details>
