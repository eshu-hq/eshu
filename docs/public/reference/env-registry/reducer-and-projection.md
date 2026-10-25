# Environment Variables: Reducer and projection

<!-- Generated from go/internal/envregistry. Do not edit by hand; regenerate with `bash scripts/generate-env-registry-doc.sh`. -->

The reducer, projector, workflow coordinator, push hooks, and semantic providers. This page is part of the [Environment Variable Reference](../env-registry.md), which lists every page. Run `eshu config validate` to check the current environment against the registry.

## coordinator

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_COLLECTOR_INSTANCES_JSON` | string | — | JSON array of desired collector instances reconciled by the coordinator. |
| `ESHU_COMPONENT_QUOTA_MAX_FACTS_PER_GENERATION` | int | — | Uniform operator override for per-component facts-per-generation quota (#8561): declared and validated today, not runtime-enforced; blank keeps the manifests. |
| `ESHU_COMPONENT_QUOTA_MAX_GENERATIONS_PER_HOUR` | int | — | Uniform operator override for per-component generations-per-hour claim quota (#8561): wins over every admitted-manifest declaration of a component planning at least one claims-enabled instance (#8582); blank keeps the manifests. |
| `ESHU_HOSTED_COLLECTOR_EGRESS_POLICY_JSON` | string | — | JSON egress policy applied to hosted collectors. |
| `ESHU_HOSTED_EXTENSION_EGRESS_POLICY_JSON` | string | — | JSON egress policy applied to hosted extensions. |
| `ESHU_WORKFLOW_COORDINATOR_AWS_FRESHNESS_CLAIM_LEASE_DURATION` | duration | `5m` | Claim lease duration for AWS freshness trigger handoff (#4576). |
| `ESHU_WORKFLOW_COORDINATOR_CLAIMS_ENABLED` | bool | `false` | Enable claim-based workflow coordination. Aliases: `ESHU_WORKFLOW_COORDINATOR_ENABLE_CLAIMS`. |
| `ESHU_WORKFLOW_COORDINATOR_CLAIM_LEASE_TTL` | duration | — | TTL for workflow claim leases. |
| `ESHU_WORKFLOW_COORDINATOR_DEPLOYMENT_MODE` | enum | `dark` | Workflow coordinator deployment mode. Allowed: `dark`, `active`. |
| `ESHU_WORKFLOW_COORDINATOR_EXPIRED_CLAIM_LIMIT` | int | — | Reap batch limit for expired claims per pass. |
| `ESHU_WORKFLOW_COORDINATOR_EXPIRED_CLAIM_REQUEUE_DELAY` | duration | — | Delay before requeuing expired claims. |
| `ESHU_WORKFLOW_COORDINATOR_FRESHNESS_CLAIM_REAP_LIMIT` | int | `100` | Reap batch limit for expired AWS/GCP freshness trigger claims per pass (#4576). |
| `ESHU_WORKFLOW_COORDINATOR_GCP_FRESHNESS_CLAIM_LEASE_DURATION` | duration | `5m` | Claim lease duration for GCP freshness trigger handoff (#4576). |
| `ESHU_WORKFLOW_COORDINATOR_HEARTBEAT_INTERVAL` | duration | — | Heartbeat interval for claim owners. |
| `ESHU_WORKFLOW_COORDINATOR_REAP_INTERVAL` | duration | — | Expired-claim reaping interval. |
| `ESHU_WORKFLOW_COORDINATOR_RECONCILE_INTERVAL` | duration | `30s` | Workflow state reconciliation interval. |
| `ESHU_WORKFLOW_COORDINATOR_RUN_RECONCILE_INTERVAL` | duration | `30s` | Run-level reconciliation interval. |
| `ESHU_WORKFLOW_COORDINATOR_TENANT_BOUNDARY_JSON` | string | — | JSON tenant boundary configuration. |
| `ESHU_WORKFLOW_COORDINATOR_WORK_ITEM_RETENTION_AGE` | duration | `720h` | How long the active-mode coordinator keeps a completed or failed_terminal workflow_work_items row before retention deletes it (#8829); on by default, 0 turns retention off, otherwise at least 48h. Deleted history cannot be restored. |
| `ESHU_WORKFLOW_COORDINATOR_WORK_ITEM_RETENTION_BATCH_SIZE` | int | `1000` | Rows one retention batch transaction examines and may delete; 1 to 10000. |

## projector

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_PROJECTOR_MAX_ATTEMPTS` | int | `3` | Maximum retry attempts for projector work items. |
| `ESHU_PROJECTOR_RETRY_DELAY` | duration | `30s` | Delay between projector work-item retries. |
| `ESHU_PROJECTOR_WORKERS` | int | — | Concurrent projector workers (default NumCPU capped at 8, min 1). |

