# Telemetry Metrics

This page documents the current Go metric surface that operators can rely on
today. It intentionally focuses on the metrics that exist in code and that are
useful for runtime health, backlog management, throughput tracking, and storage
pressure.

## Reading The Metrics

- Runtime metrics come from the shared `/metrics` surface mounted by each
  long-running runtime.
- Data-plane metrics come from the Go telemetry instruments used by collector,
  projector, reducer, shared follow-up, and storage paths.
- High-cardinality identifiers such as `repo_id`, `run_id`, `scope_id`, and
  `work_item_id` do not belong in metric labels. For Terraform-state sources,
  raw bucket names, object keys, local paths, and full locators should not go in
  traces or logs either; use bounded correlation fields and locator hashes.

## Runtime Health And Backlog

These metrics come from the shared runtime status reader and exist on the API,
MCP server, ingester, and reducer metrics endpoints.

### `eshu_runtime_info`

- Type: Gauge
- Meaning: Presence metric for one runtime endpoint, labeled by
  `service_name` and `service_namespace`.
- Use it for: Dashboard anchoring and scrape-target sanity checks.

### `eshu_runtime_health_state`

- Type: Gauge
- Meaning: One-hot health verdict by `service_name` and `state`
  (`healthy`, `progressing`, `degraded`, `stalled`).
- Use it for: Alerting and quick runtime triage.

### `eshu_runtime_scope_active`
### `eshu_runtime_scope_changed`
### `eshu_runtime_scope_unchanged`

- Type: Gauge
- Meaning: Scope activity and incremental-refresh mix for a runtime.
- Use them for: Distinguishing real scope churn from no-op refresh cycles.

### `eshu_runtime_refresh_skipped_total`

- Type: Counter
- Meaning: Refreshes skipped because the runtime observed no meaningful change.
- Use it for: Measuring how much work incremental refresh is avoiding.

### `eshu_runtime_retry_policy_max_attempts`
### `eshu_runtime_retry_policy_retry_delay_seconds`

- Type: Gauge
- Meaning: Effective retry policy exposed by runtime and stage.
- Use them for: Debugging whether retries are a behavior issue or simply a
  configuration choice.

### `eshu_runtime_queue_total`
### `eshu_runtime_queue_outstanding`
### `eshu_runtime_queue_pending`
### `eshu_runtime_queue_in_flight`
### `eshu_runtime_queue_retrying`
### `eshu_runtime_queue_succeeded`
### `eshu_runtime_queue_dead_letter`
### `eshu_runtime_queue_failed`
### `eshu_runtime_queue_overdue_claims`
### `eshu_runtime_queue_oldest_outstanding_age_seconds`

- Type: Gauge family
- Meaning: Shared runtime queue depth and age surface.
- Use them for: The first backlog dashboard you open when a runtime feels slow,
  stuck, or retry-heavy.

### `eshu_runtime_stage_items`

- Type: Gauge
- Meaning: Work-item counts by `service_name`, `stage`, and `status`.
- Use it for: Pinpointing whether pressure is concentrated in collector,
  projector, reducer, or replay behavior.

### `eshu_runtime_generation_total`

- Type: Gauge
- Meaning: Generation lifecycle totals by state.
- Use it for: Understanding whether changed scopes are draining into active,
  superseded, or failed generations as expected.

### `eshu_runtime_domain_outstanding`
### `eshu_runtime_domain_retrying`
### `eshu_runtime_domain_dead_letter`
### `eshu_runtime_domain_failed`
### `eshu_runtime_domain_oldest_age_seconds`

- Type: Gauge family
- Meaning: Domain-specific backlog and age.
- Use them for: Answering which platform slice is actually stuck instead of
  treating the whole reducer or runtime as one opaque backlog.

## Data-Plane Throughput And Queueing

These metrics are emitted by the Go telemetry instruments used by the
collector/projector/reducer path.

### `eshu_dp_queue_depth`
### `eshu_dp_queue_oldest_age_seconds`
### `eshu_dp_queue_claim_duration_seconds`

