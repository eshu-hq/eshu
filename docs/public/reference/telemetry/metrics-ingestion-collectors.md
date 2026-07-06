# Ingestion And Collector Metrics

This catalog covers metrics emitted before reducer-owned materialization:
source collection, discovery pruning, fact emission, registry/cloud collectors,
Confluence, workflow coordination, and webhook intake.

Most instruments are registered in `go/internal/telemetry/instruments.go`.
Terraform-state discovery registers `eshu_dp_tfstate_discovery_candidates_total`
in `go/internal/collector/terraformstate/metrics.go`, and workflow-coordinator
loop metrics live in `go/internal/coordinator/metrics.go`.

## Git, Discovery, And Fact Streaming

| Metric | Use |
| --- | --- |
| `eshu_dp_repos_snapshotted_total` | Repository snapshot completion volume. |
| `eshu_dp_files_parsed_total` | File parse volume. |
| `eshu_dp_repo_snapshot_duration_seconds` | Per-repository snapshot cost (whole snapshot). |
| `eshu_dp_collector_snapshot_stage_duration_seconds` | Per-stage snapshot cost within one repository, labeled by `collector_kind` and bounded `stage` (`discovery`, `pre_scan`, `go_package_semantic_prescan`, `parse`, `materialize`, `value_flow_evidence`). Use it to attribute a slow `eshu_dp_repo_snapshot_duration_seconds` to a specific stage — parse and SCIP cost shows under `parse`, and taint/interprocedural/function-summary value-flow cost shows under `value_flow_evidence`. Repository and file paths stay in logs/spans, never metric labels. |
| `eshu_dp_file_parse_duration_seconds` | Per-file parse cost. |
| `eshu_dp_file_prescan_duration_seconds` | Per-file pre_scan cost, labeled by bounded `language`. Only files that actually dispatch to a language pre-scanner emit a sample — `parser.IsDerivedPreScanLanguage` languages (php, javascript, typescript, tsx) derive their ImportsMap contribution from the parse stage on a full ingest (#4764) and so contribute no samples there; a delta sync still runs the legacy pre_scan pass for every language and so does emit samples for those too. Pairs with the `pre_scan` stage's `language_prescan_summary` structured-log bucket (mirrors the parse stage's `language_parse_summary`) to attribute pre_scan cost per language the same way parse cost is already attributed. |
| `eshu_dp_scip_snapshot_attempts_total` | SCIP supplement attempt volume per selected language package or workspace root, labeled by bounded `language` and `result` (`used`, `disabled`, `no_supported_language`, `binary_unavailable`, `indexer_failed`, `parse_failed`, or `empty_result`). A sustained non-`used` rate means call precision is falling back to native parser output; investigate SCIP binary availability, indexer errors, parser errors, language allowlists, or empty index output. Repository names, root paths, file paths, and index paths stay out of labels. |
| `eshu_dp_scip_process_wait_seconds` | Time spent waiting for the shared SCIP process limiter before launching an external indexer, labeled only by bounded `language`. Sustained wait growth means `SCIP_WORKERS` is saturated across concurrent repository snapshots; either raise the worker budget with CPU/memory proof, narrow `SCIP_LANGUAGES`, or lower snapshot concurrency. |
| `eshu_dp_discovery_dirs_skipped_total` | Directory pruning by discovery policy. |
| `eshu_dp_discovery_files_skipped_total` | File pruning by discovery policy. |
| `eshu_dp_large_repo_classifications_total` | Large-repo classification volume. |
| `eshu_dp_large_repo_semaphore_wait_seconds` | Wait time for large-repo concurrency control. |
| `eshu_dp_facts_emitted_total` | Collector fact output volume. |
| `eshu_dp_facts_committed_total` | Durable fact commit volume. |
| `eshu_dp_fact_batches_committed_total` | Streaming multi-row fact batch commits. |
| `eshu_dp_generation_fact_count` | Fact volume per generation. |
| `eshu_dp_content_rereads_total` | Content reloads in the projection path. |
| `eshu_dp_content_reread_skips_total` | Content reloads avoided by reuse. |
| `eshu_dp_collector_delta_baseline_fallback_total` | Git delta syncs that fell back to a full snapshot, labeled by `skip_reason` (`no_projected_baseline`, `baseline_unreachable`, `baseline_lookup_error`). A sustained rate means delta sync is rarely applying; investigate clone depth, repeated projection failures, or (for `baseline_lookup_error`) Postgres availability. |
| `eshu_dp_collector_reconciliation_full_snapshots_total` | Git scopes the periodic sweep forced to a full reconciliation snapshot to retract drift the delta path missed. A steady low rate is expected (one per scope per `ESHU_REPO_RECONCILE_INTERVAL_HOURS`); a spike means many scopes came due at once. |
| `eshu_dp_reconciliation_drift_retractions_total` | Graph nodes and edges actually retracted by those forced reconciliation snapshots, labeled by bounded `domain`, `write_phase`, and `kind` (`node` or `edge`). A sustained nonzero rate after the first reconciliation window means delta-path cleanup is repeatedly finding stale graph state; investigate failed delta projections, graph write errors, or missed deletion events. Scope and repo identifiers stay in logs/spans, not metric labels. |

Content-aware skip reasons use the `content:` prefix. Repo-local
`.eshu/discovery.json` rules use the `user:` prefix. `.eshuignore` matches use
`skip_reason=eshuignore`.

No-Regression Evidence: `go test ./internal/collector ./internal/telemetry
-count=1` (468 tests, NornicDB-independent unit path) proves the git-collector
snapshot path records `eshu_dp_collector_snapshot_stage_duration_seconds` once
per bounded stage and emits one `collector.snapshot_stage` span per stage. The
per-stage timing is pure instrumentation around already-executed snapshot work:
it adds one `time.Now()` read, one histogram `Record`, and one zero-duration
back-dated span per stage (six stages per repository), so it introduces no new
graph write, Cypher, queue, lease, batch, or worker behavior and no measurable
snapshot wall-time cost on the collector path.

Observability Evidence: a slow `eshu_dp_repo_snapshot_duration_seconds` can now
be attributed to a specific snapshot stage through
`eshu_dp_collector_snapshot_stage_duration_seconds{collector_kind,stage}` and
the `collector.snapshot_stage` span, where `stage` is one of the bounded values
`discovery`, `pre_scan`, `go_package_semantic_prescan`, `parse`, `materialize`,
or `value_flow_evidence`. Per-stage structured logs carry the repository and
file counts; repository and file paths stay in logs and spans and never appear
as metric labels, so the histogram and span set stay low-cardinality. When the
collector runs without an instruments meter or tracer the path is a safe no-op,
proven by `TestSnapshotRepositoryStageTelemetryNoInstrumentsNoPanic`.

## Terraform-State Collector

| Metric | Key labels | Use |
| --- | --- | --- |
| `eshu_dp_workflow_claim_wait_seconds` | `collector_kind`, `source_system` | Work-item age when any claim-aware collector starts processing a claim. |
| `eshu_dp_tfstate_claim_wait_seconds` | `collector_kind`, `source_system` | Work-item age when a state claim starts. |
| `eshu_dp_tfstate_discovery_candidates_total` | `source` | Candidate sources accepted before opening a state file. |
| `eshu_dp_tfstate_snapshots_observed_total` | `backend_kind`, `result` | State source observations. |
| `eshu_dp_tfstate_snapshot_bytes` | bounded backend/result labels | State source size. |
| `eshu_dp_tfstate_parse_duration_seconds` | bounded backend/result labels | Streaming parser cost. |
| `eshu_dp_tfstate_resources_emitted_total` | `backend_kind` | Resource fact volume. |
| `eshu_dp_tfstate_outputs_emitted_total` | `safe_locator_hash` | Output fact volume without raw locators. |
| `eshu_dp_tfstate_modules_emitted_total` | `safe_locator_hash` | Module observation fact volume without raw locators. |
| `eshu_dp_tfstate_warnings_emitted_total` | `warning_kind`, `safe_locator_hash` | Parser or policy warning volume. |
| `eshu_dp_tfstate_redactions_applied_total` | `reason` | Redaction or safe-drop decisions. |
| `eshu_dp_tfstate_s3_conditional_get_not_modified_total` | none | S3 conditional reads that avoided work. |
| `eshu_dp_tfstate_schema_resolver_entries` | none | Loaded provider-schema resolver entry count. |

Raw bucket names, object keys, local paths, and full locators must not appear in
metric labels.

For unsupported Terraform-state composite attributes, the parser emits one
`warning_kind=unsupported_composite_attribute` summary fact per
`resource_type`/`attribute_key`/`reason` shape with an `occurrence_count`.
Other composite safe drops use `warning_kind=composite_attribute_skipped` with
the same shape fields.
Use `eshu_dp_drift_schema_unknown_composite_total{resource_type,reason}` for
per-occurrence skip volume; `attribute_key` stays in warning facts and bounded
structured logs, not metric labels.

## OCI And Package Registry Collectors

| Metric | Key labels | Use |
| --- | --- | --- |
| `eshu_dp_oci_registry_api_calls_total` | `provider`, `operation`, `result` | Registry API volume and failures. |
| `eshu_dp_oci_registry_tags_observed_total` | `provider`, `result` | Tags accepted into bounded scans. |
| `eshu_dp_oci_registry_manifests_observed_total` | `provider`, `media_family` | Manifest, index, and descriptor observations. |
| `eshu_dp_oci_registry_referrers_observed_total` | `provider`, `artifact_family` | SBOM, signature, attestation, vulnerability, or unknown referrer evidence. |
| `eshu_dp_oci_registry_scan_duration_seconds` | `provider`, `result` | One repository scan before durable commit. |
| `eshu_dp_package_registry_requests_total` | `ecosystem`, `status_class` | Metadata request attempts. |
| `eshu_dp_package_registry_facts_emitted_total` | `ecosystem`, `fact_kind` | Parser output volume. |
| `eshu_dp_package_registry_rate_limited_total` | `ecosystem` | HTTP 429 pressure. |
| `eshu_dp_package_registry_parse_failures_total` | `ecosystem`, `document_type` | Metadata parse failures. |
| `eshu_dp_package_registry_observe_duration_seconds` | bounded ecosystem/result labels | Claimed target observation cost. |
| `eshu_dp_package_registry_generation_lag_seconds` | bounded ecosystem/result labels | Source observation lag. |

Registry hosts, repositories, tags, digests, package names, versions, feed URLs,
artifact paths, and credentials stay out of labels.

## Vulnerability Intelligence Collector

| Metric | Key labels | Use |
| --- | --- | --- |
| `eshu_dp_vulnerability_intelligence_observations_total` | `source`, `status_class` | Source target observations, including success, errors, and rate limits. |
| `eshu_dp_vulnerability_intelligence_facts_emitted_total` | `source`, `fact_kind` | Source fact volume emitted per bounded target. |
| `eshu_dp_vulnerability_intelligence_rate_limited_total` | `source` | HTTP 429 pressure across OSV, NVD, EPSS, KEV, or mirrors. |
| `eshu_dp_vulnerability_intelligence_fetch_duration_seconds` | `source`, `result` | Bounded source fetch duration. |

Source URLs, CVE IDs, package names, versions, API keys, and cache paths stay
out of metric labels. Durable checkpoint and retry details are exposed through
status/API fields rather than high-cardinality metrics. Successful checkpoint
writes also add a `vulnerability_intelligence.source_state_checkpoint` event to
the source observe span with bounded source, ecosystem, freshness, terminal, and
failure-class attributes.

## Security Alert Collector

| Metric | Key labels | Use |
| --- | --- | --- |
| `eshu_dp_security_alert_provider_requests_total` | `provider`, `status_class` | Hosted provider alert request attempts, including retryable and terminal failures. |
| `eshu_dp_security_alert_facts_emitted_total` | `provider`, `fact_kind` | Repository-alert source facts emitted per claimed target. |
| `eshu_dp_security_alert_rate_limited_total` | `provider` | GitHub rate-limit pressure surfaced to workflow retry handling. |
| `eshu_dp_security_alert_fetch_duration_seconds` | `provider`, `status_class` | Bounded provider fetch duration for one claimed target. |

Repository names, package names, alert URLs, token environment names, token
values, and provider response bodies stay out of metric labels. Use
`/admin/status`, workflow failures, and traces to connect a bounded failure
class to a specific private target in the operator environment.
When a bounded GitHub open-alert read reaches `max_pages`, API and MCP
reconciliation count/list responses expose `coverage.state=target_incomplete`
and `source_freshness=partial`; this is response metadata, not a metric label.

## CI/CD Run Collector

| Metric | Key labels | Use |
| --- | --- | --- |
| `eshu_dp_ci_cd_run_provider_requests_total` | `provider`, `status_class` | Hosted CI/CD run provider request attempts, including success, rate-limited, and error outcomes. |
| `eshu_dp_ci_cd_run_facts_emitted_total` | `provider`, `fact_kind` | `ci.*` source facts emitted per claimed target. |
| `eshu_dp_ci_cd_run_rate_limited_total` | `provider` | GitHub Actions rate-limit pressure surfaced to workflow retry handling. |
| `eshu_dp_ci_cd_run_partial_generations_total` | `provider`, `reason` | Bounded partial evidence such as truncated job pages or provider warnings. |
| `eshu_dp_ci_cd_run_fetch_duration_seconds` | `provider`, `status_class` | Bounded provider fetch duration for one claimed target. |

Repository names, workflow run IDs, artifact names, URLs, token environment
names, token values, and provider response bodies stay out of metric labels.
Use `/admin/status`, workflow failures, and traces to connect a bounded failure
class to a specific private target in the operator environment.

## PagerDuty Collector

| Metric | Key labels | Use |
| --- | --- | --- |
| `eshu_dp_pagerduty_provider_requests_total` | `provider`, `status_class` | PagerDuty incident evidence request attempts, including partial coverage, retryable failures, and terminal failures. |
| `eshu_dp_pagerduty_facts_emitted_total` | `provider`, `fact_kind` | Incident, lifecycle-event, and change-event source facts emitted per claimed target. |
| `eshu_dp_pagerduty_rate_limited_total` | `provider` | PagerDuty rate-limit pressure surfaced to workflow retry handling. |
| `eshu_dp_pagerduty_config_resources_observed_total` | `provider`, `resource_type` | Optional live PagerDuty service and integration resources observed for no-IaC fallback and freshness validation. |
| `eshu_dp_pagerduty_config_drift_candidates_total` | `provider`, `reason` | Bounded live-config drift candidates, such as manually-created resources, before reducer-owned comparison. |
| `eshu_dp_pagerduty_config_partial_failures_total` | `provider`, `reason` | Partial optional live-config reads, including permission-hidden, missing, unsupported, or rate-limited resource families. |
| `eshu_dp_pagerduty_config_redactions_total` | `provider`, `reason` | Sensitive live-config values redacted or fingerprinted before fact emission. |
| `eshu_dp_pagerduty_fetch_duration_seconds` | `provider`, `status_class` | Bounded PagerDuty fetch duration for one claimed target, including `partial` when optional evidence is hidden. |
| `eshu_dp_pagerduty_generation_lag_seconds` | `provider` | Difference between collector clock and provider observation time. |

Incident IDs, incident titles, service names, escalation-policy names, PagerDuty
URLs, integration names, routing keys, warning resource IDs, token environment
names, token values, and provider response bodies stay out of metric labels.
Use `/admin/status`, workflow failures, and traces to connect a bounded failure
class to a specific private target in the operator environment.
Permission-hidden optional related change events complete as partial evidence:
the readable incident and lifecycle facts remain, an
`incident_routing.coverage_warning` fact records the missing enrichment, and
the provider request metric uses `status_class=partial`.

## Jira Collector

| Metric | Key labels | Use |
| --- | --- | --- |
| `eshu_dp_jira_provider_requests_total` | `provider`, `status_class` | Jira work-item evidence request attempts, including retryable and terminal failures. |
| `eshu_dp_jira_facts_emitted_total` | `provider`, `fact_kind` | Work-item source facts emitted per claimed target. |
| `eshu_dp_jira_rate_limited_total` | `provider` | Jira rate-limit pressure surfaced to workflow retry handling. |
| `eshu_dp_jira_fetch_duration_seconds` | `provider`, `status_class` | Bounded Jira fetch duration for one claimed target. |

Jira fetch spans carry bounded page and output counters for search pages,
changelog pages, remote-link pages, metadata pages, issues emitted, changelog
events emitted, remote links emitted, remote links rejected, unsupported
provider links, metadata objects scanned/emitted, unsupported metadata,
permission-hidden metadata, stale metadata, metadata redactions, partial
failures, rate limits, Retry-After seconds, and stale collection windows. Site
IDs, issue keys, summaries, metadata names, custom-field IDs, user identifiers,
raw remote-link URLs, token environment names, token values, and provider
response bodies stay out of metric labels. Use `/admin/status`, workflow
failures, and traces to connect a bounded failure class to a specific private
target in the operator environment.

## Grafana Collector

| Metric | Key labels | Use |
| --- | --- | --- |
| `eshu_dp_grafana_provider_requests_total` | `provider`, `status_class` | Live Grafana metadata request attempts, including retryable and terminal failures. |
| `eshu_dp_grafana_facts_emitted_total` | `provider`, `fact_kind` | Observability source facts emitted per claimed target. |
| `eshu_dp_grafana_rate_limited_total` | `provider` | Grafana rate-limit pressure surfaced as partial observed coverage. |
| `eshu_dp_grafana_retries_total` | `provider` | Bounded provider retry attempts before the source returns or fails. |
| `eshu_dp_grafana_redactions_total` | `provider`, `reason` | Dashboard URL, datasource URL, query model, contact, and notification metadata dropped or fingerprinted before fact emission. |
| `eshu_dp_grafana_fetch_duration_seconds` | `provider`, `status_class` | Bounded Grafana fetch duration for one claimed target. |

Grafana fetch spans are `grafana.observe` and `grafana.fetch`. Instance IDs,
folder names, dashboard titles, datasource names, URLs, alert query models,
contact points, notification destinations, token environment names, token
values, and provider response bodies stay out of metric labels.

## Prometheus/Mimir Collector

| Metric | Key labels | Use |
| --- | --- | --- |
| `eshu_dp_prometheus_mimir_provider_requests_total` | `provider`, `status_class` | Live Prometheus/Mimir metadata request attempts, including retryable and terminal failures. |
| `eshu_dp_prometheus_mimir_facts_emitted_total` | `provider`, `fact_kind` | Observability source facts emitted per claimed target. |
| `eshu_dp_prometheus_mimir_rate_limited_total` | `provider` | Prometheus/Mimir rate-limit pressure surfaced as partial observed coverage or retry pressure. |
| `eshu_dp_prometheus_mimir_retries_total` | `provider` | Bounded provider retry attempts before the source returns or fails. |
| `eshu_dp_prometheus_mimir_redactions_total` | `provider`, `reason` | Target URL, label value, PromQL, annotation, and tenant metadata dropped or fingerprinted before fact emission. |
| `eshu_dp_prometheus_mimir_stale_total` | `provider` | Targets or rules older than the configured freshness window. |
| `eshu_dp_prometheus_mimir_fetch_duration_seconds` | `provider`, `status_class` | Bounded Prometheus/Mimir fetch duration for one claimed target. |

Prometheus/Mimir fetch spans are `prometheus_mimir.observe` and
`prometheus_mimir.fetch`. Instance IDs, scrape target URLs, label values, raw
PromQL, annotations, tenant IDs, tenant headers, token environment names, token
values, and provider response bodies stay out of metric labels.

## Loki Collector

| Metric | Key labels | Use |
| --- | --- | --- |
| `eshu_dp_loki_provider_requests_total` | `provider`, `status_class` | Live Loki metadata request attempts, including retryable and terminal failures. |
| `eshu_dp_loki_facts_emitted_total` | `provider`, `fact_kind` | Observability source facts emitted per claimed target. |
| `eshu_dp_loki_rate_limited_total` | `provider` | Loki rate-limit pressure surfaced as partial observed coverage or retry pressure. |
| `eshu_dp_loki_retries_total` | `provider` | Bounded provider retry attempts before the source returns or fails. |
| `eshu_dp_loki_redactions_total` | `provider`, `reason` | Label value, LogQL, tenant, URL, and provider metadata dropped or fingerprinted before fact emission. |
| `eshu_dp_loki_high_cardinality_rejected_total` | `provider`, `reason` | Allowlisted label values rejected because they exceeded the configured cardinality bound. |
| `eshu_dp_loki_stale_total` | `provider` | Signals or rules older than the configured freshness window. |
| `eshu_dp_loki_fetch_duration_seconds` | `provider`, `status_class` | Bounded Loki fetch duration for one claimed target. |

Loki fetch spans are `loki.observe` and `loki.fetch`. Instance IDs, label
values, private URLs, raw LogQL, tenant IDs, tenant headers, token environment
names, token values, and provider response bodies stay out of metric labels.

## Tempo Collector

| Metric | Key labels | Use |
| --- | --- | --- |
| `eshu_dp_tempo_provider_requests_total` | `provider`, `status_class` | Tempo metadata request attempts, including retryable, rate-limited, and terminal failures. |
| `eshu_dp_tempo_facts_emitted_total` | `provider`, `fact_kind` | Observability source facts emitted per claimed Tempo target. |
| `eshu_dp_tempo_rate_limited_total` | `provider` | Tempo HTTP 429 pressure surfaced to workflow retry handling. |
| `eshu_dp_tempo_retries_total` | `provider` | Bounded retry attempts for retryable Tempo metadata requests. |
| `eshu_dp_tempo_redactions_total` | `provider` | Tag-value and tenant metadata redactions applied before fact emission. |
| `eshu_dp_tempo_high_cardinality_rejected_total` | `provider` | Allowlisted tag-value reads rejected because they exceeded the configured cardinality limit. |
| `eshu_dp_tempo_stale_total` | `provider` | Tempo metadata observations classified stale. |
| `eshu_dp_tempo_fetch_duration_seconds` | `provider`, `status_class` | Bounded Tempo metadata fetch duration for one claimed target. |

Tempo fetch spans are `tempo.observe` and `tempo.fetch`. They cover metadata
reads from `/api/echo`, `/api/v2/search/tags`, and configured
`/api/v2/search/tag/<tag>/values` endpoints. Tenant IDs, base URLs, raw tag
values, trace IDs, spans, request attributes, TraceQL bodies, token environment
names, token values, and provider response bodies stay out of metric labels.
Use workflow failures and traces to connect a bounded failure class to a
specific private target in the operator environment.

## Scanner-Worker Boundary

Scanner-worker metrics are emitted by the hosted scanner-worker runtime for
isolated security analyzers. The fallback warning analyzer and the concrete
`os_package_extraction` rootfs analyzer both record these signals so operators
can prove claim, retry, dead-letter, resource, and fact-output behavior before
a concrete heavy analyzer is enabled by default.

| Metric | Key labels | Use |
| --- | --- | --- |
| `eshu_dp_scanner_worker_claims_total` | `analyzer`, `target_kind`, `outcome` | Scanner-worker claims started, completed, failed, or skipped. |
| `eshu_dp_scanner_worker_retries_total` | `analyzer`, `target_kind`, `failure_class` | Retryable scanner-worker failures. |
| `eshu_dp_scanner_worker_dead_letters_total` | `analyzer`, `target_kind`, `failure_class` | Terminal scanner-worker failures. |
| `eshu_dp_scanner_worker_facts_emitted_total` | `analyzer`, `target_kind`, `fact_kind` | Source facts emitted by scanner workers. |
| `eshu_dp_scanner_worker_queue_wait_seconds` | `analyzer`, `target_kind` | Work-item age when claim processing starts. |
| `eshu_dp_scanner_worker_scan_duration_seconds` | `analyzer`, `target_kind`, `result` | Analyzer execution duration. |
| `eshu_dp_scanner_worker_target_count` | `analyzer`, `target_kind` | Bounded targets processed by one claim. |
| `eshu_dp_scanner_worker_result_count` | `analyzer`, `target_kind` | Source results emitted by one claim. |
| `eshu_dp_scanner_worker_cpu_seconds` | `analyzer`, `target_kind`, `result` | CPU seconds consumed by analyzer execution. |
| `eshu_dp_scanner_worker_memory_bytes` | `analyzer`, `target_kind`, `result` | Peak scanner-worker memory use. |

Analyzer, target, limit, failure, fact-kind, outcome, and result labels must
come from bounded enums. Raw repository paths, image names, registry URLs,
package coordinates, bucket keys, and source locators stay out of labels.

The bounded `image_unpacking` analyzer
(`internal/collector/scannerworker/imageanalyzer`) reuses these signals without
introducing new metric instruments, spans, log keys, or queues. The hosted
scanner worker wires configured `image_targets` whose local `rootfs_path` or
ordered `layer_paths` are read under scanner-worker resource limits. Emitted
`vulnerability.os_package`, `vulnerability.warning`, and
`scanner_worker.warning` source facts appear on
`eshu_dp_scanner_worker_facts_emitted_total` with
`analyzer="image_unpacking"` and bounded `fact_kind` labels. Unsupported image
shapes surface as warning facts with an `extraction_reason`; local source
unavailability and resource-limit breaches surface through the existing
retry/dead-letter metric vocabulary without raw paths, image names, registry
URLs, package names, or layer locators in labels.

The bounded `sbom_generation` analyzer
(`internal/collector/scannerworker/sbomgenerator`) reuses these signals
without introducing new metric instruments, spans, log keys, or queues. The
hosted scanner worker wires a configured repository-manifest source for
`package-lock.json`, `npm-shrinkwrap.json`, and `go.mod`; emitted
`sbom.document`, `sbom.component`, and `sbom.warning` source facts appear on
`eshu_dp_scanner_worker_facts_emitted_total` with
`analyzer="sbom_generation"` and `fact_kind` set to the SBOM/attestation schema
kind. The bounded `target_kind` label is `repository`, `image`, or `artifact`
according to the claimed scanner-worker target. Resource-limit breaches surface on
`eshu_dp_scanner_worker_dead_letters_total` with
`failure_class` in `{file_limit_exceeded, input_limit_exceeded,
fact_limit_exceeded, unsupported_target, analyzer_failed}`; retryable source
unavailability surfaces on `eshu_dp_scanner_worker_retries_total` with
`failure_class="source_unavailable"`. CPU, memory, and timeout enforcement
remain runtime concerns observable through
`eshu_dp_scanner_worker_cpu_seconds`, `eshu_dp_scanner_worker_memory_bytes`,
and the host pprof endpoint on `eshu-scanner-worker`.

## AWS Cloud Collector

| Metric | Key labels | Use |
| --- | --- | --- |
| `eshu_dp_aws_api_calls_total` | `service`, `account`, `region`, `operation`, `result` | AWS API call volume and result class. |
| `eshu_dp_aws_throttle_total` | `service`, `account`, `region` | Throttle-shaped AWS errors. |
| `eshu_dp_aws_assumerole_failed_total` | `account` | Claim credential acquisition failures. |
| `eshu_dp_aws_budget_exhausted_total` | `service`, `account`, `region` | Scans yielded after exhausting API budget. |
| `eshu_dp_aws_pagination_checkpoint_events_total` | `service`, `account`, `region`, `operation`, `event_kind`, `result` | Durable pagination checkpoint behavior. |
| `eshu_dp_aws_resources_emitted_total` | `service`, `account`, `region`, `resource_type` | AWS resource fact volume. |
| `eshu_dp_aws_relationships_emitted_total` | `service`, `account`, `region` | AWS relationship fact volume. |
| `eshu_dp_aws_tag_observations_emitted_total` | `service`, `account`, `region` | Tag observation fact volume. |
| `eshu_dp_aws_freshness_events_total` | `kind`, `action` | AWS Config/EventBridge freshness intake and handoff. |
| `eshu_dp_aws_org_access_skipped_total` | `service`, `account`, `region`, `reason` | Organizations scans skipped because credentials were not org-aware. |
| `eshu_dp_aws_scan_duration_seconds` | `service`, `account`, `region`, `result` | One service claim before durable commit. |
| `eshu_dp_aws_claim_concurrency` | `account` | Active AWS claims by account. |

ARNs, resource names, tags, policy JSON, image digests, lifecycle policies,
Security Hub finding IDs, Security Hub insight filters, Access Analyzer finding
bodies, GuardDuty finding types, and GuardDuty list locations stay out of
metric labels. Access Analyzer finding aggregate buckets are counted through
`eshu_dp_aws_resources_emitted_total{service="accessanalyzer"}` with bounded
`resource_type=aws_accessanalyzer_finding_count`.

## GCP Cloud Collector

| Metric | Key labels | Use |
| --- | --- | --- |
| `eshu_dp_gcp_freshness_events_total` | `kind`, `action` | Cloud Asset Inventory Pub/Sub push freshness intake and handoff. |

`kind` is one of `asset_change`, `asset_deleted`, or `unknown`. Raw CAI asset
names, parent scope ids, and push payload bodies stay out of metric labels;
see the normalization contract in
`go/internal/collector/gcpcloud/freshness/README.md`. GCP resource,
relationship, and materialization metrics are documented in
[Reducer And Storage Metrics](metrics-reducer-storage.md).

## Confluence Collector

| Metric | Key labels | Use |
| --- | --- | --- |
| `eshu_dp_confluence_http_requests_total` | `operation`, `result`, `status_class` | Confluence source request volume. |
| `eshu_dp_confluence_fetch_duration_seconds` | `operation`, `result` | Page listing, traversal, and body fetch cost. |
| `eshu_dp_confluence_permission_denied_pages_total` | `operation` | Pages skipped because credentials cannot view them. |
| `eshu_dp_confluence_documents_observed_total` | `result` | Document volume after normalization. |
| `eshu_dp_confluence_sections_emitted_total` | `result` | Section fact volume. |
| `eshu_dp_confluence_links_emitted_total` | `result` | Link fact volume. |
| `eshu_dp_confluence_sync_failures_total` | `failure_class` | Configuration, source-read, and fact-build failures. |

Page IDs, titles, URLs, excerpts, paths, and body content stay out of metric
labels.

## Workflow Coordinator

| Metric | Key labels | Use |
| --- | --- | --- |
| `eshu_dp_workflow_coordinator_reconcile_total` | `outcome` | Collector-instance reconcile loop executions. |
| `eshu_dp_workflow_coordinator_reconcile_duration_seconds` | `outcome` | Reconcile loop duration. |
| `eshu_dp_workflow_coordinator_reap_total` | `outcome` | Expired-claim reap passes. |
| `eshu_dp_workflow_coordinator_reap_duration_seconds` | `outcome` | Expired-claim reap duration. |
| `eshu_dp_workflow_coordinator_run_reconcile_total` | `outcome` | Workflow-run reconciliation passes. |
| `eshu_dp_workflow_coordinator_run_reconcile_duration_seconds` | `outcome` | Workflow-run reconciliation duration. |
| `eshu_dp_workflow_coordinator_desired_collector_instances` | none | Desired collector-instance count. |
| `eshu_dp_workflow_coordinator_durable_collector_instances` | none | Durable collector-instance count. |
| `eshu_dp_workflow_coordinator_collector_instance_drift` | none | Absolute drift between desired and durable collector instances. |
| `eshu_dp_workflow_coordinator_last_reaped_claims` | none | Claims reaped by the last successful reap pass. |
| `eshu_dp_workflow_coordinator_last_reconciled_runs` | none | Runs reconciled by the last successful run reconciliation pass. |
| `eshu_dp_workflow_coordinator_semantic_provider_claim_total` | `outcome`, `provider_kind`, `provider_profile_class`, `source_class` | Semantic-provider worker claim outcomes by egress decision and terminal disposition (`egress_denied`, `egress_policy_missing`, `provider_disabled`, `dispatched`, `provider_unavailable`). Labels are redacted and low-cardinality; no provider host, endpoint, URL, or credential is emitted. |

## Webhook Listener

| Metric | Key labels | Use |
| --- | --- | --- |
| `eshu_dp_webhook_requests_total` | `provider`, `outcome`, `reason` | Public webhook requests, including rejected requests. |
| `eshu_dp_webhook_trigger_decisions_total` | `provider`, `event_kind`, `decision`, `reason`, `status` | Normalized provider events that reached durable trigger storage. |
| `eshu_dp_webhook_store_operations_total` | `provider`, `outcome`, `status` | Trigger-store upsert attempts. |
| `eshu_dp_webhook_request_duration_seconds` | `provider`, `outcome`, `reason` | End-to-end provider route duration. |
| `eshu_dp_webhook_store_duration_seconds` | `provider`, `outcome`, `status` | Durable trigger-store duration. |

Repository names, delivery IDs, branch names, and commit SHAs do not belong in
labels.
