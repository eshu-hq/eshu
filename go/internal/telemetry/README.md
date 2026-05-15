# Telemetry

## Purpose

`telemetry` owns Eshu's frozen Go data-plane OpenTelemetry contract: metric instruments, span names,
structured log keys, pipeline phase constants, attribute helpers, OTEL provider
initialization, and the trace-injecting `slog` handler. Every runtime-affecting
package in the data plane imports this package and nothing imports it back.

## Ownership boundary

This package is the single source of truth for all `eshu_dp_*` metric names, all
span name constants (`SpanCollectorObserve`, `SpanProjectorRun`, etc.), and all
log key constants (`LogKeyScopeID`, `LogKeyFailureClass`, etc.). New names are
registered here before being used anywhere else. It does not own queue workers,
graph writers, or HTTP handlers — it only defines the naming contract and the
bootstrapping seams those packages call at startup. Pipeline stage,
graph-backend, and failure-class labels stay here so runtime packages do not
invent local observability vocabularies.

See `CLAUDE.md` §Observability Contract for the project-wide rules that flow
from this package.

## Where this fits in the runtime

```mermaid
flowchart LR
  A["cmd/* entrypoints"] --> B["telemetry.NewBootstrap\ntelemetry.NewProviders"]
  B --> C["*sdktrace.TracerProvider\n*sdkmetric.MeterProvider\nPrometheusHandler"]
  B --> D["telemetry.NewInstruments\n(Instruments struct)"]
  B --> E["telemetry.NewLogger\n(TraceHandler + slog.Logger)"]
  D --> F["internal/projector\ninternal/reducer\ninternal/collector\n..."]
  E --> F
  C --> G["/metrics endpoint\n(Prometheus always active)"]
  C --> H["OTLP gRPC exporters\n(when OTEL_EXPORTER_OTLP_ENDPOINT set)"]
```

## Exported surface

See `doc.go` for the godoc contract. Key groups:

### Bootstrap and providers

- `Bootstrap` — minimum OTEL runtime settings (service name, namespace, meter
  name, tracer name, logger name); built by `NewBootstrap`
- `Providers` — holds `*sdktrace.TracerProvider`, `*sdkmetric.MeterProvider`,
  `PrometheusHandler`, and a combined `Shutdown` function; created by
  `NewProviders`
- `NewProviders` — configures OTLP gRPC trace and metric exporters when
  OTEL_EXPORTER_OTLP_ENDPOINT is set; always creates a Prometheus exporter
- `RecordGOMEMLIMIT` — registers `eshu_dp_gomemlimit_bytes` as an observable
  gauge at startup

### Metric instruments

`Instruments` holds all pre-registered OTEL metric instruments. Create with
`NewInstruments(meter)`. Observable gauges require a separate
`RegisterObservableGauges` call once the queue and worker observers are wired.
`RegisterAcceptanceObservableGauges` adds the `eshu_dp_shared_acceptance_rows`
gauge when a shared-acceptance observer is available.

#### Counters (Int64)

