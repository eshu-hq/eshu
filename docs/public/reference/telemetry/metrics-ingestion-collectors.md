# Ingestion And Collector Metrics

This catalog covers metrics emitted before reducer-owned materialization:
source collection, discovery pruning, fact emission, registry/cloud collectors,
Confluence, and webhook intake.

## Git And Discovery

| Metric | Type | Labels | Use |
| --- | --- | --- | --- |
| `eshu_dp_repos_snapshotted_total` | counter | `status` | Repository snapshot completion volume. |
| `eshu_dp_files_parsed_total` | counter | `status` | File parse volume. |
| `eshu_dp_repo_snapshot_duration_seconds` | histogram | bounded status labels | Per-repository snapshot cost. |
| `eshu_dp_file_parse_duration_seconds` | histogram | bounded status labels | Per-file parse cost. |
| `eshu_dp_discovery_dirs_skipped_total` | counter | `skip_reason` | Directory pruning by discovery policy. |
| `eshu_dp_discovery_files_skipped_total` | counter | `skip_reason` | File pruning by discovery policy. |
| `eshu_dp_large_repo_classifications_total` | counter | bounded tier labels | Large-repo classification volume. |
| `eshu_dp_large_repo_semaphore_wait_seconds` | histogram | bounded tier labels | Wait time for large-repo concurrency control. |

Content-aware skip reasons use the `content:` prefix. Repo-local
`.eshu/discovery.json` rules use the `user:` prefix. `.eshuignore` matches use
`skip_reason=eshuignore`.

## Fact Streaming

| Metric | Type | Use |
| --- | --- | --- |
| `eshu_dp_facts_emitted_total` | counter | Collector fact output volume. |
| `eshu_dp_facts_committed_total` | counter | Durable fact commit volume. |
| `eshu_dp_fact_batches_committed_total` | counter | Streaming multi-row fact batch commits. |
| `eshu_dp_generation_fact_count` | histogram | Fact volume per generation. |
| `eshu_dp_content_rereads_total` | counter | Content reloads in the projection path. |
| `eshu_dp_content_reread_skips_total` | counter | Content reloads avoided by reuse. |

Use fact batch rate with Postgres query latency to distinguish slow producers
from slow persistence.

## Terraform-State Collector

| Metric | Type | Labels | Use |
| --- | --- | --- | --- |
| `eshu_dp_tfstate_claim_wait_seconds` | histogram | `collector_kind`, `source_system` | Work-item age when a state claim starts. |
| `eshu_dp_tfstate_discovery_candidates_total` | counter | `source` | Candidate sources accepted before opening a state file. |
| `eshu_dp_tfstate_snapshots_observed_total` | counter | `backend_kind`, `result` | State source observations. |
| `eshu_dp_tfstate_snapshot_bytes` | histogram | bounded backend/result labels | State source size. |
| `eshu_dp_tfstate_parse_duration_seconds` | histogram | bounded backend/result labels | Streaming parser cost. |
| `eshu_dp_tfstate_resources_emitted_total` | counter | `backend_kind` | Resource fact volume. |
| `eshu_dp_tfstate_outputs_emitted_total` | counter | `safe_locator_hash` | Output fact volume without raw locators. |
| `eshu_dp_tfstate_modules_emitted_total` | counter | `safe_locator_hash` | Module observation fact volume without raw locators. |
| `eshu_dp_tfstate_warnings_emitted_total` | counter | `warning_kind`, `safe_locator_hash` | Parser or policy warning volume. |
| `eshu_dp_tfstate_redactions_applied_total` | counter | `reason` | Redaction or safe-drop decisions. |
| `eshu_dp_tfstate_s3_conditional_get_not_modified_total` | counter | none | S3 conditional reads that avoided work. |

Raw bucket names, object keys, local paths, and full locators must not appear in
metric labels.

## OCI Registry Collector

| Metric | Type | Labels | Use |
| --- | --- | --- | --- |
| `eshu_dp_oci_registry_api_calls_total` | counter | `provider`, `operation`, `result` | Registry API volume and failures. |
| `eshu_dp_oci_registry_tags_observed_total` | counter | `provider`, `result` | Tags accepted into bounded scans. |
| `eshu_dp_oci_registry_manifests_observed_total` | counter | `provider`, `media_family` | Manifest, index, and descriptor observations. |
| `eshu_dp_oci_registry_referrers_observed_total` | counter | `provider`, `artifact_family` | SBOM, signature, attestation, vulnerability, or unknown referrer evidence. |
| `eshu_dp_oci_registry_scan_duration_seconds` | histogram | `provider`, `result` | One repository scan before durable commit. |