- Type: Gauge, gauge, histogram
- Meaning: Queue depth, oldest pending age, and claim latency for the
  facts-first work queue.
- Use them for: Autoscaling and backlog diagnosis across the data plane.

### `eshu_dp_worker_pool_active`

- Type: Gauge
- Meaning: Active worker count for instrumented worker pools.
- Use it for: Confirming whether configured parallelism is actually in use.

### `eshu_dp_collector_observe_duration_seconds`
### `eshu_dp_repo_snapshot_duration_seconds`
### `eshu_dp_repos_snapshotted_total`
### `eshu_dp_file_parse_duration_seconds`
### `eshu_dp_files_parsed_total`

- Type: Histograms and counters
- Meaning: Repository discovery, snapshot, and parse throughput.
- Use them for: Measuring collector cost and spotting oversized repos or slow
  parse phases.

### `eshu_dp_fact_emit_duration_seconds`
### `eshu_dp_facts_emitted_total`
### `eshu_dp_facts_committed_total`
### `eshu_dp_fact_batches_committed_total`
### `eshu_dp_generation_fact_count`

- Type: Histograms and counters
- Meaning: Fact emission and durable commit volume.
- Use them for: Understanding whether slowdowns happen before queueing or after
  facts are already materialized.

### `eshu_dp_tfstate_discovery_candidates_total`

- Type: Counter
- Labels: `source` (`seed`, `graph`, or `git_local_file`)
- Meaning: Terraform-state discovery candidates accepted by the resolver before
  any state source is opened.
- Use it for: Confirming whether tfstate collection is bootstrapping from
  operator seeds or from Git-observed backend evidence.

### `eshu_dp_oci_registry_api_calls_total`

- Type: Counter
- Labels: `provider`, `operation`, `result`
- Meaning: OCI registry API calls split by provider and operation. Operations
  include `ping`, `list_tags`, `get_manifest`, and `list_referrers`.
- Use it for: Separating auth or capability failures from manifest-specific
  failures without putting registry hostnames, repositories, tags, or digests
  into labels.

### `eshu_dp_oci_registry_tags_observed_total`

- Type: Counter
- Labels: `provider`, `result`
- Meaning: Tags accepted into the bounded scan after tag listing and `tag_limit`
  filtering.
- Use it for: Confirming whether the collector is seeing tag volume for a
  provider before manifest fetches begin.

### `eshu_dp_oci_registry_manifests_observed_total`

- Type: Counter
- Labels: `provider`, `media_family`
- Meaning: Digest objects observed from manifest reads. `media_family` is
  `image_manifest`, `image_index`, or `descriptor`.
- Use it for: Understanding whether a registry target is mostly
  single-platform manifests, multi-platform indexes, or unknown descriptor
  evidence.

### `eshu_dp_oci_registry_referrers_observed_total`

- Type: Counter
- Labels: `provider`, `artifact_family`
- Meaning: Referrer descriptors reported for subject digests. `artifact_family`
  is bounded to `sbom`, `signature`, `attestation`, `vulnerability`, `unknown`,
  or `other`.
- Use it for: Seeing whether SBOM, signature, attestation, or scan artifact
  evidence exists without interpreting those artifacts in the registry
  collector.

### `eshu_dp_oci_registry_scan_duration_seconds`

- Type: Histogram
- Labels: `provider`, `result`
- Meaning: One target scan from client creation through tag, manifest,
  referrer, and fact-envelope construction. The later Postgres commit is still
  measured by `eshu_dp_collector_observe_duration_seconds`.
- Use it for: Distinguishing slow registry APIs from slow durable ingestion.

### `eshu_dp_aws_api_calls_total`

- Type: Counter
- Labels: `service`, `account`, `region`, `operation`, `result`
- Meaning: AWS service API calls split by service, target account, region, and
  SDK operation.
- Use it for: Separating IAM pagination failures, throttles, and successful
  scans without putting ARNs, policy JSON, tags, or resource names into labels.

### `eshu_dp_aws_throttle_total`