| Field | Metric name |
| --- | --- |
| `FactsEmitted` | `eshu_dp_facts_emitted_total` |
| `FactsCommitted` | `eshu_dp_facts_committed_total` |
| `ProjectionsCompleted` | `eshu_dp_projections_completed_total` |
| `ReducerIntentsEnqueued` | `eshu_dp_reducer_intents_enqueued_total` |
| `ReducerExecutions` | `eshu_dp_reducer_executions_total` |
| `CanonicalWrites` | `eshu_dp_canonical_writes_total` |
| `CanonicalNodesWritten` | `eshu_dp_canonical_nodes_written_total` |
| `CanonicalEdgesWritten` | `eshu_dp_canonical_edges_written_total` |
| `CanonicalAtomicWrites` | `eshu_dp_canonical_atomic_writes_total` |
| `CanonicalAtomicFallbacks` | `eshu_dp_canonical_atomic_fallbacks_total` |
| `SharedProjectionCycles` | `eshu_dp_shared_projection_cycles_total` |
| `SharedProjectionStaleIntents` | `eshu_dp_shared_projection_stale_intents_total` |
| `SharedAcceptanceUpserts` | `eshu_dp_shared_acceptance_upserts_total` |
| `SharedAcceptanceLookupErrors` | `eshu_dp_shared_acceptance_lookup_errors_total` |
| `DocumentationEntityMentions` | `eshu_dp_documentation_entity_mentions_extracted_total` |
| `DocumentationClaimCandidates` | `eshu_dp_documentation_claim_candidates_extracted_total` |
| `DocumentationClaimsSuppressed` | `eshu_dp_documentation_claim_candidates_suppressed_total` |
| `DocumentationDriftFindings` | `eshu_dp_documentation_drift_findings_total` |
| `SharedEdgeWriteGroups` | `eshu_dp_shared_edge_write_groups_total` |
| `CodeCallEdgeBatches` | `eshu_dp_code_call_edge_batches_total` |
| `Neo4jBatchesExecuted` | `eshu_dp_neo4j_batches_executed_total` |
| `Neo4jDeadlockRetries` | `eshu_dp_neo4j_deadlock_retries_total` |
| `ReposSnapshotted` | `eshu_dp_repos_snapshotted_total` |
| `FilesParsed` | `eshu_dp_files_parsed_total` |
| `FactBatchesCommitted` | `eshu_dp_fact_batches_committed_total` |
| `ContentReReads` | `eshu_dp_content_rereads_total` |
| `ContentReReadSkips` | `eshu_dp_content_reread_skips_total` |
| `DiscoveryDirsSkipped` | `eshu_dp_discovery_dirs_skipped_total` |
| `DiscoveryFilesSkipped` | `eshu_dp_discovery_files_skipped_total` |
| `LargeRepoClassifications` | `eshu_dp_large_repo_classifications_total` |
| `EvidenceFactsDiscovered` | `eshu_dp_evidence_facts_discovered_total` |
| `DeferredBackfillEvidence` | `eshu_dp_deferred_backfill_evidence_total` |
| `DeploymentMappingReopened` | `eshu_dp_deployment_mapping_reopened_total` |
| `IaCReachabilityRows` | `eshu_dp_iac_reachability_rows_total` |
| `TerraformStateSnapshotsObserved` | `eshu_dp_tfstate_snapshots_observed_total` |
| `TerraformStateResourcesEmitted` | `eshu_dp_tfstate_resources_emitted_total` |
| `TerraformStateOutputsEmitted` | `eshu_dp_tfstate_outputs_emitted_total` |
| `TerraformStateModulesEmitted` | `eshu_dp_tfstate_modules_emitted_total` |
| `TerraformStateWarningsEmitted` | `eshu_dp_tfstate_warnings_emitted_total` |
| `TerraformStateRedactionsApplied` | `eshu_dp_tfstate_redactions_applied_total` |
| `TerraformStateS3ConditionalGetNotModified` | `eshu_dp_tfstate_s3_conditional_get_not_modified_total` |
| `OCIRegistryAPICalls` | `eshu_dp_oci_registry_api_calls_total` |
| `OCIRegistryTagsObserved` | `eshu_dp_oci_registry_tags_observed_total` |
| `OCIRegistryManifestsObserved` | `eshu_dp_oci_registry_manifests_observed_total` |
| `OCIRegistryReferrersObserved` | `eshu_dp_oci_registry_referrers_observed_total` |
| `PackageRegistryRequests` | `eshu_dp_package_registry_requests_total` |
| `PackageRegistryFactsEmitted` | `eshu_dp_package_registry_facts_emitted_total` |
| `PackageRegistryRateLimited` | `eshu_dp_package_registry_rate_limited_total` |
| `PackageRegistryParseFailures` | `eshu_dp_package_registry_parse_failures_total` |
| `PackageSourceCorrelations` | `eshu_dp_package_source_correlations_total` |
| `ContainerImageIdentityDecisions` | `eshu_dp_container_image_identity_decisions_total` |
| `ConfluenceHTTPRequests` | `eshu_dp_confluence_http_requests_total` |
| `ConfluencePermissionDeniedPages` | `eshu_dp_confluence_permission_denied_pages_total` |
| `ConfluenceDocumentsObserved` | `eshu_dp_confluence_documents_observed_total` |
| `ConfluenceSectionsEmitted` | `eshu_dp_confluence_sections_emitted_total` |
| `ConfluenceLinksEmitted` | `eshu_dp_confluence_links_emitted_total` |
| `ConfluenceSyncFailures` | `eshu_dp_confluence_sync_failures_total` |
| `AWSAPICalls` | `eshu_dp_aws_api_calls_total` |
| `AWSThrottles` | `eshu_dp_aws_throttle_total` |
| `AWSAssumeRoleFailed` | `eshu_dp_aws_assumerole_failed_total` |
| `AWSBudgetExhausted` | `eshu_dp_aws_budget_exhausted_total` |
| `AWSCheckpointEvents` | `eshu_dp_aws_pagination_checkpoint_events_total` |
| `AWSResourcesEmitted` | `eshu_dp_aws_resources_emitted_total` |
| `AWSRelationshipsEmitted` | `eshu_dp_aws_relationships_emitted_total` |
| `AWSTagObservationsEmitted` | `eshu_dp_aws_tag_observations_emitted_total` |
| `AWSFreshnessEvents` | `eshu_dp_aws_freshness_events_total` |
| `CrossRepoEvidenceLoaded` | `eshu_dp_cross_repo_evidence_loaded_total` |
| `CrossRepoEdgesResolved` | `eshu_dp_cross_repo_edges_resolved_total` |
| `CorrelationRuleMatches` | `eshu_dp_correlation_rule_matches_total` |
| `CorrelationDriftDetected` | `eshu_dp_correlation_drift_detected_total` |
| `CorrelationDriftIntentsEnqueued` | `eshu_dp_correlation_drift_intents_enqueued_total` |
| `CorrelationOrphanDetected` | `eshu_dp_correlation_orphan_detected_total` |
| `CorrelationUnmanagedDetected` | `eshu_dp_correlation_unmanaged_detected_total` |
| `DriftUnresolvedModuleCalls` | `eshu_dp_drift_unresolved_module_calls_total` |
| `DriftSchemaUnknownComposite` | `eshu_dp_drift_schema_unknown_composite_total` |
| `WebhookRequests` | `eshu_dp_webhook_requests_total` |
| `WebhookTriggerDecisions` | `eshu_dp_webhook_trigger_decisions_total` |
| `WebhookStoreOperations` | `eshu_dp_webhook_store_operations_total` |

