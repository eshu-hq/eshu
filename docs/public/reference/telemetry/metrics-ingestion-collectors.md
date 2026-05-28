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
| `eshu_dp_repo_snapshot_duration_seconds` | Per-repository snapshot cost. |
| `eshu_dp_file_parse_duration_seconds` | Per-file parse cost. |
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

Content-aware skip reasons use the `content:` prefix. Repo-local
`.eshu/discovery.json` rules use the `user:` prefix. `.eshuignore` matches use
`skip_reason=eshuignore`.

## Terraform-State Collector

| Metric | Key labels | Use |
| --- | --- | --- |
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

The bounded `sbom_generation` analyzer
(`internal/collector/scannerworker/sbomgenerator`) reuses these signals
without introducing new metric instruments, spans, log keys, or queues. Its
emitted `sbom.document`, `sbom.component`, and `sbom.warning` source facts
appear on `eshu_dp_scanner_worker_facts_emitted_total` with
`analyzer="sbom_generation"` and `fact_kind` set to the SBOM/attestation
schema kind. The bounded `target_kind` label is `repository`, `image`, or
`artifact` according to the claimed scanner-worker target. Resource-limit
breaches surface on
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