- Type: Counter
- Labels: `service`, `account`, `region`
- Meaning: AWS API calls that returned throttling-shaped service errors.
- Use it for: Tuning claim fan-out and service scan cadence per AWS account.

### `eshu_dp_aws_assumerole_failed_total`

- Type: Counter
- Labels: `account`
- Meaning: Claim-scoped credential acquisition failures before a service scan
  starts.
- Use it for: Detecting broken trust policy, external ID, or workload identity
  setup.

### `eshu_dp_aws_resources_emitted_total`

- Type: Counter
- Labels: `service`, `account`, `region`, `resource_type`
- Meaning: AWS `aws_resource` facts emitted by service scanner and resource
  type.
- Use it for: Confirming service scanners are producing bounded source facts
  without putting ARNs, tags, image digests, lifecycle policy JSON, or resource
  names into labels.

### `eshu_dp_aws_relationships_emitted_total`

- Type: Counter
- Labels: `service`, `account`, `region`
- Meaning: AWS `aws_relationship` facts emitted by service scanner.
- Use it for: Confirming relationship-producing scanners are emitting source
  relationships before reducer-owned correlation.

### `eshu_dp_aws_tag_observations_emitted_total`

- Type: Counter
- Labels: `service`, `account`, `region`
- Meaning: AWS `aws_tag_observation` facts emitted by service scanner.
- Use it for: Confirming tag evidence is available for reducer-owned tag
  normalization without adding raw tags to metric labels.

### `eshu_dp_aws_scan_duration_seconds`

- Type: Histogram
- Labels: `service`, `account`, `region`, `result`
- Meaning: One claimed AWS service scan before durable commit.
- Use it for: Distinguishing slow AWS service reads from slow Postgres commit
  time.

### `eshu_dp_aws_claim_concurrency`

- Type: Observable gauge
- Labels: `account`
- Meaning: Active AWS claims by account when a multi-worker AWS runner
  registers its account limiter.
- Use it for: Confirming per-account concurrency caps during AWS scans.

### `eshu_dp_tfstate_claim_wait_seconds`

- Type: Histogram
- Labels: `collector_kind`, `source_system`
- Meaning: How old a Terraform-state workflow item is when the collector claims
  it.
- Use it for: Checking whether Terraform-state work is backing up before
  collection begins without creating per-scope metric series.

### `eshu_dp_tfstate_snapshots_observed_total`
### `eshu_dp_tfstate_snapshot_bytes`
### `eshu_dp_tfstate_parse_duration_seconds`
### `eshu_dp_tfstate_resources_emitted_total`
### `eshu_dp_tfstate_redactions_applied_total`
### `eshu_dp_tfstate_s3_conditional_get_not_modified_total`

- Type: Counters and histograms
- Labels: `backend_kind`, `result`, or `reason` depending on the metric.
  These labels are bounded. They do not include state locators, bucket names,
  local paths, work item IDs, or repository names.
- Meaning: Terraform-state source observations, parser cost, emitted resource
  fact volume, redaction/drop volume by policy reason, and S3 conditional-read
  no-op outcomes.
- Use them for: Separating "no new state" from real collector failure, spotting
  large or slow state files, and checking whether a new provider schema gap is
  causing more values to be redacted or dropped.

### `eshu_dp_webhook_requests_total`

- Type: Counter
- Labels: `provider`, `outcome`, `reason`.
  Provider is one of `github`, `gitlab`, `bitbucket`, or `unknown`.
  Outcome is a bounded listener result such as `stored`, `rejected`, or
  `failed`. Reason is a closed listener reason such as `auth_failed`,
  `missing_delivery_id`, `body_too_large`, `malformed_event`, `store_failed`,
  or `none`.
- Meaning: Count of public webhook requests handled by the listener, including
  requests rejected before normalization.
- Use it for: Seeing provider delivery volume, alerting on auth or malformed
  delivery spikes, and checking whether upstream providers are reaching the
  public listener at all.

### `eshu_dp_webhook_trigger_decisions_total`

- Type: Counter
- Labels: `provider`, `event_kind`, `decision`, `reason`, `status`.
  `event_kind` is the normalized provider-neutral event kind, `decision` is
  `accepted` or `ignored`, and `status` is the durable trigger status such as
  `queued` or `ignored`.