Registry hosts, repositories, tags, digests, and credentials stay out of labels.

## Package Registry Collector

| Metric | Type | Labels | Use |
| --- | --- | --- | --- |
| `eshu_dp_package_registry_requests_total` | counter | `ecosystem`, `status_class` | Metadata request attempts. |
| `eshu_dp_package_registry_facts_emitted_total` | counter | `ecosystem`, `fact_kind` | Parser output volume. |
| `eshu_dp_package_registry_rate_limited_total` | counter | `ecosystem` | HTTP 429 pressure. |
| `eshu_dp_package_registry_parse_failures_total` | counter | `ecosystem`, `document_type` | Metadata parse failures. |
| `eshu_dp_package_registry_observe_duration_seconds` | histogram | bounded ecosystem/result labels | Claimed target observation cost. |
| `eshu_dp_package_registry_generation_lag_seconds` | histogram | bounded ecosystem/result labels | Source observation lag. |

Package names, versions, feed URLs, artifact paths, and credential env names
stay in logs or traces, not metric labels.

## AWS Cloud Collector

| Metric | Type | Labels | Use |
| --- | --- | --- | --- |
| `eshu_dp_aws_api_calls_total` | counter | `service`, `account`, `region`, `operation`, `result` | AWS API call volume and result class. |
| `eshu_dp_aws_throttle_total` | counter | `service`, `account`, `region` | Throttle-shaped AWS errors. |
| `eshu_dp_aws_assumerole_failed_total` | counter | `account` | Claim credential acquisition failures. |
| `eshu_dp_aws_budget_exhausted_total` | counter | `service`, `account`, `region` | Scans yielded after exhausting API budget. |
| `eshu_dp_aws_pagination_checkpoint_events_total` | counter | `service`, `account`, `region`, `operation`, `event_kind`, `result` | Durable pagination checkpoint behavior. |
| `eshu_dp_aws_resources_emitted_total` | counter | `service`, `account`, `region`, `resource_type` | AWS resource fact volume. |
| `eshu_dp_aws_relationships_emitted_total` | counter | `service`, `account`, `region` | AWS relationship fact volume. |
| `eshu_dp_aws_tag_observations_emitted_total` | counter | `service`, `account`, `region` | Tag observation fact volume. |
| `eshu_dp_aws_freshness_events_total` | counter | `kind`, `action` | AWS Config/EventBridge freshness intake and handoff. |
| `eshu_dp_aws_scan_duration_seconds` | histogram | `service`, `account`, `region`, `result` | One service claim before durable commit. |
| `eshu_dp_aws_claim_concurrency` | observable gauge | `account` | Active AWS claims by account. |

ARNs, resource names, tags, policy JSON, image digests, and lifecycle policies
stay out of metric labels.

## Confluence Collector

| Metric | Type | Labels | Use |
| --- | --- | --- | --- |
| `eshu_dp_confluence_http_requests_total` | counter | `operation`, `result`, `status_class` | Confluence source request volume. |
| `eshu_dp_confluence_fetch_duration_seconds` | histogram | `operation`, `result` | Page listing, traversal, and body fetch cost. |
| `eshu_dp_confluence_permission_denied_pages_total` | counter | `operation` | Pages skipped because the credential cannot view them. |
| `eshu_dp_confluence_documents_observed_total` | counter | `result` | Document volume after normalization. |
| `eshu_dp_confluence_sections_emitted_total` | counter | `result` | Section fact volume. |
| `eshu_dp_confluence_links_emitted_total` | counter | `result` | Link fact volume. |
| `eshu_dp_confluence_sync_failures_total` | counter | `failure_class` | Configuration, source-read, and fact-build failures. |

Page IDs, titles, URLs, excerpts, paths, and body content stay out of metric
labels.

## Webhook Listener

| Metric | Type | Labels | Use |
| --- | --- | --- | --- |
| `eshu_dp_webhook_requests_total` | counter | `provider`, `outcome`, `reason` | Public webhook requests, including rejected requests. |
| `eshu_dp_webhook_trigger_decisions_total` | counter | `provider`, `event_kind`, `decision`, `reason`, `status` | Normalized provider events that reached durable trigger storage. |
| `eshu_dp_webhook_store_operations_total` | counter | `provider`, `outcome`, `status` | Trigger-store upsert attempts. |
| `eshu_dp_webhook_request_duration_seconds` | histogram | `provider`, `outcome`, `reason` | End-to-end provider route duration. |
| `eshu_dp_webhook_store_duration_seconds` | histogram | `provider`, `outcome`, `status` | Durable trigger-store duration. |

Repository names, delivery IDs, branch names, and commit SHAs do not belong in
labels.