`DriftUnresolvedModuleCalls` uses
`MetricDimensionDriftUnresolvedModuleReason` with the bounded reasons
`external_registry`, `external_git`, `external_archive`, `cross_repo_local`,
`cycle_detected`, `depth_exceeded`, and `module_renamed`. The first six
classify module calls the drift loader cannot resolve locally; `module_renamed`
classifies prior-config projection where the same callee path has different
module prefixes across generations.

#### Histograms (Float64 unless noted)

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
| `ConfluenceFetchDuration` | `eshu_dp_confluence_fetch_duration_seconds` | 0.01–60 s |
| `ScopeAssignDuration` | `eshu_dp_scope_assign_duration_seconds` | default |
| `FactEmitDuration` | `eshu_dp_fact_emit_duration_seconds` | default |
| `ProjectorRunDuration` | `eshu_dp_projector_run_duration_seconds` | 0.1–120 s |
| `ProjectorStageDuration` | `eshu_dp_projector_stage_duration_seconds` | default |
| `ReducerRunDuration` | `eshu_dp_reducer_run_duration_seconds` | default |
| `ReducerQueueWaitDuration` | `eshu_dp_reducer_queue_wait_seconds` | 0.001–21600 s |
| `CanonicalWriteDuration` | `eshu_dp_canonical_write_duration_seconds` | 0.01–60 s |
| `CanonicalProjectionDuration` | `eshu_dp_canonical_projection_duration_seconds` | 0.01–60 s |
| `CanonicalRetractDuration` | `eshu_dp_canonical_retract_duration_seconds` | 0.001–2.5 s |
| `CanonicalBatchSize` | `eshu_dp_canonical_batch_size` | 1–1000 rows |
| `CanonicalPhaseDuration` | `eshu_dp_canonical_phase_duration_seconds` | 0.001–5 s |
| `QueueClaimDuration` | `eshu_dp_queue_claim_duration_seconds` | default |
| `PostgresQueryDuration` | `eshu_dp_postgres_query_duration_seconds` | 0.001–2.5 s |
| `Neo4jQueryDuration` | `eshu_dp_neo4j_query_duration_seconds` | 0.001–10 s |
| `Neo4jBatchSize` | `eshu_dp_neo4j_batch_size` | 1–1000 rows |
| `SharedAcceptanceUpsertDuration` | `eshu_dp_shared_acceptance_upsert_duration_seconds` | default |
| `SharedAcceptanceLookupDuration` | `eshu_dp_shared_acceptance_lookup_duration_seconds` | default |
| `SharedAcceptancePrefetchSize` | `eshu_dp_shared_acceptance_prefetch_size` (Int64) | 1–512 rows |
| `SharedProjectionIntentWaitDuration` | `eshu_dp_shared_projection_intent_wait_seconds` | 0.001–21600 s |
| `SharedProjectionProcessingDuration` | `eshu_dp_shared_projection_processing_seconds` | 0.001–60 s |
| `SharedProjectionStepDuration` | `eshu_dp_shared_projection_step_seconds` | 0.001–60 s |
| `DocumentationDriftGenerationDuration` | `eshu_dp_documentation_drift_generation_duration_seconds` | default |
| `SharedEdgeWriteGroupDuration` | `eshu_dp_shared_edge_write_group_duration_seconds` | 0.001–60 s |
| `SharedEdgeWriteGroupStatementCount` | `eshu_dp_shared_edge_write_group_statement_count` (Int64) | 1–128 stmts |
| `CodeCallEdgeDuration` | `eshu_dp_code_call_edge_batch_duration_seconds` | 0.001–5 s |
| `BatchClaimSize` | `eshu_dp_reducer_batch_claim_size` (Int64) | 1–128 items |
| `RepoSnapshotDuration` | `eshu_dp_repo_snapshot_duration_seconds` | 0.1–300 s |
| `FileParseDuration` | `eshu_dp_file_parse_duration_seconds` | 0.001–2.5 s |
| `GenerationFactCount` | `eshu_dp_generation_fact_count` | 10–300000 facts |
| `LargeRepoSemaphoreWait` | `eshu_dp_large_repo_semaphore_wait_seconds` | 0–300 s |
| `DeferredBackfillDuration` | `eshu_dp_deferred_backfill_duration_seconds` | 0.1–300 s |
| `IaCReachabilityMaterializationDuration` | `eshu_dp_iac_reachability_materialization_duration_seconds` | 0.1–300 s |
| `CrossRepoResolutionDuration` | `eshu_dp_cross_repo_resolution_duration_seconds` | 0.01–30 s |
| `PipelineOverlapDuration` | `eshu_dp_pipeline_overlap_seconds` | 1–1800 s |