- Meaning: Count of normalized provider events that reached durable trigger
  storage. This metric records graph-refresh intent after authentication,
  delivery identity validation, normalization, and idempotent storage have all
  succeeded.
- Use it for: Measuring accepted versus ignored provider events, proving
  default-branch merge and push events are becoming queued work, and separating
  provider noise from real refresh demand.

### `eshu_dp_webhook_store_operations_total`

- Type: Counter
- Labels: `provider`, `outcome`, `status`.
- Meaning: Count of webhook trigger store attempts. Successful attempts include
  the resulting durable status; failed attempts use `status=unknown`.
- Use it for: Distinguishing listener rejection problems from Postgres trigger
  persistence failures.

### `eshu_dp_webhook_request_duration_seconds`

- Type: Histogram
- Labels: `provider`, `outcome`, `reason`.
- Meaning: End-to-end provider route duration, including body read, signature
  or token verification, normalization, trigger persistence, and response
  writing.
- Use it for: Detecting slow public webhook handling and deciding whether the
  latency is broad or concentrated in a rejection/failure reason.

### `eshu_dp_webhook_store_duration_seconds`

- Type: Histogram
- Labels: `provider`, `outcome`, `status`.
- Meaning: Duration of the durable trigger-store operation inside the listener.
  This isolates Postgres upsert latency from provider authentication and
  normalization cost.
- Use it for: Checking whether webhook intake latency is caused by Postgres
  persistence before tuning ingress, body limits, or provider-side retries.

### `eshu_dp_projector_run_duration_seconds`
### `eshu_dp_projector_stage_duration_seconds`
### `eshu_dp_projections_completed_total`

- Type: Histograms and counter
- Meaning: Source-local projector latency and completion volume.
- Use them for: Separating projector latency from reducer latency.

### `eshu_dp_correlation_rule_matches_total`
### `eshu_dp_correlation_drift_detected_total`

- Type: Counters
- Labels:
  `eshu_dp_correlation_rule_matches_total` carries `pack` and `rule`.
  `eshu_dp_correlation_drift_detected_total` carries `pack`, `rule`, and
  `drift_kind`.
  All three label values are bounded: `pack` is a frozen string from the
  rule-pack registry, `rule` is one of the rule names declared by that
  pack, `drift_kind` is the closed enum
  `{added_in_state, added_in_config, attribute_drift, removed_from_state,
  removed_from_config}`. Resource addresses, attribute paths, and module
  paths never appear as label values — they go in structured logs and
  span attributes.
- Meaning: Match-phase activity volume and admitted drift volume from the
  correlation engine. `eshu_dp_correlation_rule_matches_total` advances
  by `Result.MatchCounts[ruleName]` for each `RuleKindMatch` rule per
  admitted candidate, so its `rule` label always names a match-phase
  rule (e.g. `match-config-against-state` for the drift pack).
  `eshu_dp_correlation_drift_detected_total` advances per
  admitted drift candidate, with `drift_kind` set by the classifier in
  `go/internal/correlation/drift/tfconfigstate`.
- Use them for: Detecting Terraform config-vs-state drift volume per
  drift kind, alerting on `attribute_drift` spikes that imply manual
  cloud edits, and confirming that the rule pack is being exercised
  after state-snapshot scope generations roll forward.

### `eshu_dp_correlation_drift_intents_enqueued_total`

- Type: Counter
- Labels: `pack` (frozen string from the rule-pack registry, e.g.
  `terraform_config_state_drift`), `source` (currently always
  `bootstrap_index`; reserved for a future ingester delta-trigger).
- Meaning: Count of `config_state_drift` reducer intents the bootstrap-index
  Phase 3.5 trigger emitted per run. Advances by the number of active
  `state_snapshot:*` scopes at trigger time; advances by 0 when there are
  no active state-snapshot scopes, which is itself the useful signal "the
  trigger ran and produced zero work."
