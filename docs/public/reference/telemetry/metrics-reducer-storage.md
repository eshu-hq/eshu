# Reducer And Storage Metrics

This catalog covers reducer execution, shared follow-up, graph writes, storage,
correlation, supply-chain impact, capacity, and memory metrics.

## Reducer Execution

| Metric | Type | Use |
| --- | --- | --- |
| `eshu_dp_reducer_intents_enqueued_total` | counter | Reducer intent enqueue volume by domain. |
| `eshu_dp_reducer_executions_total` | counter | Reducer execution volume by domain and status. |
| `eshu_dp_reducer_run_duration_seconds` | histogram | Handler execution window after a worker starts a work item. |
| `eshu_dp_reducer_queue_wait_seconds` | histogram | Time visible in the reducer queue before handler start. |
| `eshu_dp_reducer_batch_claim_size` | histogram | Batch claim size where batched reducer claiming is used. |

Compare queue wait with run duration before changing worker counts. High queue
age with low run duration points to claim, routing, or conflict-domain pressure.
High run duration points to the handler, store, or graph-write path.

## Shared Follow-Up And Acceptance

| Metric | Type | Use |
| --- | --- | --- |
| `eshu_dp_shared_projection_cycles_total` | counter | Shared projection partition cycles by domain and partition key. |
| `eshu_dp_shared_projection_intent_wait_seconds` | histogram | Maximum selected intent age for a partition cycle. |
| `eshu_dp_shared_projection_processing_seconds` | histogram | Graph-write and completion duration after partition selection. |
| `eshu_dp_shared_projection_step_seconds` | histogram | Shared projection substeps such as selection, load, retract, write, replay, and mark-completed. |
| `eshu_dp_shared_projection_stale_intents_total` | counter | Stale shared projection intents filtered during processing. |
| `eshu_dp_shared_acceptance_lookup_duration_seconds` | histogram | Shared acceptance lookup latency. |
| `eshu_dp_shared_acceptance_lookup_errors_total` | counter | Shared acceptance lookup failures. |
| `eshu_dp_shared_acceptance_upsert_duration_seconds` | histogram | Shared acceptance write latency. |
| `eshu_dp_shared_acceptance_upserts_total` | counter | Shared acceptance write volume. |
| `eshu_dp_shared_acceptance_prefetch_size` | histogram | Shared acceptance prefetch size. |
| `eshu_dp_shared_acceptance_rows` | observable gauge | Durable shared acceptance row count. |
| `eshu_dp_shared_edge_write_groups_total` | counter | Shared edge write group volume. |
| `eshu_dp_shared_edge_write_group_duration_seconds` | histogram | Shared edge write group latency. |
| `eshu_dp_shared_edge_write_group_statement_count` | histogram | Statements per shared edge write group. |
| `eshu_dp_code_call_edge_batches_total` | counter | Isolated code-call edge batch volume. |
| `eshu_dp_code_call_edge_batch_duration_seconds` | histogram | Isolated code-call edge batch latency. |

These metrics are domain-scoped. Use traces and logs when you need repository
or generation context.

## Storage And Graph Writes

| Metric | Type | Use |
| --- | --- | --- |
| `eshu_dp_postgres_query_duration_seconds` | histogram | Postgres query and exec latency from the instrumented wrapper. |
| `eshu_dp_neo4j_query_duration_seconds` | histogram | Neo4j/NornicDB Bolt query latency from the graph wrapper. |
| `eshu_dp_neo4j_deadlock_retries_total` | counter | Legacy graph-write retry counter for deadlocks, lock timeouts, driver connectivity failures, and retryable NornicDB commit conflicts. |
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

Use graph/storage metrics before tuning NornicDB row caps, Neo4j batch sizes, or
worker counts.

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
| `eshu_dp_iac_reachability_rows_total` | counter | IaC usage rows materialized after projection drains. |
| `eshu_dp_iac_reachability_materialization_duration_seconds` | histogram | Corpus-wide IaC reachability materialization cost. |
| `eshu_dp_cross_repo_resolution_duration_seconds` | histogram | Cross-repo relationship resolution latency. |
| `eshu_dp_cross_repo_evidence_loaded_total` | counter | Evidence rows loaded for cross-repo resolution. |
| `eshu_dp_cross_repo_edges_resolved_total` | counter | Cross-repo edges resolved. |
| `eshu_dp_evidence_facts_discovered_total` | counter | Evidence facts discovered during ingestion. |

`eshu_dp_drift_unresolved_module_calls_total` uses the closed reasons
`external_registry`, `external_git`, `external_archive`, `cross_repo_local`,
`cycle_detected`, `depth_exceeded`, and `module_renamed`.

## Package, Image, CI/CD, And Supply Chain Correlation

| Metric | Type | Use |
| --- | --- | --- |
| `eshu_dp_package_source_correlations_total` | counter | Package source-correlation decisions by reducer domain and outcome. |
| `eshu_dp_container_image_identity_decisions_total` | counter | Container image identity decisions by reducer domain and outcome. |
| `eshu_dp_ci_cd_run_correlations_total` | counter | CI/CD run correlation decisions by reducer domain and outcome. |
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
