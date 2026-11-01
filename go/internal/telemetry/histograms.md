# Telemetry Histograms

Histogram instruments on `Instruments` (Float64 unless noted), with their
registered metric names and bucket ranges. Moved out of `README.md` to keep
that file under the 500-line cap; the counters and gauges stay there.

| Field | Metric name | Custom buckets |
| --- | --- | --- |
| `CollectorObserveDuration` | `eshu_dp_collector_observe_duration_seconds` | 0.01–60 s |
| `TerraformStateClaimWaitDuration` | `eshu_dp_tfstate_claim_wait_seconds` | 0–3600 s |
| `TerraformStateSnapshotBytes` | `eshu_dp_tfstate_snapshot_bytes` | 1 KiB–100 MiB |
| `TerraformStateParseDuration` | `eshu_dp_tfstate_parse_duration_seconds` | 0.001–10 s |
| `WebhookRequestDuration` | `eshu_dp_webhook_request_duration_seconds` | 0.001–10 s |
| `WebhookStoreDuration` | `eshu_dp_webhook_store_duration_seconds` | 0.001–10 s |
| `OCIRegistryScanDuration` | `eshu_dp_oci_registry_scan_duration_seconds` | 0.05–120 s |
| `PackageRegistryObserveDuration` | `eshu_dp_package_registry_observe_duration_seconds` | 0.01–60 s |
| `PackageRegistryGenerationLag` | `eshu_dp_package_registry_generation_lag_seconds` | 0.01–60 s |
| `VulnerabilityIntelligenceFetchDuration` | `eshu_dp_vulnerability_intelligence_fetch_duration_seconds` (`page_count` 0-100) | 0.01-60 s |
| `SecurityAlertFetchDuration` | `eshu_dp_security_alert_fetch_duration_seconds` | 0.01–60 s |
| `CICDRunFetchDuration`, `CICDRunWebhookLag` | `eshu_dp_ci_cd_run_fetch_duration_seconds`, `eshu_dp_ci_cd_run_webhook_lag_seconds` | 0.01–60 s fetch; 0–21600 s webhook lag |
| `PagerDutyFetchDuration` | `eshu_dp_pagerduty_fetch_duration_seconds` | 0.01-60 s |
| `PagerDutyGenerationLag` | `eshu_dp_pagerduty_generation_lag_seconds` | 0.01–60 s |
| `JiraFetchDuration` | `eshu_dp_jira_fetch_duration_seconds` | 0.01–60 s |
| `GrafanaFetchDuration` | `eshu_dp_grafana_fetch_duration_seconds` | 0.01–60 s |
| `PrometheusMimirFetchDuration` | `eshu_dp_prometheus_mimir_fetch_duration_seconds` | 0.01–60 s |
| `LokiFetchDuration` | `eshu_dp_loki_fetch_duration_seconds` | 0.01–60 s |
| `TempoFetchDuration` | `eshu_dp_tempo_fetch_duration_seconds` | 0.01–60 s |
| `ScannerWorkerQueueWaitDuration` | `eshu_dp_scanner_worker_queue_wait_seconds` | 0.001–21600 s |
| `ScannerWorkerScanDuration` | `eshu_dp_scanner_worker_scan_duration_seconds` | 0.05–1200 s |
| `ScannerWorkerTargetCount` | `eshu_dp_scanner_worker_target_count` (Int64) | 1–100000 targets |
| `ScannerWorkerResultCount` | `eshu_dp_scanner_worker_result_count` (Int64) | 1–100000 results |
| `ScannerWorkerCPUSeconds` | `eshu_dp_scanner_worker_cpu_seconds` | 0.01–1800 s |
| `ScannerWorkerMemoryBytes` | `eshu_dp_scanner_worker_memory_bytes` (Int64) | 1 MiB–16 GiB |
| `ConfluenceFetchDuration` | `eshu_dp_confluence_fetch_duration_seconds` | 0.01–60 s |
| `ScopeAssignDuration` | `eshu_dp_scope_assign_duration_seconds` | 0.005–30 s |
| `FactEmitDuration` | `eshu_dp_fact_emit_duration_seconds` | 0.1–300 s |
| `ProjectorRunDuration` | `eshu_dp_projector_run_duration_seconds` | 0.1–120 s |
| `ProjectorStageDuration` | `eshu_dp_projector_stage_duration_seconds` | 0.001–120 s |
| `ProjectorAckWaitDuration` | `eshu_dp_projector_ack_wait_seconds` | 1–600 s |
| `ProjectorWriteMarkerWaitDuration` | `eshu_dp_projector_write_marker_wait_seconds` | 1–600 s |
| `ReducerRunDuration` | `eshu_dp_reducer_run_duration_seconds` | 0.05–900 s |
| `SearchIndexWriteDuration` | `eshu_dp_search_index_write_duration_seconds` | 0.001–21600 s |
| `ReducerQueueWaitDuration` | `eshu_dp_reducer_queue_wait_seconds` | 0.001–21600 s |
| `GCPMaterializationDuration` | `eshu_dp_gcp_materialization_duration_seconds` | 0.001–21600 s |
| `GenerationRetentionDuration` | `eshu_dp_generation_retention_duration_seconds` | 0.001–900 s |
| `GenerationRetentionBatchSize` | `eshu_dp_generation_retention_batch_size` (Int64) | 1–100 generations |
| `GenerationRetentionOldestEligibleAge` | `eshu_dp_generation_retention_oldest_eligible_age_seconds` | 1h–90d |
| `CanonicalWriteDuration` | `eshu_dp_canonical_write_duration_seconds` | 0.01–60 s |
| `CanonicalProjectionDuration` | `eshu_dp_canonical_projection_duration_seconds` | 0.01–60 s |
| `QueueClaimDuration` | `eshu_dp_queue_claim_duration_seconds` | 0.001–30 s |
| `PostgresQueryDuration` | `eshu_dp_postgres_query_duration_seconds` | 0.001–300 s (top buckets 5, 10, 30, 60, 120, 300 s for planner full scans, #9422) |
| `Neo4jQueryDuration` | `eshu_dp_neo4j_query_duration_seconds` | 0.001–10 s |
| `StatusSnapshotReadDuration` | `eshu_dp_status_snapshot_read_duration_seconds` | 0.001–10 s; `read` (closed reader set), `outcome` |
| `RelationshipBreakdownPermitWaitDuration` | `eshu_dp_relationship_breakdown_permit_wait_seconds` | 0–30 s; no labels |
| `RelationshipBreakdownQueued` | `eshu_dp_relationship_breakdown_queued` (Int64 UpDownCounter) | current waiters; no labels |
| `RelationshipBreakdownInFlight` | `eshu_dp_relationship_breakdown_in_flight` (Int64 UpDownCounter) | current permit holders; no labels |
| `Neo4jBatchSize` | `eshu_dp_neo4j_batch_size` | 1–1000 rows |
| `SharedAcceptanceUpsertDuration` | `eshu_dp_shared_acceptance_upsert_duration_seconds` | 0.001–30 s |
| `SharedAcceptanceLookupDuration` | `eshu_dp_shared_acceptance_lookup_duration_seconds` | 0.001–30 s |
| `SharedProjectionIntentWaitDuration` | `eshu_dp_shared_projection_intent_wait_seconds` | 0.001–21600 s |
| `SharedProjectionProcessingDuration` | `eshu_dp_shared_projection_processing_seconds` | 0.001–60 s |
| `SharedProjectionStepDuration` | `eshu_dp_shared_projection_step_seconds` | 0.001–60 s |
| `DocumentationDriftGenerationDuration` | `eshu_dp_documentation_drift_generation_duration_seconds` | 0.001–60 s |
| `SharedEdgeWriteGroupDuration` | `eshu_dp_shared_edge_write_group_duration_seconds` | 0.001–60 s |
| `SharedEdgeWriteGroupStatementCount` | `eshu_dp_shared_edge_write_group_statement_count` (Int64) | 1–128 stmts |
| `CodeCallEdgeDuration` | `eshu_dp_code_call_edge_batch_duration_seconds` | 0.001–5 s |
| `BatchClaimSize` | `eshu_dp_reducer_batch_claim_size` (Int64) | 1–128 items |
| `RepoSnapshotDuration` | `eshu_dp_repo_snapshot_duration_seconds` | 0.1–300 s |
| `FileParseDuration` | `eshu_dp_file_parse_duration_seconds` | 0.001–2.5 s |
| `FilePreScanDuration` | `eshu_dp_file_prescan_duration_seconds` (labels: `language`) | 0.001–2.5 s |
| `GenerationFactCount` | `eshu_dp_generation_fact_count` | 10–300000 facts |
| `SCIPProcessWaitDuration` | `eshu_dp_scip_process_wait_seconds` | 0–60 s |
| `LargeRepoSemaphoreWait` | `eshu_dp_large_repo_semaphore_wait_seconds` | 0–300 s |
| `DeferredBackfillDuration` | `eshu_dp_deferred_backfill_duration_seconds` | 0.1–300 s |
| `DeferredBackfillBatchDuration` | `eshu_dp_deferred_backfill_batch_duration_seconds` | 0.1–300 s |
| `DeferredBackfillPartitionLoadDuration` | `eshu_dp_deferred_backfill_partition_load_duration_seconds` | 0.1–300 s |
| `DeferredBackfillPartitionLoadFactCount` | `eshu_dp_deferred_backfill_partition_load_fact_count` (Int64) | 0–100000 facts; no labels |
| `IaCReachabilityMaterializationDuration` | `eshu_dp_iac_reachability_materialization_duration_seconds` | 0.1–300 s |
| `CrossRepoResolutionDuration` | `eshu_dp_cross_repo_resolution_duration_seconds` | 0.01–30 s |
| `PipelineOverlapDuration` | `eshu_dp_pipeline_overlap_seconds` | 1–1800 s |
| `WorkflowClaimRunDuration` | `eshu_dp_workflow_claim_run_duration_seconds` (labels: `collector_kind`, `source_system`, `outcome`) | 0.1–1800 s |
| `OCRRecognitionDuration` | `eshu_dp_ocr_recognition_duration_seconds` (labels: `outcome`) | 0.05–60 s |
| `OCRPeakRSS` | `eshu_dp_ocr_engine_peak_rss_bytes` (Int64; sampled child VmHWM, #9728) | 16 MiB–2 GiB |

`Neo4jQueryDuration` records each logical graph read with `operation="read"`
and one bounded outcome: `success`, `slow`, `recovered`, `deadline`,
`caller_deadline`, `unavailable`, `canceled`, or `error`. The matching
`neo4j.query` span uses the same outcome and adds only the bounded attempt count
and configured deadline; query text and raw driver errors are excluded.

`WorkflowClaimRunDuration` records the wall time of one claimed-service
processing cycle (`ClaimedService.processClaimed`) for every collector family,
recorded via `defer` on every return path. `outcome` is a bounded enum
(`success`, `unchanged`, `released`, `fail_retryable`, `fail_terminal`). It is
the per-collector long-pole signal: `sum by (collector_kind)` of `_sum` over
`_count` gives mean run duration per family. It shares the `collector_kind`
label with the #3678 per-stage `eshu_dp_bootstrap_pipeline_phase_seconds` so the
per-collector and per-phase layers join cleanly. The matching trace span is
`collector.claimed_run`, with the same `collector_kind`, `source_system`, and `outcome` attributes.

`WorkflowClaimFactsEmitted` (`eshu_dp_workflow_claim_facts_emitted_total`, labels
`collector_kind` and `source_system`) counts facts committed per claimed-service
run from `CollectedGeneration.FactCount`, recorded on the success path only — no
extra scan or IO. `sum by (collector_kind)` of its rate gives facts/second per
collector; paired with `WorkflowClaimRunDuration` an operator sees whether a slow
collector is IO-bound or emission-heavy, and it joins the #3678
`eshu_dp_content_entity_emitted_total` (`source_file_kind`) counter for a
per-collector and per-file-kind volume breakdown.