#### Observable gauges

| Field | Metric name | Dimensions |
| --- | --- | --- |
| `QueueDepth` | `eshu_dp_queue_depth` | `queue`, `status` |
| `QueueOldestAge` | `eshu_dp_queue_oldest_age_seconds` | `queue` |
| `WorkerPoolActive` | `eshu_dp_worker_pool_active` | `pool` |
| `SharedAcceptanceRows` | `eshu_dp_shared_acceptance_rows` | none |
| (via `RecordGOMEMLIMIT`) | `eshu_dp_gomemlimit_bytes` | none |

### Span name constants

Defined in `contract.go` and small companion files such as
`contract_query_spans.go`. Use `telemetry.SpanXxx` rather than string literals;
new query routes such as hardcoded-secret investigation register their span name
here before handlers use it.

Pipeline spans: `SpanCollectorObserve`, `SpanCollectorStream`, `SpanScopeAssign`,
`SpanFactEmit`, `SpanProjectorRun`, `SpanReducerIntentEnqueue`, `SpanReducerRun`,
`SpanReducerBatchClaim`, `SpanReducerDriftEvidenceLoad`,
`SpanCanonicalWrite`, `SpanCanonicalProjection`,
`SpanCanonicalRetract`, `SpanEvidenceDiscovery`,
`SpanIaCReachabilityMaterialization`, `SpanSQLRelationshipMaterialization`,
`SpanInheritanceMaterialization`, `SpanCrossRepoResolution`,
`SpanSharedAcceptanceLookup`, `SpanSharedAcceptanceUpsert`,
`SpanQueryRelationshipEvidence`, `SpanQueryEvidenceCitationPacket`,
`SpanQueryDocumentationFindings`,
`SpanQueryDocumentationEvidencePacket`, `SpanQueryDocumentationPacketFreshness`,
`SpanQueryDeadIaC`, `SpanQueryIaCUnmanagedResources`,
`SpanQueryIaCManagementStatus`, `SpanQueryIaCManagementExplanation`,
`SpanQueryIaCTerraformImportPlan`,
`SpanQueryInfraResourceSearch`, `SpanQueryCodeTopicInvestigation`,
`SpanQueryHardcodedSecretInvestigation`, `SpanQueryDeadCodeInvestigation`,
`SpanQueryCallGraphMetrics`, `SpanQueryChangeSurfaceInvestigation`,
`SpanQueryPackageRegistryPackages`, `SpanQueryPackageRegistryVersions`,
`SpanQueryPackageRegistryDependencies`,
`SpanTerraformStateClaimProcess`,
`SpanTerraformStateDiscoveryResolve`, `SpanTerraformStateSourceOpen`,
`SpanTerraformStateParserStream`, `SpanTerraformStateFactEmitBatch`,
`SpanTerraformStateCoordinatorDone`, `SpanWebhookHandle`, `SpanWebhookStore`,
`SpanOCIRegistryScan`, and `SpanOCIRegistryAPICall`.