- Use it for: Decoupling drift enqueue health from drift admission health.
  Paired with `eshu_dp_correlation_drift_detected_total`, the two counters
  let operators attribute a drop:
  - Flat enqueue + dropping admission → classifier or loader regression.
  - Dropping enqueue + dropping admission → bootstrap trigger or upstream
    fact-set regression.
  - Dashboard example (sum to a single series per `pack` before subtracting,
    so the admission counter's additional `rule` and `drift_kind` labels
    don't trigger many-to-one vector matching):
    `sum by(pack) (rate(eshu_dp_correlation_drift_intents_enqueued_total[1h])) - sum by(pack) (rate(eshu_dp_correlation_drift_detected_total[1h]))`
    surfaces the gap between trigger emission rate and admission rate per
    pack, aggregated across all admitted drift kinds.

### `eshu_dp_drift_unresolved_module_calls_total`

- Type: Counter
- Labels: `reason` — closed enum
  `{external_registry, external_git, external_archive, cross_repo_local,
  cycle_detected, depth_exceeded}`. No resource addresses, source URLs, or
  file paths appear as label values; high-cardinality detail goes in
  structured logs.
- Meaning: One Terraform `module {}` call the drift loader's module-aware
  join (issue #169) could not resolve to a local-filesystem callee
  directory under the same repo snapshot. State-side resources whose
  canonical address would have been prefixed by the unresolvable call
  surface as `added_in_state` instead of joining cleanly with the
  config-side row. Reasons:
  - `external_registry` — Terraform Registry shorthand
    (`namespace/name/provider`).
  - `external_git` — `git::` scheme, GitHub/GitLab/Bitbucket HTTPS URL,
    `git@` SSH form.
  - `external_archive` — HTTP/HTTPS archive URL, `s3::`, `gcs::`,
    `mercurial::`, or any unparseable source string.
  - `cross_repo_local` — local relative path that escapes the repo
    snapshot root (`../../sibling-repo/...`).
  - `cycle_detected` — module call graph cycle; the resolver breaks at
    the second visit so the loader does not run forever.
  - `depth_exceeded` — module call chain deeper than
    `maxModulePrefixDepth` (10). Hard-coded; the bound exists to make
    cycles cheap to break, not as a real ceiling.
- Use it for: Sizing how much config the v1 module-aware join is missing
  per intent. Pair with
  `eshu_dp_correlation_drift_detected_total{drift_kind="added_in_state"}`
  to distinguish "real operator-imported resource" from "callee module
  out of scope for v1 join." A growing `external_registry` count is the
  signal to vendor-in third-party modules; a `cross_repo_local` count
  shows demand for the deferred cross-repo-modules feature.

### `eshu_dp_documentation_entity_mentions_extracted_total`
### `eshu_dp_documentation_claim_candidates_extracted_total`
### `eshu_dp_documentation_claim_candidates_suppressed_total`
### `eshu_dp_documentation_drift_findings_total`
### `eshu_dp_documentation_drift_generation_duration_seconds`

- Type: Counters and histogram
- Meaning: Documentation truth extraction volume after a documentation section
  has been collected, plus read-only drift finding volume and latency.
- Use them for: Checking whether entity mentions resolve cleanly and whether
  claim candidates are being emitted. Ambiguous and unmatched mention outcomes
  usually point to a catalog or alias problem, not a writer problem. Suppressed
  claim counters show where exact-finding emission was blocked on purpose.
  Drift finding outcomes show whether documentation is matching, conflicting,
  ambiguous, unsupported, stale, or still building.

### `eshu_dp_reducer_intents_enqueued_total`
### `eshu_dp_reducer_executions_total`
### `eshu_dp_reducer_run_duration_seconds`
### `eshu_dp_reducer_queue_wait_seconds`
### `eshu_dp_reducer_batch_claim_size`

- Type: Counters and histograms
- Meaning: Reducer enqueue, execution, queue wait, and claim-size behavior.
- Use them for: Tuning reducer concurrency and validating that shared follow-up
  is not starving the main reducer path. Compare queue wait with reducer run
  duration before changing worker counts.

## Shared Follow-Up And Acceptance

### `eshu_dp_shared_projection_cycles_total`
### `eshu_dp_shared_projection_intent_wait_seconds`
### `eshu_dp_shared_projection_processing_seconds`
### `eshu_dp_shared_projection_stale_intents_total`

- Type: Counters and histograms
- Meaning: Shared-projection loop cycles, selected-intent age, processing time,
  readiness-blocked wait, and stale-intent cleanup activity.
- Notes: `domain=code_calls` also includes the dedicated code-call projection
  runner, whose logs add selected-intent wait, readiness-blocked wait,
  selection duration, lease-claim duration, and processing duration for each
  completed graph-write cycle.
- Use them for: Detecting whether follow-up work is running but constantly
  finding stale or superseded intents, waiting on semantic readiness, or spending
  time inside graph writes after partition selection.

### `eshu_dp_shared_acceptance_lookup_duration_seconds`
### `eshu_dp_shared_acceptance_lookup_errors_total`
### `eshu_dp_shared_acceptance_upsert_duration_seconds`
### `eshu_dp_shared_acceptance_upserts_total`
### `eshu_dp_shared_acceptance_prefetch_size`
### `eshu_dp_shared_acceptance_rows`

- Type: Histograms, counters, gauge, histogram
- Meaning: Lookup and write behavior for shared acceptance state.
- Use them for: Diagnosing slow or error-prone shared follow-up decisions and
  validating batch sizing.

## Storage, Graph, And Cross-Repo Work

### `eshu_dp_postgres_query_duration_seconds`
### `eshu_dp_neo4j_query_duration_seconds`

- Type: Histogram
- Meaning: Storage query latency for Postgres and Neo4j.
- Use them for: Telling whether the bottleneck is the queueing layer or the
  underlying storage round-trips.

### `eshu_dp_neo4j_deadlock_retries_total`

- Type: Counter
- Meaning: Deadlock retries observed on Neo4j write paths.
- Use it for: Detecting contention regressions and verifying deadlock hardening.

### `eshu_dp_neo4j_batch_size`
### `eshu_dp_neo4j_batches_executed_total`

- Type: Histogram and counter
- Meaning: Neo4j batch sizing and batch execution volume. Grouped writes record
  one point per statement inside the transaction, with bounded labels such as
  `operation`, `write_phase`, and `node_type` when the writer provided them.
- Use them for: Tuning write chunking and understanding write amplification.
  For Neo4j parity runs, compare these with
  `eshu_dp_neo4j_query_duration_seconds{operation="write_group"}` to separate
  one slow transaction from a specific canonical phase or semantic label.

### `eshu_dp_canonical_writes_total`
### `eshu_dp_canonical_write_duration_seconds`
### `eshu_dp_canonical_atomic_writes_total`
### `eshu_dp_canonical_atomic_fallbacks_total`
### `eshu_dp_canonical_nodes_written_total`
### `eshu_dp_canonical_edges_written_total`
### `eshu_dp_canonical_projection_duration_seconds`
### `eshu_dp_canonical_retract_duration_seconds`
### `eshu_dp_canonical_batch_size`
### `eshu_dp_canonical_phase_duration_seconds`

- Type: Counters, histograms, and gauges
- Meaning: Canonical graph write throughput, latency, fallback behavior, and
  phase-level cost.
- Use them for: Understanding whether graph cost comes from projection, retract,
  batching, or atomic fallback behavior.

### `eshu_dp_cross_repo_resolution_duration_seconds`
### `eshu_dp_cross_repo_evidence_loaded_total`
### `eshu_dp_cross_repo_edges_resolved_total`
### `eshu_dp_evidence_facts_discovered_total`

- Type: Histogram and counters
- Meaning: Cross-repo resolution and evidence-loading work.
- Use them for: Diagnosing relationship-mapping cost and evidence sparsity.

## Capacity And Pipeline Shape

### `eshu_dp_discovery_dirs_skipped_total`
### `eshu_dp_discovery_files_skipped_total`

- Type: Counter
- Meaning: Filesystem entries pruned during discovery.
- Use them for: Verifying ignore policy behavior and explaining why a repo scan
  is cheaper than a raw file count might suggest.
- Content-aware skip reasons use the `content:` prefix. Generated JavaScript
  bundle filters currently emit `content:generated-webpack`,
  `content:generated-rollup`, `content:generated-esbuild`, and
  `content:generated-parcel`; legacy vendored-library filters emit
  `content:vendored-zend-framework`, `content:vendored-browser-library`, and
  `content:vendored-fpdf`; legacy PEAR libraries, including Phing, emit
  `content:vendored-pear`.
- Repo-local `.eshu/discovery.json` rules use the `user:` prefix. The legacy
  `.eshu/vendor-roots.json` compatibility file emits the same prefix. Directory
  pruning appears on `eshu_dp_discovery_dirs_skipped_total`; file-level user
  skips, when a file glob matches directly, appear on
  `eshu_dp_discovery_files_skipped_total`.
- Repo-local `.eshuignore` file matches emit `skip_reason=eshuignore` on
  `eshu_dp_discovery_files_skipped_total`. Use `.eshu/discovery.json` instead
  when the operator needs a more specific reason such as
  `user:archived-site-copy`.

### `eshu_dp_large_repo_classifications_total`
### `eshu_dp_large_repo_semaphore_wait_seconds`

- Type: Counter and histogram
- Meaning: Large-repo classification and concurrency throttling.
- Use them for: Tuning large-repo fairness and explaining collector wait time.

### `eshu_dp_content_rereads_total`
### `eshu_dp_content_reread_skips_total`

- Type: Counter
- Meaning: Content reread behavior in the projection path.
- Use them for: Understanding how often content had to be reloaded instead of
  reused.

### `eshu_dp_pipeline_overlap_seconds`

- Type: Histogram
- Meaning: Overlap between major pipeline phases.
- Use it for: Seeing whether parallelism is helping or just increasing memory
  overlap and contention.

### `eshu_dp_gomemlimit_bytes`

- Type: Gauge
- Meaning: Effective Go memory limit exposed by the runtime.
- Use it for: Correlating concurrency tuning with container memory pressure.

## Recommended Dashboards

### Runtime Health

- `eshu_runtime_health_state`
- `eshu_runtime_queue_outstanding`
- `eshu_runtime_queue_oldest_outstanding_age_seconds`
- `eshu_runtime_stage_items`
- `eshu_runtime_domain_oldest_age_seconds`

### Ingest Throughput

- `eshu_dp_repos_snapshotted_total`
- `eshu_dp_files_parsed_total`
- `eshu_dp_facts_emitted_total`
- `eshu_dp_collector_observe_duration_seconds`
- `eshu_dp_projector_run_duration_seconds`
- `eshu_dp_reducer_run_duration_seconds`

### Webhook Intake

- `eshu_dp_webhook_requests_total`
- `eshu_dp_webhook_trigger_decisions_total`
- `eshu_dp_webhook_store_operations_total`
- `eshu_dp_webhook_request_duration_seconds`
- `eshu_dp_webhook_store_duration_seconds`

### Shared Follow-Up

- `eshu_dp_shared_projection_cycles_total`
- `eshu_dp_shared_projection_intent_wait_seconds`
- `eshu_dp_shared_projection_processing_seconds`
- `eshu_dp_shared_projection_stale_intents_total`
- `eshu_dp_shared_acceptance_lookup_duration_seconds`
- `eshu_dp_shared_acceptance_lookup_errors_total`
- `eshu_dp_shared_acceptance_upsert_duration_seconds`

### Storage Pressure

- `eshu_dp_postgres_query_duration_seconds`
- `eshu_dp_neo4j_query_duration_seconds`
- `eshu_dp_neo4j_deadlock_retries_total`
- `eshu_dp_canonical_write_duration_seconds`
- `eshu_dp_canonical_atomic_fallbacks_total`

If you need exact repository, scope, generation, or work-item context, move
from metrics into [traces](traces.md) and [logs](logs.md). Metrics should tell
you where to look next, not carry the full debugging payload themselves.
