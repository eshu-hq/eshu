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
| `eshu_dp_code_fingerprint_entities_total` | Function body fingerprint volume for the code-divergence report (#6835), labeled by bounded `language` and `outcome` (`fingerprinted`, `skipped`); skipped rows carry bounded `reason` (`below_floor`, `has_error`, `no_body`). Absent fingerprint keys mean "not fingerprinted", never "unique". |
| `eshu_dp_code_fingerprint_duration_seconds` | Per-file fingerprinting time histogram, labeled by bounded `language`. Observed once per file that produced any fingerprint outcome. |
| `eshu_dp_file_prescan_duration_seconds` | Per-file pre_scan cost, labeled by bounded `language`. Only files that actually dispatch to a language pre-scanner emit a sample — `parser.IsDerivedPreScanLanguage` languages (php, javascript, typescript, tsx) derive their ImportsMap contribution from the parse stage on a full ingest (#4764) and so contribute no samples there; a delta sync still runs the legacy pre_scan pass for every language and so does emit samples for those too. Pairs with the `pre_scan` stage's `language_prescan_summary` structured-log bucket (mirrors the parse stage's `language_parse_summary`) to attribute pre_scan cost per language the same way parse cost is already attributed. |
| `eshu_dp_scip_snapshot_attempts_total` | SCIP supplement attempt volume per selected language package or workspace root, labeled by bounded `language` and `result` (`used`, `disabled`, `no_supported_language`, `binary_unavailable`, `indexer_failed`, `parse_failed`, or `empty_result`). A sustained non-`used` rate means call precision is falling back to native parser output; investigate SCIP binary availability, indexer errors, parser errors, language allowlists, or empty index output. Repository names, root paths, file paths, and index paths stay out of labels. |
| `eshu_dp_scip_process_wait_seconds` | Time spent waiting for the shared SCIP process limiter before launching an external indexer, labeled only by bounded `language`. Sustained wait growth means `SCIP_WORKERS` is saturated across concurrent repository snapshots; either raise the worker budget with CPU/memory proof, narrow `SCIP_LANGUAGES`, or lower snapshot concurrency. |
| `eshu_dp_discovery_dirs_skipped_total` | Directory pruning by discovery policy. |
| `eshu_dp_discovery_files_skipped_total` | File pruning by discovery policy. |
| `eshu_dp_large_repo_classifications_total` | Large-repo classification volume. |
| `eshu_dp_large_repo_semaphore_wait_seconds` | Wait time for large-repo concurrency control. |
| `eshu_dp_facts_emitted_total` | Collector fact output volume. |
| `eshu_dp_facts_committed_total` | Durable fact commit volume. |
| `eshu_dp_generation_fact_count` | Fact volume per generation. |
| `eshu_dp_ingestion_successor_guard_decisions_total` | Destructive-successor guard decisions at ingestion commit (#8750, the #7926 backstop), one per guarded commit, labeled by `kind` (`full`, `delta`), the closed `rule` set (`trusted_deletion`, `no_prior`, `orphan_fallback`, `empty`, `collector_ratio`, `anchor_missing`, `file_union_bound`, `file_union_exact`, `other`), and `outcome` (`stored`, `refused`). A full successor is compared with the prior's collector rows (reducer-written rows excluded); a git delta is sized by its file union against its anchor generation. A rising `refused` rate with `rule=collector_ratio` or `file_union_exact` is real shrinkage or a collector problem; read the `successor_guard` commit-stage log for the counts. A steady `file_union_exact` rate means deltas are removing most of their anchor's files and paying the probe. |
| `eshu_dp_collector_delta_baseline_fallback_total` | Git delta syncs that fell back to a full snapshot, labeled by `skip_reason` (`no_projected_baseline`, `no_projected_full`, `baseline_unreachable`, `delta_exceeds_base_half`, `baseline_lookup_error`, `default_branch_changed`). `no_projected_full` means the scope has an active generation but no activated full to diff a cumulative delta from; `delta_exceeds_base_half` is the #8780 growth valve (the cumulative delta touched at least half of the union base's files). A sustained rate means delta sync is rarely applying; investigate clone depth, repeated projection failures, or (for `baseline_lookup_error`) Postgres availability. `default_branch_changed` counts a repository whose remote default branch changed (#7678); the paired WARN log `git repository default branch changed` names the repository and both branches. A change counts at most once: one found on a forced reconciliation or after a baseline lookup failure is logged but not counted, because those syncs take a full snapshot anyway. A forced reconciliation, including the #7389 `graph_dirty` reason, never counts here. |
| `eshu_dp_collector_delta_touched_ratio` | Histogram of each cumulative git delta's touched paths over its union base's tracked files (#8780), labeled by `outcome` (`delta` when the delta was used, `delta_exceeds_base_half` when the growth valve took a full). It is the growth signal of cumulative deltas: a p95 climbing toward 0.5 means deltas are drifting far from their base and the reconcile interval is too long for that churn. Buckets 0.01-2. |
| `eshu_dp_collector_repository_selection_evaluations_total` | Repository selection evaluations (#7625) on shard 0, labeled by `collector_kind`, `selector_kind`, and `outcome`. `selector_kind=github_org` counts one per githubOrg cycle; `selector_kind=gitlab_group` counts one per gitlabGroup cycle (#7765); `selector_kind=explicit` counts one per owner of the explicit repository list per cycle and only ever reports `evaluated` or `store_error`. Outcomes: `evaluated` (observations written), `listing_truncated` (the listing reached the selector's repository limit in [ingestion environment](../environment-ingestion-queues.md), nothing written), `guard_tripped` (more newly unlisted scopes than `max(10, ceil(10% of known))`, or an empty listing with known scopes; nothing written), or `store_error` (a Postgres read or the upsert failed; the cycle continued). The listing pages at 100 repositories per request, keeps paging past a short page, and counts as complete only when an empty page without a `Link` `rel="next"` arrives before the limit; a listing that reaches the limit counts as truncated even when the org or group holds exactly that many repositories. A steady `listing_truncated` means the repository limit is not above the org or group size, so no scope can ever be marked `not_listed`. Every cycle logs INFO `git_repository_selection_evaluated` with its `outcome` and counts; the other outcomes also log WARN `git_repository_selection_listing_truncated`, `git_repository_selection_guard_tripped`, or `git_repository_selection_store_failed` (with `failure_class`). |
| `eshu_dp_collector_repository_listing_requests_total` | GitLab group listing requests (#7765), labeled by `collector_kind`, `provider` (`gitlab`), and `outcome` (`success`, `retried`, `rate_limited`, `http_error`, `transport_error`, `decode_error`). One increment per page request plus one per group resolve; a `retried` count with no `rate_limited` means throttling was absorbed. Every finished listing logs INFO `git_gitlab_group_listing_completed` with its page and project counts; every waited retry logs WARN `git_gitlab_group_listing_retry`. |
| `eshu_dp_collector_repository_selection_scopes` | Gauge of the evaluating selector's known repository scopes by selection `state` after the last `evaluated` cycle (#7625, #7765): `selected`, `not_listed_pending` (unlisted, not yet confirmed), `not_listed` (unlisted for at least two cycles spanning at least five minutes), `archived_excluded`, `rule_excluded`. Archived and rule-excluded scopes count from their first cycle; the freshness `not_selected` verdict applies the same confirmation rule to every non-selected state. A non-zero `not_listed` names scopes that dropped out of the org or group listing (transferred, renamed, or deleted upstream) and still hold their last graph; the evaluated log's `not_listed_sample` lists up to 10 of them. |
| `eshu_dp_collector_repository_selection_observations_deleted_total` | Expired `repository_selection_observations` rows deleted by the shard 0 sweep (#7774), labeled by `collector_kind`. The sweep runs at most once per collector cycle (a cycle with no observation request, in filesystem mode, with an explicit list that has no owners, or whose discovery fails, issues none), after the githubOrg request (or, in explicit mode, the last owner's request) ends in any outcome but `store_error`, including `listing_truncated`, and deletes rows of any selector whose `evaluated_at` plus their own liveness window plus a 7-day grace has passed. A `not_listed` row, which the mass-miss guard reads, is deleted only for an abandoned selector (#7840): every cycle advances its selector's `repository_selectors.last_cycle_at` marker (including a guard-tripped cycle, which writes no observation rows), and a `not_listed` row is reclaimed only when that marker is missing or older than the row's window plus the grace. It deletes at most 500 rows per statement and 10,000 per cycle, so a large backlog drains over several cycles. The INFO `git_repository_selection_evaluated` log of the request that swept carries `expired_deleted_count` and `not_listed_reclaimed_count`. A failed sweep logs WARN `git_repository_selection_store_failed` with `failure_class=expired_sweep`, and a failed marker write the same log with `failure_class=selector_cycle_upsert`; both leave the cycle's `outcome` unchanged. Growth after a credential rotation, rules change, or owner change is expected: the old selector's rows become sweepable 7 days after its window ends. |
| `eshu_dp_collector_repository_selection_not_listed_reclaimed_total` | The subset of `eshu_dp_collector_repository_selection_observations_deleted_total` that were abandoned selectors' `not_listed` rows (#7840), labeled by `collector_kind`. A nonzero value means a selector stopped cycling for longer than its rows' liveness window plus the 7-day grace — expected after a credential rotation, rules change, or owner change, and a signal of a dead collector otherwise. A selector that still cycles, even one whose mass-miss guard trips every cycle, keeps its `not_listed` rows and never counts here. |
| `eshu_dp_collector_repository_selection_probe_total` | Upstream classification probe answers (#8454), labeled by `collector_kind` and `outcome`. After an `evaluated` githubOrg cycle the observer probes the `Confirmed` `not_listed` rows by stored id (`GET /repositories/{id}`, `/repos/{slug}` fallback when the id is 0) with the listing credential, at most 25 per cycle oldest-first, re-probing a classified scope at most once per liveness window. Outcomes: `transferred` (still exists under another owner), `renamed` (same owner, new name; the successor slug is stored, resolution at read time only), `still_present` (same name: the listing missed a present repository), `not_found_or_inaccessible` (404, never "deleted"), or `kept_prior` (5xx, rate-limit, network, decode, or probe error: the prior verdict stands unwritten). Probe errors never fail ingestion. A failed verdict write logs WARN `git_repository_selection_store_failed` with `failure_class=classification_upsert` and leaves the outcome `evaluated`. |
| `eshu_dp_collector_repository_selection_classifications` | Gauge of the probing githubOrg selector's `Confirmed` `not_listed` scopes by `classification` after the last `evaluated` cycle that probed (#8454): `transferred`, `renamed`, `still_present`, `not_found_or_inaccessible`, or `unclassified` (no probe has reported yet). Sampled only when the probe ran and its write succeeded; a failed write leaves the previous sample rather than reporting what the store does not hold. |
| `eshu_dp_git_repo_sync_failures_total` | Per-repository git sync operations that failed and were isolated to that one repository for the cycle — the failing repo is skipped and the rest of the fleet keeps syncing. With scheduled sync enabled it is retried on the next poll; in webhook-only mode (scheduled sync disabled; see [ingestion and queue environment](../environment-ingestion-queues.md)) it waits for its next webhook trigger, and the current trigger is still marked `handed_off`, the same as an isolated clone or fetch failure — labeled by bounded `operation` (`clone`, `fetch`, `list_refs`) (issue #7001). A sustained `list_refs` rate on a handful of repos points at remote/DNS trouble for those specific repos; a rate approaching the fleet size across all three operations points at a fleet-wide auth or network outage. Repository identity stays in the paired `git repository sync failed` log line (`failure_class=git_sync_failure` (a failure the retry classifier names, issue #8091, carries that class instead, such as `dns_resolve_failed` or `auth_failed`, plus an `attempts` field)), not in the metric label. |
| `eshu_dp_git_command_deadline_exceeded_total` | Git commands the collector's sync loop killed because they ran past their deadline (issue #8052), labeled by the closed `operation` set `clone`, `fetch`, `ls_remote` (the per-repository ref listing), and `resolve` (the remote default-branch lookup). The loop is sequential, so a command stuck on name resolution used to pause the whole fleet sync for libcurl's 300 s; now it costs at most its own deadline: `ESHU_GIT_RESOLVE_TIMEOUT_SECONDS` (default 60 s) for `ls_remote` and `resolve`, `ESHU_GIT_FETCH_TIMEOUT_SECONDS` (90 s) for `fetch`, `ESHU_GIT_CLONE_TIMEOUT_SECONDS` (600 s) for `clone`. Purely local git commands have no deadline. Fetch and clone also pass `http.lowSpeedLimit=1` and `http.lowSpeedTime=60`, so a transfer stuck at 0 bytes per second for 60 s aborts by itself with git's own `Operation too slow` error, which this counter does not count. One increment per killed attempt (issue #8091: a deadline kill is one attempt of the bounded retry, so a command that recovered on its retry counts a kill and writes no `git repository sync failed` line); a shutdown cancel is never counted. Each killed attempt writes a `git command attempt failed` WARN. A command whose last attempt was a kill in the main sync is paired with `eshu_dp_git_repo_sync_failures_total` and one `git repository sync failed` log line per repository failure carrying `failure_class=deadline_exceeded`, `operation`, `deadline_operation`, `deadline_seconds`, `elapsed_seconds`, `attempts`, `repository_id`, and `repository_index`, and the repository is retried next cycle. A pinned-ref fetch that hits its deadline is counted here and logged in the `failed to fetch pinned ref; skipping` warning, which then carries `failure_class=deadline_exceeded`, `deadline_operation`, `deadline_seconds`, `operation` and `elapsed_seconds`; it does not touch the sync-failure counter. A steady `fetch`, `ls_remote` or `resolve` rate on a handful of repositories points at name resolution or remote trouble; a `clone` count means a clone outlived the clone deadline, so raise it if the repository is large and healthy. |
| `eshu_dp_git_command_attempts_total` | Every attempt of a network git command the sync loop ran, including the first (issue #8091), by the closed `operation` set `clone`, `fetch`, `ls_remote`, `resolve`, the provider `host` (`github.com`, `bitbucket.org`, the configured GitLab host, or `unknown`), the closed `failure_class` set, which is the contract: `none` on success; the seven retryable classes `deadline_exceeded`, `dns_resolve_failed`, `connect_failed`, `connection_reset`, `http2_framing`, `tls_handshake`, `low_speed_abort`; the five non-retryable classes `auth_failed`, `http_4xx`, `repository_not_found`, `rate_limited`, `pack_object_error`; and the fallback `git_sync_failure` for any text the classifier does not name (HTTP 5xx is in the fallback in this version), and `outcome` (`success`, `retried`, `failed`). One increment per attempt; a shutdown cancel is never counted. Sum `outcome="retried"` to see how often a connectivity failure was absorbed, and `outcome="failed"` by `failure_class` for failures that stayed final. |
| `eshu_dp_git_command_retries_total` | Retries the loop started after a retryable connectivity failure (issue #8091), by `operation`, `host`, and the `failure_class` that caused the retry. It is counted when the backoff ends and the next attempt starts, so a retry cancelled during its backoff counts in `eshu_dp_git_command_attempts_total{outcome="retried"}` but not here. A rate on one `host` points at that provider's network path; a rate across all hosts points at the collector's own egress or resolver. `ESHU_GIT_RETRY_ATTEMPTS=1` turns it off. |
| `eshu_dp_github_api_attempts_total` | Every attempt of a GitHub REST call that repository discovery makes, including the first (issue #8084): the org repository listing page (`operation="list"`) and the GitHub App installation token mint (`operation="mint"`). `failure_class` is `none` on success; otherwise `server_error` (HTTP 5xx), `rate_limited` (429, or 403 with a `retry-after` header or `x-ratelimit-remaining` 0), `client_error` (any other 4xx or 3xx), `decode` (invalid JSON on a fully read body), or a transport class (`reset`, `refused`, `timeout`, `unreachable`, `eof`, `dns`, `other`; an HTTP/2 stream reset is `reset`). `outcome` is `success`, `retried`, or `failed`. A `failed` count with `server_error` or `rate_limited` is one discovery call that ran out its budget; the collector then skips the cycle. |
| `eshu_dp_github_api_retries_total` | Retries of a GitHub REST call that repository discovery started after a retryable failure (issue #8084), by `operation` (`list`, `mint`) and the `failure_class` that caused the retry. It is counted when the wait ends and the next attempt starts, so a retry cancelled during its wait counts in `eshu_dp_github_api_attempts_total{outcome="retried"}` but not here. The budget is 4 attempts per call with 1 s, 2 s, 4 s waits spread over 75% to 125%; a `retry-after` or `x-ratelimit-reset` wait of at most 60 s replaces the backoff, and a longer one ends the call. A rate across all operations points at the collector's egress path; one on `mint` alone points at the App installation. |
| `eshu_dp_collector_discovery_skipped_cycles_total` | Discovery cycles the collector loop skipped after a retryable source failure instead of exiting the process (issue #8084), by `collector_kind`, `operation` (`list` for the org listing, `mint` for the GitHub App token mint), and the bounded `failure_class` of the failure (`server_error`, `rate_limited`, or a transport class `reset`, `refused`, `timeout`, `unreachable`, `eof`, `dns`). One increment per skipped cycle; the loop waits the error's `RetryAfter`, or the poll interval when it names none. A steady rate means the provider or the egress path is down, and `eshu_dp_collector_discovery_consecutive_failures` shows how close the process is to its crash-loop cap. |
| `eshu_dp_collector_discovery_consecutive_failures` | Discovery cycles in a row that ended in a retryable failure (issue #8084), by `collector_kind` and `operation` (`list` for the org listing, `mint` for the GitHub App token mint), as the git selectors count them. It rises by one per failing cycle of that operation. A native selection cycle that succeeds returns both operations to 0; a webhook sync of a non-empty repository set returns only `mint` to 0 and never `list`. At 20 for one operation the next failure is fatal and the process exits with an error that names the class and the count, so alert well below 20. |
| `eshu_dp_git_command_retry_budget_exhausted_total` | Retryable failures that were not retried because the per-sweep retry budget (`ESHU_GIT_RETRY_SWEEP_BUDGET`, default 20) was spent (issue #8091), by the closed `scope` set `sweep`. Each one is final on its first attempt, exactly as before the retry existed, and its `git command attempt failed` log line carries `retry_budget_exhausted=true`. A non-zero rate during an outage is the cap working; a non-zero rate on a healthy fleet means the budget is too small for the normal failure rate. |
| `eshu_dp_git_command_attempt_duration_seconds` | Wall time of each network git command attempt in seconds (issue #8091), by `operation` and `outcome` (`success`, `retried`, `failed`). Attempts that die at their deadline land at the deadline value (60, 90, or 600 s by default); compare `retried` with `success` to see what a recovered attempt costs the sweep. |
| `eshu_dp_git_resolve_cache_lookups_total` | Go-resolver lookups of a git provider host for the per-host address cache the sync passes to git as `http.curloptResolve` (issue #8092), by provider `host` and `result` (`success`, `error`, `empty`). Every git process would otherwise resolve again and one unanswered lookup stalls the serial sync; one receiver goroutine runs every lookup, single-flight per host with a 30 s minimum gap, under `ESHU_GIT_RESOLVE_CACHE_LOOKUP_SECONDS` (default 5 s). A steady `error` rate on one host points at the resolver path to that provider; `empty` means the resolver answered with no address and the last good set was kept. Paired with the `git resolve cache lookup` log line (INFO per result, WARN on failure) and the `git.resolve_cache.lookup` span. |
| `eshu_dp_git_resolve_cache_refresh_requests_total` | Address-refresh requests to the cache (issue #8092), by `host` and `reason`: `initial` (the first command for a host), `failure` (a retry whose attempt died at its deadline or with one of the closed curl wording rows — could not resolve host, resolving timed out, failed to connect, connection timed out, connection reset, Operation timed out after), `periodic` (the background refresh, `ESHU_GIT_RESOLVE_CACHE_REFRESH_SECONDS`, default hourly), and `suppressed` (a request inside the 30 s per-host minimum gap that started no lookup). HTTP status, auth, and pack answers never request a refresh, and a plain local `operation timed out` line does not either — only curl's `Operation timed out after` wording. |
| `eshu_dp_git_resolve_cache_fallback_total` | Network git commands that ran without a `curloptResolve` pin (issue #8092), by `host` and `reason`: `cache_empty` (no address cached yet — the first command waited at most the lookup deadline, then ran exactly as before the cache existed) and `proxy_set` (an HTTP_PROXY, HTTPS_PROXY, or ALL_PROXY setting means curl resolves the proxy's name, not the remote's). `ESHU_GIT_RESOLVE_CACHE=off` and SSH remotes pin nothing and count nothing here. A persistent `cache_empty` rate on one host means its lookups keep failing; read `eshu_dp_git_resolve_cache_lookups_total{result=error}` beside it. |
| `eshu_dp_git_resolve_cache_lookup_duration_seconds` | Wall time of each address-cache lookup in seconds (issue #8092), by `host` and `result`. Lookups that run out their budget land at `ESHU_GIT_RESOLVE_CACHE_LOOKUP_SECONDS` (default 5 s); a p99 near it means first commands for that host fall back unpinned and the sync still pays resolver stalls. |
| `eshu_dp_collector_reconciliation_full_snapshots_total` | Git scopes the periodic sweep forced to a full reconciliation snapshot to retract drift the delta path missed, labeled by `reason` (`never_reconciled`, `interval_elapsed`, `in_flight_expired`, `retry_after_unprojected`, `graph_dirty`, `reindex_requested`, `repository_reindex_requested`, `reconcile_request_stale`). `reconcile_request_stale` (#7978) means the active generation's re-projection request went unacknowledged for one interval or was dead-lettered, so the sweep forces a full that the store stores. `reindex_requested` (#7620) means an operator called `POST /api/v0/admin/reindex` after the scope's newest activated full generation; its rate rises after each request and falls to zero once every visited scope has re-parsed (INFO log `git_reconcile_forced` with `reason=reindex_requested` per forced scope). `repository_reindex_requested` (#7620) is the same for a repository named in a repository-scoped request, when that request is later than the fleet watermark; it rises by about one per requested repository. `graph_dirty` (#7389) means a generation wrote the graph and never activated (WARN log `git_delta_baseline_graph_dirty` names the generations and their failure classes); it should stay near zero unless matched by an Ack-phase delta-baseline refusal, a dead letter, a lease expiry, or the retry-then-newer path (writer `failure_class=projector_superseded_by_newer_generation`). A steady low rate of `interval_elapsed` is expected (one per scope per `ESHU_REPO_RECONCILE_INTERVAL_HOURS`); a spike means many scopes came due at once. A sustained `in_flight_expired` or `retry_after_unprojected` rate means forced full generations are not activating; match the WARN `git_reconcile_forced` log by `scope_id`. See [Reconciliation Sweep](../reconciliation-sweep.md). |
| `eshu_dp_collector_reconciliation_suppressed_total` | Reconciliation sweep evaluations that held a due scope off, labeled by `reason`: `reconcile_in_flight` (the newest full generation is still pending, younger than the interval) or `reconcile_retry_backoff` (it failed or was superseded before activating, younger than a quarter of the interval), or `in_flight_expired_held` (ruling a9-126: it is pending for longer than the interval at the remote head and its projector work is still live, so the sweep holds instead of parsing again; INFO log `git_reconcile_held`; the reducer or projector is behind on that scope). Counted once per scope per selection cycle while held. A sustained rate with few forced reconciliations points at projector lag. |
| `eshu_dp_collector_reconcile_parse_skip_decisions_total` | Pre-parse skip decisions of forced git reconciles (#7978 follow-up), labeled by `outcome`: `skipped` (the pre-parse identity equals the active generation's at the same commit, so the collector re-projects that generation without parsing; the store also counts `reconcile_reprojected` on `eshu_runtime_refresh_skipped_by_reason_total`), or why it parsed: `reason_ineligible` (only `interval_elapsed` and `graph_dirty` may skip), `disabled` (`ESHU_REPO_RECONCILE_PARSE_MAX_SKIPS=0`), `no_active_full`, `delta_active`, `head_moved`, `identity_absent` (the first sweep after deploy, which parses to stamp one unless the opt-in `ESHU_REPO_RECONCILE_STAMP_IDENTITY_WITHOUT_PARSE` stamp proves the generation), `stamped_without_parse` (that opt-in stamp wrote the identity without parsing, #8095), `identity_mismatch` (the content version or the parse configuration moved; WARN `git_reconcile_parse_skip_refused`), `submodules_present`, `dirty_tree`, `valve_due` (the scope skipped N-1 times), `read_error` (the tree check or the store call failed, or the store returned an unknown reason; WARN), `reprojection_pending` (the store found the re-projection already open or leased; no parse, INFO), `reprojection_superseded` (the store fence refused the re-open because another generation is active or the commit or identity changed; the collector parses in the same cycle; WARN). One increment per forced reconcile. A fleet at steady state reads mostly `skipped` and one `valve_due` per scope per N intervals; a sustained `identity_mismatch` rate after a deploy that did not change the content version or the configuration needs investigation. |
| `eshu_dp_collector_preparse_identity_dropped_total` | Generations the git collector kept the pre-parse identity off (#7978 follow-up), labeled by the closed `reason` set `telemetry.PreparseIdentityDropReasons`. Snapshot phase (the snapshot degraded before any fact was emitted): `scip_binary_unavailable`, `scip_indexer_failed`, `scip_parse_failed`, `file_read_failed`, `docs_read_failed`, `codeowners_read_failed`, `workflow_digest_failed`. Stream phase (the fact stream could not read a file between the parse and the emit, skipped it, and the store marked the generation `stream_dropped`): `content_read_failed`, `workflow_read_failed`, `docs_body_read_failed`. An unknown value counts as `other`. Each drop also logs WARN `git_snapshot_preparse_identity_dropped` with the repository, scope, and generation ids (and, for a stream drop, the relative path). A stream-dropped generation keeps its identity and records the dropped paths (`stream_dropped_paths`), so the next equal sweep verifies the paths still fail and skips (#8185); a marked one is still replaced by a new generation when the scope parses. A sustained stream-phase rate means files vanish or turn unreadable during ingestion. |
| `eshu_dp_collector_reconcile_parse_skip_persistent_drops_total` | Forced reconciles that skipped the parse while carrying the active generation's recorded stream drops (#8185), no labels. One increment per skip; the per-interval rate is the count of scopes in drop debt. Each such skip also logs INFO `git_reconcile_parse_skipped_with_persistent_drops` with `scope_id`, `generation_id`, `commit_sha`, `drop_count`, and `dropped_paths`. A recorded set that overflowed the 32-path cap forces a parse instead, since unrecorded drops cannot be re-checked. See [Reconciliation Sweep](../reconciliation-sweep.md). |
| `eshu_dp_reconciliation_drift_retractions_total` | Graph nodes and edges actually retracted by those forced reconciliation snapshots, labeled by bounded `domain`, `write_phase`, and `kind` (`node` or `edge`). A sustained nonzero rate after the first reconciliation window means delta-path cleanup is repeatedly finding stale graph state; investigate failed delta projections, graph write errors, or missed deletion events. Scope and repo identifiers stay in logs/spans, not metric labels. Ordinary workload removal is counted separately on `eshu_dp_workload_repository_edge_retractions_total`. |

Content-aware skip reasons use the `content:` prefix. A parser file over the 64 MiB per-file read cap counts as `skip_reason=content:oversize` (#8986, log key `files_skipped.content.oversize`, plus one WARN `git_snapshot_file_skipped_oversize` per file with `repository_id`, `scope_id`, `generation_id`, `relative_path`, `size_bytes`, and `cap_bytes`); it is never read, parsed, or streamed, and the rest of the repository keeps indexing. A symlink whose
resolved target fails admission counts as `skip_reason=content:symlink_target`
(#8809). Admission fails when the target dangles, leaves the repository, is not
a regular file, falls in a different admission category than the link name, or
sits where discovery would skip a regular file (an ignored or hidden directory,
an ignored extension, a hidden name, or an ignored path glob). The `discovery
stats` log carries it as `files_skipped.content.symlink_target`. The target
check runs before the path glob check, so a rejected link whose own path also
matches an operator glob counts as `content:symlink_target`, not `user:`.
`.gitignore` and `.eshuignore` run after the walk, so a dropped link those
files would also skip counts as `content:symlink_target`, not as an ignore
skip (it counted as an ignore skip before #8809).
Repo-local `.eshu/discovery.json` rules use the `user:` prefix. `.eshuignore`
matches use `skip_reason=eshuignore`.

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

Observability Evidence (#4767): before this change, an operator could see the
`pre_scan` stage's total wall time and file count via
`eshu_dp_collector_snapshot_stage_duration_seconds{stage="pre_scan"}` but could
not tell which language dominated that cost, unlike the `parse` stage which
already carried a `language_parse_summary` structured-log breakdown. The
pre_scan stage log now carries a matching `language_prescan_summary` bucket
(same `language`/`file_count`/`total_duration_seconds`/`avg_duration_seconds`
shape as `language_parse_summary`), and the new
`eshu_dp_file_prescan_duration_seconds` histogram (labeled `language`, mirrors
`eshu_dp_file_parse_duration_seconds`'s bucket boundaries) lets an operator
graph per-language pre_scan cost the same way per-language parse cost is
already graphed. Verified by
`TestNativeRepositorySnapshotterLogsPreScanLanguageSummary`
(`go/internal/collector/repo/git/snapshot_native_test.go`), which asserts the
`pre_scan` stage log line for a python+groovy fixture repo carries
`language_prescan_summary` with both languages' `file_count`,
`total_duration_seconds`, and `avg_duration_seconds`.

The summary only ever reflects pre_scan work that actually ran: `Engine`'s new
`PreScanRepositoryPathsWithWorkersStats` method (the stats-returning
counterpart to the unchanged `PreScanRepositoryPathsWithWorkers`) marks a file
`dispatched` only when it reached a language pre-scanner, so an unregistered
file or a `parser.IsDerivedPreScanLanguage` language (php, javascript,
typescript, tsx — which derive their ImportsMap contribution from the parse
stage on a full ingest per #4764 and so never reach `preScanOnePath`'s
dispatch) contributes zero fabricated samples. A delta sync still routes every
language through the legacy pre_scan pass and so still emits samples for all
of them, including the derived-eligible set.

No-Regression Evidence (#4767): `go test ./internal/parser ./internal/collector
./cmd/ingester -count=1` (all packages green) proves the three existing
exported `Engine.PreScanPaths` / `PreScanRepositoryPaths` /
`PreScanRepositoryPathsWithWorkers` methods keep their exact signatures,
merge/sort behavior, and first-error semantics —
`TestDefaultEnginePreScanRepositoryPathsWithWorkersMatchesSequential` and the
other 35+ existing PreScan call sites across `internal/parser`,
`internal/reducer`, `internal/resolutionparity`, and `internal/accuracygate`
are unmodified and pass unchanged. The added cost per pre-scanned file is one
`time.Now()` read at dispatch entry/exit (already timed identically at the
parse-stage per-file level) and one histogram `Record` call per dispatched
file at the collector layer; no new graph write, Cypher, queue, lease, batch,
or worker behavior is introduced, and the per-file work `preScanOnePath`
performs is unchanged. `go vet` confirms no other caller in
`internal/reducer`, `internal/resolutionparity`, or `internal/accuracygate`
needed changes.

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
| `eshu_dp_oci_registry_scan_duration_seconds` | `provider`, `result` | One repository scan before durable commit. `result` is `success`, `failed`, `retryable_transport` (a connection reset, unexpected EOF, or network timeout), or `retryable_status` (an HTTP 408, 5xx, or 429). A skipped target is retried on the next poll; the warn log carries `cause_class` and `consecutive_transport_failures`, and 20 consecutive failed cycles return the error as fatal, issue #7110. |
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
and `source_freshness=partial`; this is response metadata, not a metric label. Org fan-out counters: [Security Alert Org Fan-Out Telemetry](security-alert-org-fanout.md).

## CI/CD Run Collector

See [CI/CD Run Telemetry](metrics-cicd-run.md) for auth, webhook handoff, and fallback signals.

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
| `eshu_dp_confluence_sync_failures_total` | `failure_class` | Configuration, source-read, and fact-build failures, plus retryable backoff classes (`rate_limited`, `provider_unavailable`, `retryable_status`, and `transport_error` for a connection reset, unexpected EOF, or network timeout on one request, issue #7110). A retryable class schedules backoff and does not exit the collector, except that 20 consecutive `transport_error` retries return the failure as fatal (`source_read`) so a persistent wrong host or port crash-loops. The retry warn log carries `attempt`, `cause_class`, and `consecutive_transport_failures`. |

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
| `eshu_dp_workflow_coordinator_activation_derived_scopes_planned_total` | `collector_kind`, `collector_instance_id`, `ecosystem` | Derived scopes planned for a component activation per reconcile pass, before open-target admission (today a `vulnerability_intelligence` activation's OSV scopes, #9311). A repeat pass inside one plan bucket counts the same scopes again; the planned/enqueued log line shows what was admitted. |
| `eshu_dp_workflow_coordinator_semantic_provider_claim_total` | `outcome`, `provider_kind`, `provider_profile_class`, `source_class` | Semantic-provider worker claim outcomes by egress decision and terminal disposition (`egress_denied`, `egress_policy_missing`, `provider_disabled`, `dispatched`, `provider_unavailable`). Labels are redacted and low-cardinality; no provider host, endpoint, URL, or credential is emitted. |

## Webhook Listener

| Metric | Key labels | Use |
| --- | --- | --- |
| `eshu_dp_webhook_requests_total` | `provider`, `outcome`, `reason` | Public webhook requests, including rejected requests. |
| `eshu_dp_webhook_trigger_decisions_total` | `provider`, `event_kind`, `decision`, `reason`, `status` | Normalized provider events that reached durable trigger storage. |
| `eshu_dp_webhook_store_operations_total` | `provider`, `outcome`, `status` | Trigger-store upsert attempts. |
| `eshu_dp_webhook_request_duration_seconds` | `provider`, `outcome`, `reason` | End-to-end provider route duration. |
| `eshu_dp_webhook_store_duration_seconds` | `provider`, `outcome`, `status` | Durable trigger-store duration. |
| `eshu_dp_webhook_trigger_claim_reaps_total` | `outcome` (`requeued`, `exhausted`, `ignored_decision`) | Stale trigger claims recovered per tick (#7661); `ignored_decision` fails non-accepted claims with `ignored_decision` instead of requeueing them (#7795). |
| `eshu_dp_webhook_trigger_stale_claims` | — | Rows stuck in claimed past the lease window after the reap sweep (#7661). |
| `eshu_dp_webhook_trigger_ignored_decisions_total` | — | Triggers actually terminated for a non-accepted decision, in the claiming tick or the reap; a same-tick row flipped back to accepted stays on the handoff path and is not counted (#7795). |

Repository names, delivery IDs, branch names, and commit SHAs do not belong in
labels.