Dependency spans: `SpanPostgresExec`, `SpanPostgresQuery`, `SpanNeo4jExecute`.

The full frozen list is also accessible at runtime via `SpanNames()`.

### Log keys and phase constants

Log keys (all frozen in `contract.go`): `LogKeyScopeID`, `LogKeyScopeKind`,
`LogKeySourceSystem`, `LogKeyGenerationID`, `LogKeyCollectorKind`,
`LogKeyDomain`, `LogKeyPartitionKey`, `LogKeyRequestID`, `LogKeyFailureClass`,
`LogKeyRefreshSkipped`, `LogKeyPipelinePhase`, `LogKeyAcceptanceScopeID`,
`LogKeyAcceptanceUnitID`, `LogKeyAcceptanceSourceRunID`,
`LogKeyAcceptanceGenerationID`, `LogKeyAcceptanceStaleCount`.

Drift-specific log keys (also frozen in `contract.go`):
`LogKeyDriftPriorConfigDepth`, `LogKeyDriftPriorConfigAddresses`,
`LogKeyDriftStateOnlyAddresses`, `LogKeyDriftAddressesPromoted` carry the
prior-config walk summary emitted by `PostgresDriftEvidenceLoader`.
`LogKeyDriftMultiElementPrefix`, `LogKeyDriftMultiElementCount`, and
`LogKeyDriftMultiElementSource` flag the first-wins truncation policy applied
to multi-element repeated nested blocks — emitted at debug level from both
`flattenStateAttributes`
(`internal/storage/postgres/tfstate_drift_evidence_state_row.go:90`) and
`walkBlockAttributes`
(`internal/parser/hcl/terraform_resource_attributes.go:132`).

Pipeline phase constants (defined in `logging.go`): `PhaseDiscovery`,
`PhaseParsing`, `PhaseEmission`, `PhaseProjection`, `PhaseReduction`,
`PhaseShared`, `PhaseQuery`, `PhaseServe`.

### Attribute and log helpers