## push-hooks

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_DOCS_BUILD_TIMEOUT_SECONDS` | int | `600` | Bound in seconds for the docs-build pre-push hook (scripts/verify-docs-build-changed.sh): past the bound the hook fails closed naming the live-gate holder. Integer 1 through 3600; unset keeps 600 (#7983). |

## reducer

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_ACTIVATION_OBLIGATION_CONSUMER_ENABLED` | bool | `true` | Run the #7584 activation obligation consumer in the resolution engine. It settles the exact-generation obligations ProjectorQueue.Ack writes by running the partition-scoped deferred maintenance pass on the owed (scope, generation) only when its backward-evidence phase is missing, then waking the deployment_mapping rows waiting on it. On by default since the #7638 flip; set false to restore the old behavior, under which obligations stay pending until generation retention removes their generation. |
| `ESHU_ACTIVATION_OBLIGATION_LEASE` | duration | `30m` | How long one activation obligation claim holds its row (#7638 item 2, arbiter-ruled 30m interim). Read only while ESHU_ACTIVATION_OBLIGATION_CONSUMER_ENABLED is on. An unparsable or non-positive value falls back to the default without failing startup. The interim 30m default fits the measured 395 s full-corpus (984-scope) pass inside the lease-minus-a-fifth deadline; lower back only when a rewrite re-measures the pass p99 far below the deadline. |
| `ESHU_ACTIVATION_OBLIGATION_POLL_INTERVAL` | duration | `10s` | Wait after an activation obligation cycle that claimed nothing. Read only while ESHU_ACTIVATION_OBLIGATION_CONSUMER_ENABLED is on. An unparsable or non-positive value falls back to the default without failing startup. |
| `ESHU_ACTIVATION_OBLIGATION_WORKERS` | int | `1` | Concurrent activation obligation claim loops in one reducer process; workers claim through SKIP LOCKED so they never share work. Read only while ESHU_ACTIVATION_OBLIGATION_CONSUMER_ENABLED is on. An unparsable or non-positive value falls back to the default without failing startup. Values above 1 are unprobed; keep 1 until full-corpus evidence lands (#7638 item 3). |
| `ESHU_CHANGED_SINCE_LINK_BACKFILL_SCOPES_PER_CYCLE` | int | `10` | Scopes with no activation journal row whose retained generation chain one changed-since cycle backfills. |
| `ESHU_CHANGED_SINCE_LINK_MAX_ATTEMPTS` | int | `5` | Counting failures (statement_timeout, connection_lost, sql_error, internal) of one activation before it becomes a link_poisoned chain break: the cursor advances, the scope keeps its state, and the next full generation links from it. Retries back off min(30m, 30s*2^(n-1)); lock misses and busy slots never count. |
| `ESHU_CHANGED_SINCE_LINK_POLL_INTERVAL` | duration | `5s` | Wait between changed-since link cycles when a cycle was idle, retried, or failed. A cycle that linked or broke anything starts the next at once. |
| `ESHU_CHANGED_SINCE_LINK_SLOTS` | int | `2` | Database-wide cap on concurrent full changed-since links (root and incremental), held as transaction advisory locks shared by every reducer replica. Each full link of a 0.77M-key scope holds up to about 1 GiB of Postgres backend memory at work_mem 256MB. Raise only with the 1/2/4-link cap evidence. |
| `ESHU_CHANGED_SINCE_LINK_STATEMENT_TIMEOUT` | duration | `120s` | statement_timeout of one changed-since link statement; the whole link transaction is also bounded by this value plus 30s. A timeout rolls back and is a counting failure (failure class statement_timeout): it is recorded on the scope cursor with backoff and, at ESHU_CHANGED_SINCE_LINK_MAX_ATTEMPTS, becomes a link_poisoned chain break. |
| `ESHU_CHANGED_SINCE_LINK_WORKERS` | int | `4` | Scopes one reducer links at once. Full links are further bounded by ESHU_CHANGED_SINCE_LINK_SLOTS across all replicas. |
| `ESHU_CODE_CALL_PROJECTION_LEASE_OWNER` | string | `code-call-projection-runner` | Prefix for the code-call partition lease owner. The reducer appends hostname, PID, and a boot nonce so replicas and restarted processes never share one active owner identity. |
| `ESHU_CODE_VALUE_FLOW_STALE_CLEANUP_DELETE_BATCH_LIMIT` | int | `500` | Maximum stale value-flow evidence nodes or edges deleted per active scope and family in one reducer cleanup pass. |
| `ESHU_CODE_VALUE_FLOW_STALE_CLEANUP_ENABLED` | bool | `true` | Enable the reducer side runner that removes stale CodeTaintEvidence nodes and TAINT_FLOWS_TO edges from older active-scope generations. |
| `ESHU_CODE_VALUE_FLOW_STALE_CLEANUP_LEASE_OWNER` | string | — | Lease owner for the single value-flow stale cleanup worker; defaults to a unique process token. |
| `ESHU_CODE_VALUE_FLOW_STALE_CLEANUP_LEASE_TTL` | duration | `10m` | TTL for the value-flow stale cleanup partition lease. Keep above the graph write budget (the QA environment's ESHU_CANONICAL_WRITE_TIMEOUT is 300s) plus margin (#7047). |
| `ESHU_CODE_VALUE_FLOW_STALE_CLEANUP_POLL_INTERVAL` | duration | `1h` | Delay between value-flow stale cleanup passes. |
| `ESHU_CODE_VALUE_FLOW_STALE_CLEANUP_SCOPE_BATCH_LIMIT` | int | `100` | Active repository scopes scanned per value-flow stale cleanup pass. |
| `ESHU_GENERATION_LIVENESS_ACTIVATION_DEADLINE` | duration | `30m` | Age past which an active generation with outstanding shared intents and no unresolved reducer work can count as wedged. |
| `ESHU_GENERATION_LIVENESS_BATCH_LIMIT` | int | `200` | Maximum generations the sweep re-drives in one poll. |
| `ESHU_GENERATION_LIVENESS_ENABLED` | bool | `true` | Runs the generation-liveness sweep in the resolution engine. When false the sweep neither re-drives wedged active generations nor supersedes orphaned older active generations; the stuck gauge bucket still reports. |
| `ESHU_GENERATION_LIVENESS_MAX_RECOVER_ATTEMPTS` | int | `5` | Per-generation re-drive budget; a generation at the ceiling is no longer re-driven by the sweep. |
| `ESHU_GENERATION_LIVENESS_POLL_INTERVAL` | duration | `5m` | How often the generation-liveness sweep runs. |
| `ESHU_GENERATION_LIVENESS_PROGRESS_WINDOW` | duration | `10m` | Quiet period a blocking shared-intent projection_domain queue must show before the generation-liveness sweep re-drives a wedged active generation; generations whose domain queue completed work inside it are counted draining and skipped. Raised to the liveness poll interval when set lower. |
| `ESHU_GENERATION_RETENTION_HARD_MAX_SUPERSEDED_AGE` | duration | `2160h` | Hard history ceiling for superseded generations (#7585): ordinary superseded history older than this is eligible for bounded cleanup even when the retained count (ESHU_GENERATION_RETENTION_MIN_SUPERSEDED_GENERATIONS) would keep it. Unset resolves to 2160h, or to ESHU_GENERATION_RETENTION_MAX_SUPERSEDED_AGE when that is longer (#7611); an explicit value below ESHU_GENERATION_RETENTION_MAX_SUPERSEDED_AGE is rejected at startup. |
| `ESHU_GENERATION_RETENTION_MAX_SUPERSEDED_AGE` | duration | `168h` | Soft keep window for superseded generations: newer generations stay retained. The hard history ceiling must not be set below this. |
| `ESHU_GENERATION_RETENTION_MIN_SUPERSEDED_GENERATIONS` | int | `24` | Minimum superseded generations retained per scope after the active one; the hard history ceiling (ESHU_GENERATION_RETENTION_HARD_MAX_SUPERSEDED_AGE) still applies past it. |
| `ESHU_GENERATION_RETENTION_SEARCH_INDEX_SUPERSEDED_SWEEP_ENABLED` | bool | `false` | Opts into the superseded search index sweep (#8231): a still-existing superseded generation's search index rows (terms, documents, and stats tables) are removed in committed batches while its facts stay held. Off by default; with the switch off the sweep issues no SQL. Enablement stays held until the issue's gate-(b) primary dead-tuple/bloat evidence lands. |
| `ESHU_GRAPH_GAUGE_REFRESH_INTERVAL` | duration | `5m` | Delay between background refreshes of the graph-backed observable gauges (eshu_dp_edges_by_source_tool, eshu_dp_files_by_language, eshu_dp_graph_orphan_nodes). /metrics serves the last snapshot, so while refreshes succeed those gauges lag the graph by up to this interval plus one read. If refreshes keep failing the gauges stop reporting once the snapshot is older than three intervals; eshu_dp_gauge_snapshot_age_seconds shows the age. |
| `ESHU_GRAPH_GAUGE_REFRESH_TIMEOUT` | duration | `30s` | Deadline for each background graph read that refreshes a graph-backed observable gauge. A read that exceeds it is cancelled and counted as a timeout in eshu_dp_gauge_snapshot_refreshes_total; the previous snapshot keeps being served until it is three intervals old. |
| `ESHU_GRAPH_ORPHAN_SWEEP_BATCH_LIMIT` | int | `100` | Maximum graph orphan nodes deleted per label in one sweep pass. |
| `ESHU_GRAPH_ORPHAN_SWEEP_COUNT_LIMIT` | int | `10000` | Maximum graph orphan nodes counted per label for telemetry in one sweep pass. |
| `ESHU_GRAPH_ORPHAN_SWEEP_ENABLED` | bool | `true` | Enable the reducer side runner that marks and sweeps stale generation-owned graph orphans. |
| `ESHU_GRAPH_ORPHAN_SWEEP_LEASE_OWNER` | string | — | Lease owner for the single graph orphan sweep worker; defaults to a unique process token. |
| `ESHU_GRAPH_ORPHAN_SWEEP_LEASE_TTL` | duration | `10m` | TTL for the graph orphan sweep partition lease. Keep above the graph write budget (the QA environment's ESHU_CANONICAL_WRITE_TIMEOUT is 300s) plus margin (#7047). |
| `ESHU_GRAPH_ORPHAN_SWEEP_POLL_INTERVAL` | duration | `1h` | Delay between graph orphan sweep passes. |
| `ESHU_GRAPH_ORPHAN_SWEEP_TTL` | duration | `168h` | Minimum age before a marked graph orphan can be deleted. |
| `ESHU_ID_ANCHOR_CENSUS_ENABLED` | bool | `true` | Runs the periodic id-anchor census (#7212) on Neo4j: one AllNodesScan that counts graph nodes with an id the labeled entity-context anchor cannot reach, reported as eshu_dp_graph_id_anchor_unreachable_nodes. No effect on NornicDB. |
| `ESHU_ID_ANCHOR_CENSUS_POLL_INTERVAL` | duration | `1h` | Delay between id-anchor census passes. The first pass runs at startup. |
| `ESHU_ID_ANCHOR_CENSUS_TIMEOUT` | duration | `2m` | Deadline for one id-anchor census pass. A pass that exceeds it counts as failed and keeps the last good snapshot. |
| `ESHU_INFRA_INVENTORY_RECONCILE_ENABLED` | bool | `true` | Run the infra read model reconcile loop, which re-derives repositories whose infra_resource_entities rows drifted from content_entities, and repairs rolling-upgrade fence marks. With it false, nothing repairs rolling-upgrade fence marks: one write from an older binary or manual SQL keeps unscoped infra aggregate reads on the graph until the loop runs again (see eshu_dp_infra_inventory_dirty_repos and the infra_inventory admin status field). |
| `ESHU_INFRA_INVENTORY_RECONCILE_INTERVAL` | duration | `5m` | Wait between infra read model reconcile cycles. |
| `ESHU_INFRA_INVENTORY_RECONCILE_REPO_BUDGET` | int | `500` | Repositories one infra read model reconcile cycle checks. |
| `ESHU_POSTGRES_GAUGE_REFRESH_INTERVAL` | duration | `5m` | Delay between background refreshes of the Postgres-backed observable gauges (queue depth/age, shared acceptance rows, workflow family queue depth, active generations, poison liveness), read by the reducer and the ingester. /metrics serves the last snapshot, so while refreshes succeed those gauges lag Postgres by up to this interval plus one read. If refreshes keep failing the gauges stop reporting once the snapshot is older than three intervals; eshu_dp_gauge_snapshot_age_seconds shows the age. |
| `ESHU_POSTGRES_GAUGE_REFRESH_TIMEOUT` | duration | `30s` | Deadline for each background Postgres read that refreshes a Postgres-backed observable gauge. A read that exceeds it is cancelled and counted as a timeout in eshu_dp_gauge_snapshot_refreshes_total; the previous snapshot keeps being served until it is three intervals old. |
| `ESHU_REDUCER_ADMISSION_HIGH_WATER_MARK` | int | `10000` | Ingester source-local reducer-intent admission threshold; defers while outstanding reducer queue depth is at or above this value. Set to 0 to disable. |
| `ESHU_REDUCER_ADMISSION_POLL_INTERVAL` | duration | `1s` | Queue-depth recheck interval while reducer admission is deferring. |
| `ESHU_REDUCER_ADMISSION_RETRYING_HIGH_WATER_MARK` | int | `500` | Ingester graph-write backpressure: defers source-local reducer-intent admission while retrying-state reducer depth (the durable signal of recurring graph-write timeouts) is at or above this value, so recoverable work is throttled instead of dead-lettered. Set to 0 to disable. |
| `ESHU_REDUCER_ADMISSION_RETRYING_LOW_WATER_MARK` | int | `100` | Hysteresis floor for graph-write backpressure; admission resumes only after retrying-state reducer depth falls below this value. Must be less than ESHU_REDUCER_ADMISSION_RETRYING_HIGH_WATER_MARK. |
| `ESHU_REDUCER_BATCH_CLAIM_SIZE` | int | — | Work items claimed per cycle (default adaptive to workers and backend). |
| `ESHU_REDUCER_CLAIM_DOMAIN` | string | — | Single reducer claim domain. Deprecated; use `ESHU_REDUCER_CLAIM_DOMAINS`. |
| `ESHU_REDUCER_CLAIM_DOMAINS` | string | — | Comma-separated reducer domains for multi-domain claims. |
| `ESHU_REDUCER_HANDLER_TIMEOUT` | duration | — | Default bound on one reducer handler execution. Recorded but not enforced yet (#8211 step a). Invalid values fail startup. |
| `ESHU_REDUCER_HANDLER_TIMEOUT_OVERRIDES` | string | — | Per-domain reducer handler bounds as domain=duration,... . Recorded but not enforced yet (#8211 step a). Bad entries and duplicate domains fail startup. |
| `ESHU_REDUCER_MAX_ATTEMPTS` | int | `3` | Maximum retry attempts for reducer work items. |
| `ESHU_REDUCER_RETRY_DELAY` | duration | `30s` | Delay between reducer work-item retries. |
| `ESHU_REDUCER_WORKERS` | int | — | Concurrent reducer workers (default derived from CPU count and backend). |
| `ESHU_SECRET_LINES_REPAIR_ENABLED` | bool | `true` | Starts the reducer's hardcoded-secret side table repair (#8833). Whenever content_file_secret_lines_state is not ready (a migration that changes the derivation function, or a failed or abandoned bootstrap finalize), the repair takes the bulk-load advisory locks (5318,1) and (5318,2) without waiting, on a dedicated one-connection pool (so the reducer opens up to ESHU_POSTGRES_MAX_OPEN_CONNS + 1 connections while a rebuild runs), rebuilds the side table, and publishes ready; it skips while any bootstrap-index connection is alive; until then investigate_hardcoded_secrets reads use the legacy content scan. Leave it on: false is for diagnostics only, and reads then stay on the legacy scan until a bootstrap-index run publishes. Accepts true or false; any other value stops startup with an error that names the variable. |
| `ESHU_SECRET_LINES_REPAIR_POLL_INTERVAL` | duration | `30s` | How often the reducer's hardcoded-secret side table repair (#8833) reads content_file_secret_lines_state. A ready state costs one indexed read per poll. After a failed rebuild the wait doubles per consecutive failure up to 1h. A value below 1s or one that does not parse stops startup with an error that names the variable. |
| `ESHU_SHARED_PROJECTION_LEASE_OWNER` | string | `shared-projection-runner` | Prefix for the shared-projection partition lease owner. The reducer appends hostname, PID, and a boot nonce so replicas and restarted processes never share one active owner identity. |
| `ESHU_SHARED_PROJECTION_PARTITION_BACKOFF_MAX` | duration | `30s` | T_max for per-partition shared-projection backoff: the longest a persistently unproductive (domain, partition) waits between visits, and the pickup bound for work arriving during backoff (within T_max plus one global poll interval, at most 5s). Clamped to 5m; empty or invalid values fall back to the default. |
| `ESHU_STATUS_SUMMARY_WRITER_ENABLED` | bool | `false` | Run the reducer's periodic status summary writer (#7009), which stores the active-work summary, the Terraform-state admin evidence, and the workflow coordinator snapshot (#8828) (three rows in one pass, each in its own transaction) in status_summary_snapshots every ESHU_STATUS_SUMMARY_WRITER_INTERVAL. One replica computes per tick under a transaction advisory lock; the others skip. Off by default; with it off the reducer starts no writer and issues no writer SQL. Nothing reads the row until the status reader flag ships. |
| `ESHU_STATUS_SUMMARY_WRITER_INTERVAL` | duration | `10s` | Cadence of the status summary writer. The 10s default follows the 1.0s median active-work pass measured on the QA read replica. Values below 5s, or unparsable values, fail reducer startup: a 2s cadence measurably raised reducer claim latency. A pass longer than the interval is counted in eshu_dp_status_summary_writer_overrun_total and the next pass starts on the following interval boundary; passes never overlap. |

## semantic

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_SEMANTIC_EXTRACTION_POLICY_JSON` | string | — | JSON semantic extraction policy controlling source/provider ACL decisions. |
| `ESHU_SEMANTIC_PROVIDER_EXECUTION_ENABLED` | bool | `false` | Default-off flag permitting real provider traffic (requires security review). |
| `ESHU_SEMANTIC_PROVIDER_PROFILES_JSON` | string | — | JSON array of semantic provider profile configurations, including optional search embedding dimensions. |
| `ESHU_SEMANTIC_PROVIDER_WORKER_ENABLED` | bool | `false` | Enable the semantic-provider worker claim loop. |
| `ESHU_SEMANTIC_SEARCH_LOCAL_EMBEDDER` | enum | — | Deterministic no-network or auto-local semantic search selector for API, MCP, and reducer. Allowed: `hash`, `local_hash`, `auto_hash`. |
| `ESHU_SEMANTIC_SEARCH_PROVIDER_PROFILE_ID` | string | — | Selects one governed search_documents provider profile when multiple semantic search providers are configured. |