Attribute helpers — typed constructors for every metric dimension key, for
example `AttrDomain`, `AttrScopeID`, `AttrWritePhase`; use these rather than
`attribute.String` literals when recording metrics.
Webhook listener labels use `AttrProvider`, `AttrEventKind`, `AttrDecision`,
`AttrStatus`, `AttrOutcome`, and `AttrReason` so provider intake stays on the
same bounded vocabulary as the rest of the data plane.

`ScopeAttrs`, `DomainAttrs`, `AcceptanceAttrs` — return `[]slog.Attr` slices
for the common scope, domain, and acceptance-context log fields.

`PhaseAttr`, `FailureClassAttr`, `AcceptanceStaleCountAttr`, `EventAttr` —
single-key `slog.Attr` constructors for the most frequently repeated log fields.

### Logging

`NewLogger` and `NewLoggerWithWriter` — construct a JSON `slog.Logger` backed
by `TraceHandler`. The handler injects `trace_id`, `span_id`, and
`severity_number` from the active OTEL span context on every record. Base
attributes `service_name`, `service_namespace`, `component`, and `runtime_role`
are attached at logger creation.

### Refresh counter

`RecordSkippedRefresh`, `SkippedRefreshCount` — process-local atomic counter for
incremental-refresh skip tracking. Not a metric; used only for status-surface
reporting.

## Dependencies

No internal Eshu package imports. External dependencies:

- `go.opentelemetry.io/otel/{metric,trace}` — OTEL API
- `go.opentelemetry.io/otel/sdk/{metric,trace}` — OTEL SDK providers
- `go.opentelemetry.io/otel/exporters/otlp/...grpc` — OTLP gRPC exporters
- `go.opentelemetry.io/otel/exporters/prometheus` — Prometheus bridge
- `github.com/prometheus/client_golang/prometheus` — Prometheus registry

This is a leaf package. Introducing any `go/internal/*` import here creates a
circular dependency and must not happen.

## Telemetry

This package defines the telemetry contract. It emits nothing itself at
runtime; all emission happens in the packages that consume `Instruments`.

## Gotchas / invariants

- `NewInstruments` registers every counter and histogram but does not wire
  observable gauges. Call `RegisterObservableGauges` after the queue and worker
  implementations are ready, otherwise `eshu_dp_queue_depth`,
  `eshu_dp_queue_oldest_age_seconds`, and `eshu_dp_worker_pool_active` will not
  appear on `/metrics`.
- Observable gauges are registered exactly once per process. Calling
  `RegisterObservableGauges` more than once for the same meter produces
  duplicate-instrument errors from the OTEL SDK.
- The Prometheus exporter uses its own `prometheus.NewRegistry()` (not the
  default registry), so it is isolated from any third-party code that
  registers on the default registry.
- The Prometheus exporter is constructed with
  `otelprom.WithResourceAsConstantLabels` keyed to allow `service.name`
  and `service.namespace` only. Dropping or narrowing that filter breaks
  every dashboard that filters by `service_name` or `service_namespace`.
  The regression gate lives in `provider_resource_labels_test.go` (test
  TestPrometheusExposesServiceLabelsOnMetrics). The default exporter
  behavior leaves those attributes on `target_info` alone, so a Grafana
  template variable scoped to a data-plane selector like
  `label_values(eshu_dp_facts_emitted_total, service_name)` returns
  empty even though `label_values(target_info, service_name)` works.
- `TraceHandler` only injects `trace_id` and `span_id` when a valid span is
  active in the context. Log lines emitted outside any span do not carry trace
  fields; this is expected.
- `RecordGOMEMLIMIT` silently no-ops when `meter` is nil, so callers that have
  not yet initialized OTEL do not crash.
- High-cardinality values — repository paths, fact IDs, work-item IDs — belong
  in spans or log fields, never in metric label values. Metric labels must stay
  bounded.
- All metric names are frozen once registered. Renaming a metric name requires
  coordinating with all dashboards and alert rules; prefer adding a new name
  over renaming.

## Related docs

- `docs/docs/reference/telemetry/index.md` — operator-facing metric, span, and
  log reference with tuning guidance
- `docs/docs/architecture.md` — pipeline ownership table
- `docs/docs/deployment/service-runtimes.md` — how each binary bootstraps OTEL
