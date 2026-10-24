# Telemetry Metrics

This page is the first-stop map for Eshu metrics. It keeps dashboard questions
short and links to focused catalogs for source-specific and reducer/storage
names.

Current metric sources:

- `go/internal/runtime/metrics.go` for `eshu_runtime_*` status metrics.
- `go/internal/runtime/postgres/telemetry.go` for API/MCP reader access stage and pool metrics when reader access is wired.
- `go/internal/telemetry/instruments.go` for most `eshu_dp_*` instruments.
- `go/internal/collector/terraformstate/metrics.go` for Terraform-state
  discovery candidate metrics.
- `go/internal/coordinator/metrics.go` for workflow-coordinator loop metrics.

## Reading Metrics

- Long-running runtimes expose `/metrics`.
- Runtime metrics use the `eshu_runtime_` prefix.
- Data-plane metrics use the `eshu_dp_` prefix.
- Prometheus resource labels include `service_name` and `service_namespace`;
  dashboards should filter by them.
- High-cardinality identifiers do not belong in labels. Use logs and traces for
  exact repository, scope, generation, locator, package, resource, delivery, or
  work-item detail.

## PostgreSQL Reader Access

When an API or MCP process wires the reader access observer, use
`eshu_dp_postgres_reader_stage_duration_seconds` to separate writer checkpoint,
reader borrow, identity, replay, business-query, and transaction-control
duration. Its only labels are `role` (`writer` or `reader`), `stage`
(`writer_checkpoint`, `reader_borrow`, `reader_identity`, `reader_replay`,
`business_query`, or `transaction_control`), and `outcome`
(`ok`, `error`, `deadline`, or `canceled`). Unexpected values collapse to
`unknown`; no endpoint, SQL, user, or credential is a label. The duration
histogram uses explicit seconds boundaries from 5 ms to 10 s, plus zero;
use stage spans for comparisons finer than the histogram buckets.
`transaction_control` times control SQL inside a guarded read transaction,
today only the status snapshot's `SET LOCAL jit = off` (#7009); it is not a
business query and is not in the per-request stage seconds.

The reader access pool callback exposes
`eshu_dp_postgres_reader_pool_connections` (`role` and `state`: `max_open`,
`open`, `in_use`), `eshu_dp_postgres_reader_pool_waits_total` (`role`), and
`eshu_dp_postgres_reader_pool_wait_duration_seconds` (`role`). The writer and
reader points are the two process pools, including when a pool has multiple
candidate hosts. A rising wait rate or in-use count near `max_open` suggests
pool pressure. When `ESHU_POSTGRES_READ_MEMBERS` is set, four additional gauges
identify each configured member by its zero-based inventory ordinal, never by
host, address, credential, or free-form ID:
`eshu_dp_postgres_reader_member_qualified` (1 if bootstrap accepted it, else 0),
`eshu_dp_postgres_reader_member_connections` (`state` = `max_open`, `open`, or
`in_use`), `eshu_dp_postgres_reader_member_reservations` (allocator slots), and
`eshu_dp_postgres_reader_member_waiters` (queued requests). A missing member has
only a zero qualification point; it has no pool or allocator point. The
allocator gauges are read under one short lock and the scrape runs no SQL.
Compare an in-use or reservation count near its member maximum with waiters
to identify the pressured member. `eshu_dp_postgres_reader_member_attempts_total`
counts each fleet attempt by original `member_ordinal` and a closed `outcome`
(`ok`, `error`, `deadline`, or `canceled`). An error rate on one ordinal beside
successful attempts on another shows failover without exposing the endpoint.
`eshu_dp_postgres_reader_snapshot_export_qualified` is `1` when the reader
build may export a shared snapshot (PostgreSQL 18.7 or later, or a build
declared exactly in `ESHU_POSTGRES_READ_SNAPSHOT_EXPORT_BUILDS`) and `0` when
every shared-snapshot read it would serve is refused (#7957). It has no label
for a single reader pool and one `member_ordinal` series per live fleet member;
it is not the bootstrap membership signal `eshu_dp_postgres_reader_member_qualified`.
The scrape reads the in-memory verdict that every reader borrow refreshes, so
it runs no SQL. `eshu_dp_postgres_reader_snapshot_export_denials_total` counts
refused shared-snapshot reads by a closed `reason`: `no_declaration` (no build
declared) or `undeclared_build` (declarations exist but none equals the running
`server_version`). A rising counter with the gauge at `0` is the permanent
`503` `snapshot_export_unqualified`; see
[PostgreSQL read routing](../../deployment/postgres-read-routing.md#shared-snapshot-export-qualification).
The `NewObserver` observer implements the optional
`ContextObserver`, so each reader `postgres.reader_access` stage span, on the
single-endpoint and member-fleet paths alike, is a child
of whatever span is active on the request context (the API server span or the
query handler span) and follows its sampling decision (#7545). The
writer-checkpoint span is a child of the API server span and stays a root on
MCP, which has no server span when the checkpoint is taken. The histogram sample
is recorded with the request context, so for a sampled request the SDK's default
trace-based exemplar filter attaches a trace exemplar; set
`OTEL_METRICS_EXEMPLAR_FILTER` to change it. An observer
without that method still receives the request-less `Observe` and its spans are
roots. These signals do not establish deployed latency or replica
capacity until API/MCP wiring and measurement are complete.

The active recording span in a request trace receives `postgres.reader_query_start` before
each guarded business query. The event has `postgres.role=reader`, a per-`Access`
`postgres.query.sequence`, and `postgres.backend.identity` (`available` or
`unavailable`). When available, it includes `postgres.backend.pid` and
`postgres.backend.remote` copied from the exact borrowed TCP connection. Snapshot
transactions retain that scalar identity for each query. These values are trace
event attributes, never metric labels. A missing or unretained event, an
unavailable identity, or a nonrecording request cannot identify the backend.
For cancellation checks, pair a retained event with the physical reader and
native `pg_stat_activity` PID plus `backend_start`. The socket peer can be a
Service address and does not identify its backing pod. Then observe the active
query and transaction leave after cancellation. The event alone proves only
that the query was about to start. It does not prove deployed cancellation.

The same histogram is the signal for reader fence outcomes (#7523); no separate
counter exists. With `role="reader"`, `stage="reader_replay"` and
`outcome="deadline"` counts replicas that missed the writer checkpoint within
the replay window (the API answers `503 backend_unavailable` with `Retry-After`),
`stage="reader_borrow"` and `outcome="deadline"` counts connection-acquisition
timeouts, a pool wait or a dial under the replay-window deadline (the same
`503`; a `reader_identity` `deadline`, the identity check timing out inside the
replay window, answers it too), and `stage="reader_replay"` with `outcome="ok"` and a
duration near the replay window counts reads that waited for replay and then
succeeded. Only the `deadline` outcomes map to the `503`. The same stages with
`outcome="error"` (authentication or TLS failure, connection refused, a role
denied `pg_control_system()`, a failing replay query) answer `500`, not `503`, and
are the operator signal for a permanent reader misconfiguration; `canceled` is a
canceled context and is never a `503`. A client disconnect answers `499` on a
route that fails through the server-failure helper, as
[Failed and canceled query reads](traces.md#failed-and-canceled-query-reads-7626)
describes (#7674); a cancel from inside the server while the
request is still live answers `500`. A `499` is not counted in
`eshu_dp_api_request_errors_total`, which counts only `5xx`.

## Runtime Health And Backlog

Use these first when asking whether a runtime is alive, ready, or stuck:

- `eshu_runtime_info`
- `eshu_runtime_status_snapshot_available` — unlabelled per-scrape gauge: `1` when the status snapshot succeeded, `0` when it failed or timed out. OTEL metrics remain available when this is `0`; status-derived gauge values are omitted.
- `eshu_dp_metrics_gather_errors_total` — unlabelled counter of series the `/metrics` scrape could not gather, one increment per failing series per scrape. A gather error drops only the failing series; the rest of the scrape is still served with `200`. Alert on `increase(...[10m]) > 0`, then read the matching `telemetry.metrics.gather_failed` error log, which names the failing instrument. Before this signal a single bad series failed the whole scrape with a bare `500` and wrote no log.
- `eshu_runtime_status_summary_stale{service_name,model_key}` — per-scrape gauge, rendered only while `ESHU_STATUS_SUMMARY_READ_ENABLED=true`: `0` when the scrape served a fresh stored active-work summary row, `1` when it served the newest row this process can decode, stale included and aged to the read, or the zero summary when it can decode none, because the current row could not be served fresh. A scrape never runs the live active-work statement. Alert on `== 1` held for several minutes; it means the reducer writer is down, slow, or on another statement version. `EshuHostedStatusSummaryStale` in the hosted ops alert pack does this.
- `eshu_runtime_status_summary_age_seconds{service_name,model_key}` — per-scrape gauge, rendered with the stale gauge: the age in seconds of the active-work summary the scrape served (database clock at the read minus the row's `as_of`), growing while the writer is down; `NaN` when the process can decode no row (the zero summary), so comparisons drop it.
- `eshu_runtime_health_state`
- `eshu_runtime_scope_active`
- `eshu_runtime_scope_changed`
- `eshu_runtime_scope_unchanged`
- `eshu_runtime_refresh_skipped_total`
- `eshu_runtime_refresh_skipped_by_reason_total{service_name,reason}` — the same skips split by the closed `reason` set (#7978): `unchanged` (hint and commit equal the active generation), `pending_same_identity` (they equal a generation still pending with live projector work; a rising rate means the projector or reducer is behind), `reconcile_reprojected` (an unchanged reconcile re-opened the active generation's projector work instead of storing a duplicate), `reconcile_reprojection_pending` (that work was already open), `reconcile_superseded` (a newer generation activated first; the snapshot is dropped), `finalized_generation_replay`, `destructive_successor_refused` (a successor that would empty or sharply shrink the scope was refused and recollected; see `eshu_dp_ingestion_successor_guard_decisions_total` for the deciding rule), and `other`. A rising `reconcile_reprojected` with flat `scope_generations` growth is the expected sweep shape.
- `eshu_runtime_retry_policy_max_attempts`
- `eshu_runtime_retry_policy_retry_delay_seconds`

Queue gauges:

- `eshu_runtime_queue_total`
- `eshu_runtime_queue_outstanding`
- `eshu_runtime_queue_pending`
- `eshu_runtime_queue_in_flight`
- `eshu_runtime_queue_retrying`
- `eshu_runtime_queue_succeeded`
- `eshu_runtime_queue_dead_letter`
- `eshu_runtime_queue_failed`
- `eshu_runtime_queue_overdue_claims`
- `eshu_runtime_queue_oldest_outstanding_age_seconds`
- `eshu_runtime_provenance_edge_identity_upgrade_applied`
- `eshu_runtime_provenance_edge_identity_upgrade_required`

Stage, generation, and domain gauges:

- `eshu_runtime_generation_total`
- `eshu_runtime_stage_items`
- `eshu_runtime_domain_outstanding`
- `eshu_runtime_domain_retrying`
- `eshu_runtime_domain_dead_letter`
- `eshu_runtime_domain_failed`
- `eshu_runtime_domain_oldest_age_seconds`

Workflow-coordinator status exports add coordinator-specific
`eshu_runtime_coordinator_*` gauges for active claims, overdue claims, oldest
pending age, collector instances, run status, work-item status, and
completeness. `eshu_runtime_coordinator_collector_terminal_failure_instances`
(labels `collector_kind`, `failure_class`) is a point-in-time gauge of
registered instances with a terminal work-item failure inside 3 x their
`scan_interval` (30 minute floor) and no later completed work item for the same
instance and scope; `failure_class` is one of the 40 bounded classes in
[Collector Terminal Failure Readiness](../http-api/collector-terminal-failure.md#values)
(the nine every kind shares, the four the extension host writes, each kind's
own, and the one `extension_defined` token for a class an extension defines on
a kind that is not first-party), or
`unknown`, so the label takes at most 41 values. A class outside the kind's own
set reads `unknown`, and no persisted text reaches the label. It is a gauge,
not a counter, so a recovered or aged-out instance
drops out. Every runtime that scrapes status exports the same value with its
own `service_name`, so read it with `max by (collector_kind, failure_class)`,
never summed.

## Data-Plane Core

Use these to locate the phase that changed before opening logs or traces:

| Metric | Use |
| --- | --- |
| `eshu_dp_queue_depth` | Queue depth by queue and status. |
| `eshu_dp_queue_oldest_age_seconds` | Oldest queued item age by queue. |
| `eshu_dp_queue_source_depth` | Queue depth by queue, source system, and status. |
| `eshu_dp_queue_source_oldest_age_seconds` | Oldest queued item age by queue and source system. |
| `eshu_dp_queue_claim_duration_seconds` | Queue claim latency. |
| `eshu_dp_queue_claim_conflict_retries_total` | Claim statements retried after a Postgres deadlock (`40P01`) or serialization failure (`40001`), by `queue` and `failure_class`. The projector claim takes every row lock with `SKIP LOCKED`, so a sustained nonzero rate is worth investigating as a possible new lock-order conflict (#7108). bootstrap-index emits it too since #7122; there the retry is bounded, and 20 consecutive conflicting claims fail the run. Wiring the queue instruments in bootstrap-index also makes `eshu_dp_projector_retry_surge_total` emit from it. |
| `eshu_dp_queue_dead_letters_total` | Work items a projector or reducer Fail path moved to `dead_letter` (#7386), by `queue` (`projector` or `reducer`) and `failure_class`. Counted once, after the dead-letter UPDATE reports exactly one affected row, so a claim the lease fence rejected is not counted. The class is the stored `failure_class`; a value that is not a lowercase identifier of at most 64 characters is labeled `other`. Operator dead-letters through the admin API are not counted. The stored row keeps the full class, message and details; use the counter to see how many and which class, then read the row. |
| `eshu_dp_projector_scopes_multiple_live_leases` | Projector scopes that hold more than one unexpired `claimed` or `running` lease. No labels. The projector claim's scope fence keeps this at zero (#7115); any other value means two workers are projecting one scope and is worth an alert. Served by the reducer from its Postgres gauge snapshot (refresh cadence `ESHU_POSTGRES_GAUGE_REFRESH_INTERVAL`), not computed on scrape; reports nothing until the first refresh. |
| `eshu_dp_projector_scopes_missing_claim_fence` | Projector scopes with claimable work but no `projector_scope_claim_fences` row. No labels. The claim joins that row, so these scopes cannot be projected until it exists; the `ingestion_scopes` insert trigger and migration 130 keep this at zero (#7115). Any other value is a silent stall worth an alert. Served by the reducer from its Postgres gauge snapshot (refresh cadence `ESHU_POSTGRES_GAUGE_REFRESH_INTERVAL`), not computed on scrape; reports nothing until the first refresh. |
| `eshu_dp_projector_marked_write_oldest_age_seconds` | Age in seconds of the oldest set `projection_write_started_at` marker on a non-terminal generation with open projector work. No labels. A hung graph write with a live heartbeat keeps its marker and its claimed row, so the age grows without bound; zero means no marked write is outstanding (#7471). A value climbing past the `ESHU_CANONICAL_WRITE_TIMEOUT` budget means a marked write is stuck and is worth an alert. Served by the reducer from its Postgres gauge snapshot (refresh cadence `ESHU_POSTGRES_GAUGE_REFRESH_INTERVAL`), not computed on scrape; reports nothing until the first refresh. |
| `eshu_dp_corpus_fence_holders` | Active scopes holding the global relationship completeness fence that defers the correlation consumers (#8247): a retired-or-pending current relationship generation, or none yet with live `deployment_mapping` work. No labels. Zero means the fence is open. The count evaluates the fence's own predicate, so it cannot disagree with the gate. Served by the reducer from its Postgres gauge snapshot (gauge `reducer_corpus_fence`, refresh cadence `ESHU_POSTGRES_GAUGE_REFRESH_INTERVAL`), not computed on scrape or on the claim path; reports nothing until the first refresh. |
| `eshu_dp_corpus_fence_deferred_consumers` | Correlation consumers currently deferred on relationship-resolution readiness (#8247), by closed `domain` (`deployable_unit_correlation`, `workload_materialization`, `service_catalog_correlation`): `retrying` rows in the consumers' non-counting resolution-not-ready classes, excluding stale-generation rows. Includes consumers waiting on their own relationship generation, not only the global fence. Absent for a domain with no deferred consumer. Same snapshot serving as `eshu_dp_corpus_fence_holders`. |
| `eshu_dp_corpus_fence_oldest_deferred_age_seconds` | Age in seconds of the oldest consumer deferred on relationship-resolution readiness (#8247), by closed `domain`; includes consumers waiting on their own relationship generation, not only the global fence. The deferral is non-counting and never dead-letters, so this age is the signal that a consumer has been parked for hours or days while the fence stays closed; a steadily climbing value with `eshu_dp_corpus_fence_holders` above zero means re-resolutions are overlapping continuously. Absent for a domain with no deferred consumer. Same snapshot serving as `eshu_dp_corpus_fence_holders`. |
| `eshu_dp_worker_pool_active` | Active worker count by pool. |
| `eshu_dp_collector_observe_duration_seconds` | Collector observe cycle cost. |
| `eshu_dp_scope_assign_duration_seconds` | Repository or source scope assignment cost. |
| `eshu_dp_fact_emit_duration_seconds` | Fact emission cost. |
| `eshu_dp_facts_emitted_total` | Collector fact output volume. |
| `eshu_dp_facts_committed_total` | Durable fact commit volume. |
| `eshu_dp_generation_fact_count` | Fact volume per scope generation. |
| `eshu_dp_collector_delta_baseline_fallback_total` | Git delta syncs that fell back to a full snapshot, by `skip_reason` (`no_projected_baseline`, `no_projected_full`, `baseline_unreachable`, `delta_exceeds_base_half`, `baseline_lookup_error`, `default_branch_changed`). |
| `eshu_dp_collector_delta_touched_ratio` | Cumulative git delta touched paths over the union base's tracked files per delta sync (#8780), by `outcome` (`delta`, `delta_exceeds_base_half`). |
| `eshu_dp_collector_repository_selection_evaluations_total` | Repository selection evaluations on shard 0 (#7625), by `collector_kind`, `selector_kind` (`github_org`, `explicit`, `gitlab_group`), and `outcome` (`evaluated`, `listing_truncated`, `guard_tripped`, `store_error`). |
| `eshu_dp_collector_repository_selection_scopes` | Gauge of the evaluating selector's known repository scopes by selection `state` (`selected`, `not_listed_pending`, `not_listed`, `archived_excluded`, `rule_excluded`) after the last `evaluated` cycle (#7625, #7765). |
| `eshu_dp_collector_repository_listing_requests_total` | GitLab group listing requests (#7765), by `collector_kind`, `provider` (`gitlab`), and `outcome` (`success`, `retried`, `rate_limited`, `http_error`, `transport_error`, `decode_error`). |
| `eshu_dp_collector_repository_selection_observations_deleted_total` | Expired `repository_selection_observations` rows the git collector's sweep deleted, by `collector_kind`; a row is deleted only after its liveness window plus a 7-day grace, and a `not_listed` row only once its selector's cycle marker is older than that too (#7774, #7840). |
| `eshu_dp_collector_repository_selection_not_listed_reclaimed_total` | Abandoned selectors' `not_listed` rows reclaimed by the sweep, by `collector_kind`; a subset of the deleted total above (#7840). |
| `eshu_dp_collector_repository_selection_probe_total` | Upstream classification probe answers (#8454), by `collector_kind` and `outcome` (`transferred`, `renamed`, `still_present`, `not_found_or_inaccessible`, `kept_prior` for an inconclusive probe that keeps the prior verdict). |
| `eshu_dp_collector_repository_selection_classifications` | Gauge of the probing selector's `Confirmed` `not_listed` scopes by `classification` (`transferred`, `renamed`, `still_present`, `not_found_or_inaccessible`, `unclassified`) after the last `evaluated` cycle that probed (#8454). |
| `eshu_dp_git_repo_sync_failures_total` | Per-repository git sync operations (`clone`, `fetch`, `list_refs`) that failed and were isolated to that one repository for the cycle rather than aborting the rest of the fleet, by bounded `operation` (issue #7001). A `list_refs` spike points at remote/DNS trouble for the affected repos; a rate near the fleet size points at an auth or network outage. |
| `eshu_dp_git_command_deadline_exceeded_total` | Git commands the sync loop killed at their per-operation deadline (`ESHU_GIT_RESOLVE_TIMEOUT_SECONDS` 60 s, `ESHU_GIT_FETCH_TIMEOUT_SECONDS` 90 s, `ESHU_GIT_CLONE_TIMEOUT_SECONDS` 600 s), by the closed `operation` set `clone`, `fetch`, `ls_remote`, `resolve` (issue #8052). A rate on a few repositories points at name resolution or remote trouble; each one cost the fleet sync at most its own deadline instead of libcurl's 300 s. One increment per killed attempt: a kill is one attempt of the bounded retry (issue #8091), so a repository that recovered on its retry counts a kill and has no sync-failed line. |
| `eshu_dp_git_command_attempts_total` | Every attempt of a network git command the sync loop ran, including the first (issue #8091), by the closed `operation` set `clone`, `fetch`, `ls_remote`, `resolve`, the provider `host` (`github.com`, `bitbucket.org`, the configured GitLab host, or `unknown`), the closed `failure_class` set, which is the contract: `none` on success; the seven retryable classes `deadline_exceeded`, `dns_resolve_failed`, `connect_failed`, `connection_reset`, `http2_framing`, `tls_handshake`, `low_speed_abort`; the five non-retryable classes `auth_failed`, `http_4xx`, `repository_not_found`, `rate_limited`, `pack_object_error`; and the fallback `git_sync_failure` for any text the classifier does not name (HTTP 5xx is in the fallback in this version), and `outcome` (`success`, `retried`, `failed`). One increment per attempt; a shutdown cancel is never counted. Sum `outcome="retried"` to see how often a connectivity failure was absorbed, and `outcome="failed"` by `failure_class` for failures that stayed final. |
| `eshu_dp_git_command_retries_total` | Retries the loop started after a retryable connectivity failure (issue #8091), by `operation`, `host`, and the `failure_class` that caused the retry. It is counted when the backoff ends and the next attempt starts, so a retry cancelled during its backoff counts in `eshu_dp_git_command_attempts_total{outcome="retried"}` but not here. A rate on one `host` points at that provider's network path; a rate across all hosts points at the collector's own egress or resolver. `ESHU_GIT_RETRY_ATTEMPTS=1` turns it off. |
| `eshu_dp_github_api_attempts_total` | Every attempt of a GitHub REST call that repository discovery makes, including the first (issue #8084): the org repository listing page (`operation="list"`) and the GitHub App installation token mint (`operation="mint"`). `failure_class` is `none` on success; otherwise `server_error` (HTTP 5xx), `rate_limited` (429, or 403 with a `retry-after` header or `x-ratelimit-remaining` 0), `client_error` (any other 4xx or 3xx), `decode` (invalid JSON on a fully read body), or a transport class (`reset`, `refused`, `timeout`, `unreachable`, `eof`, `dns`, `other`; an HTTP/2 stream reset is `reset`). `outcome` is `success`, `retried`, or `failed`. A `failed` count with `server_error` or `rate_limited` is one discovery call that ran out its budget; the collector then skips the cycle. |
| `eshu_dp_github_api_retries_total` | Retries of a GitHub REST call that repository discovery started after a retryable failure (issue #8084), by `operation` (`list`, `mint`) and the `failure_class` that caused the retry. It is counted when the wait ends and the next attempt starts, so a retry cancelled during its wait counts in `eshu_dp_github_api_attempts_total{outcome="retried"}` but not here. The budget is 4 attempts per call with 1 s, 2 s, 4 s waits spread over 75% to 125%; a `retry-after` or `x-ratelimit-reset` wait of at most 60 s replaces the backoff, and a longer one ends the call. A rate across all operations points at the collector's egress path; one on `mint` alone points at the App installation. |
| `eshu_dp_collector_discovery_skipped_cycles_total` | Discovery cycles the collector loop skipped after a retryable source failure instead of exiting the process (issue #8084), by `collector_kind`, `operation` (`list` for the org listing, `mint` for the GitHub App token mint), and the bounded `failure_class` of the failure (`server_error`, `rate_limited`, or a transport class `reset`, `refused`, `timeout`, `unreachable`, `eof`, `dns`). One increment per skipped cycle; the loop waits the error's `RetryAfter`, or the poll interval when it names none. A steady rate means the provider or the egress path is down, and `eshu_dp_collector_discovery_consecutive_failures` shows how close the process is to its crash-loop cap. |
| `eshu_dp_collector_discovery_consecutive_failures` | Discovery cycles in a row that ended in a retryable failure (issue #8084), by `collector_kind` and `operation` (`list` for the org listing, `mint` for the GitHub App token mint), as the git selectors count them. It rises by one per failing cycle of that operation. A native selection cycle that succeeds returns both operations to 0; a webhook sync of a non-empty repository set returns only `mint` to 0 and never `list`. At 20 for one operation the next failure is fatal and the process exits with an error that names the class and the count, so alert well below 20. |
| `eshu_dp_git_command_retry_budget_exhausted_total` | Retryable failures that were not retried because the per-sweep retry budget (`ESHU_GIT_RETRY_SWEEP_BUDGET`, default 20) was spent (issue #8091), by the closed `scope` set `sweep`. Each one is final on its first attempt, exactly as before the retry existed, and its `git command attempt failed` log line carries `retry_budget_exhausted=true`. A non-zero rate during an outage is the cap working; a non-zero rate on a healthy fleet means the budget is too small for the normal failure rate. |
| `eshu_dp_git_command_attempt_duration_seconds` | Wall time of each network git command attempt in seconds (issue #8091), by `operation` and `outcome` (`success`, `retried`, `failed`). Attempts that die at their deadline land at the deadline value (60, 90, or 600 s by default); compare `retried` with `success` to see what a recovered attempt costs the sweep. |
| `eshu_dp_git_resolve_cache_lookups_total` | Go-resolver lookups of a git provider host for the per-host address cache the sync passes to git as `http.curloptResolve` (issue #8092), by provider `host` and `result` (`success`, `error`, `empty`). One receiver goroutine runs every lookup, single-flight per host. A steady `error` rate on one host points at the resolver path to that provider; `empty` means the resolver answered with no address and the last good set was kept. |
| `eshu_dp_git_resolve_cache_refresh_requests_total` | Address-refresh requests to the cache (issue #8092), by `host` and `reason`: `initial` (the first command for a host), `failure` (a retry whose attempt died at its deadline or with one of the closed curl wording rows — could not resolve host, resolving timed out, failed to connect, connection timed out, connection reset, Operation timed out after), `periodic` (the hourly background refresh), and `suppressed` (a request inside the 30 s per-host minimum gap that started no lookup). HTTP status, auth, and pack answers never request a refresh. |
| `eshu_dp_git_resolve_cache_fallback_total` | Network git commands that ran without a `curloptResolve` pin (issue #8092), by `host` and `reason`: `cache_empty` (no address cached yet — the first command waited at most `ESHU_GIT_RESOLVE_CACHE_LOOKUP_SECONDS`, default 5 s, then ran as before the cache existed) and `proxy_set` (an HTTP_PROXY, HTTPS_PROXY, or ALL_PROXY setting means curl resolves the proxy's name, not the remote's). `ESHU_GIT_RESOLVE_CACHE=off` and SSH remotes pin nothing and count nothing here. |
| `eshu_dp_git_resolve_cache_lookup_duration_seconds` | Wall time of each address-cache lookup in seconds (issue #8092), by `host` and `result`. Lookups that run out their budget land at `ESHU_GIT_RESOLVE_CACHE_LOOKUP_SECONDS` (default 5 s); a p99 near it means first commands for that host fall back unpinned. |
| `eshu_dp_collector_reconciliation_full_snapshots_total` | Git scopes forced to a full reconciliation snapshot to retract delta-path drift, by `reason` (including `graph_dirty`, #7389, and `reindex_requested` and `repository_reindex_requested`, #7620). |
| `eshu_dp_collector_reconciliation_suppressed_total` | Reconciliation sweep evaluations held off because a full generation is in flight, is pending past the interval with live work at the remote head (`in_flight_expired_held`), or is in retry backoff, by `reason`. |
| `eshu_dp_collector_reconcile_parse_skip_decisions_total` | Pre-parse skip decisions of forced git reconciles by `outcome` (`skipped`, or why the repository was parsed) (#7978). |
| `eshu_dp_collector_preparse_identity_dropped_total` | Git snapshots and fact streams that kept the pre-parse identity off a generation by `reason` (#7978). |
| `eshu_dp_reconciliation_drift_retractions_total` | Graph nodes and edges actually retracted while applying a forced reconciliation snapshot or a re-projection of an already-active full generation (#7978 reconcile, #7209 heal, liveness re-drive, rebuild or refinalize), by bounded `domain`, `write_phase`, and `kind`. |
| `eshu_dp_workload_repository_edge_retractions_total` | Stale `DEFINES` and repository-side `EXPOSES_ENDPOINT` edges `workload_materialization` retracted on full generations (#7285), by bounded `write_phase` (`defines_retract`, `repository_endpoint_retract`). Ordinary workload or endpoint removal, not collector drift. |
| `eshu_dp_reconciliation_convergence_total` | Denormalized graph edges classified by the dual-write reconciliation pass, by bounded `domain` and `drift_kind` (`in_sync` / `stale_generation` / `orphan_resolved_id`). Non-`in_sync` values are stranded edges whose denormalized `generation_id`/`resolved_id` no longer match the authoritative Postgres generation after a swap; a sustained nonzero `stale_generation` or `orphan_resolved_id` rate means a Postgres↔graph partial failure left inconsistent edges that the pass is retracting to converge. |
| `eshu_dp_projector_run_duration_seconds` | Projector claim-and-project cycle cost. |
| `eshu_dp_projector_stage_duration_seconds` | Projector substage duration. |
| `eshu_dp_projector_ack_deferrals_total` | Projector Acks deferred because a same-scope ingestion commit held the scope row, by `outcome`: `retried`, `abandoned` (the 150-retry bound ran out), or `shutdown`. `retried` is counted before the lease renewal; a renewal that then ends the wait shows its result only in `eshu_dp_projector_ack_wait_seconds`. A rising `abandoned` rate means work is being dropped for re-projection after its lease expires. |
| `eshu_dp_component_producer_grant_decisions_total` | Producer-grant allow/deny decisions for core-owned fact kinds, by `decision` (`allow`/`deny`), `stage` (`install`/`readback`/`activation`/`emission`), closed `reason` (`granted` or a deny reason), and core `fact_kind`. Producer id is span/log only. Emitted by `collector-component-extension` (readback, activation, emission) and `workflow-coordinator` (readback, activation); the CLI, API, and MCP server emit none, so separate the two emitters by `service_name`. See [Producer-Grant Decisions](producer-grant-decisions.md). |
| `eshu_dp_component_extension_results_total` | Component-extension claim results by `collector_kind`, `state` (`complete`, `unchanged`, `partial`, `retryable`, `terminal`), and `failure_class`, including host verdicts (`invalid_claim`, `launch_failure`, `invalid_result`, `identity_mismatch`, `status_record`). `failure_class` is `none` without a failure and folds to `other` past 64 distinct values per process; the cap is per process and, because failure classes are collector-defined, which classes fold depends on arrival order. |
| `eshu_dp_component_extension_status_records_total` | Validated SDK status rows by `collector_kind`, `state`, `status_class`, and `failure_class`. |
| `eshu_dp_component_extension_reported_facts_total` / `eshu_dp_component_extension_reported_warnings_total` | The `fact_count` and `warning_count` an extension reported in its status rows, by `collector_kind` and `state`. Committed facts are `eshu_dp_workflow_claim_facts_emitted_total`. |
| `eshu_dp_component_extension_source_latency_seconds` | Reported `source_latency_ms` from status rows that carry it, by `collector_kind` and `state`. |
| `eshu_dp_component_extension_retry_after_seconds` | Gauge: the retry delay the host honored on the latest result for the `collector_kind`, after the one-hour cap; `0` when that result was not retryable. Pair with `eshu_dp_workflow_claim_provider_throttle_total{outcome="retry_after_honored"}`. For extension collectors that counter counts every retryable result that asked the host to back off (`retry_after_seconds > 0`, which the SDK requires on every retryable result), not only HTTP rate limiting. |
| `eshu_dp_projector_ack_wait_seconds` | Time a deferred projector Ack waited for a busy scope, by terminal `outcome`: `succeeded`, `abandoned`, `shutdown`, `superseded`, `claim_lost`, or `failed`. Acks that never waited record nothing. |
| `eshu_dp_projector_write_marker_deferrals_total` | Projector write-marker attempts deferred because a busy generation row held the lock or a busy claim fence owned the scope (#7819), by `outcome`: `retried`, `gave_up` (the 150-retry bound ran out), or `shutdown`. Deferrals are paced 10 ms doubling to 200 ms (#7907). A rising `gave_up` rate means rows stay busy for the whole bound and items go through the queue's Fail path for a later retry. The deferral WARN log carries `deferral_cause` (`generation_row` or `fence_busy`). |
| `eshu_dp_projector_write_marker_wait_seconds` | Time a deferred projector write marker waited on a busy generation row or claim fence, by terminal `outcome`: `written`, `gave_up`, `shutdown`, `superseded`, `claim_lost`, or `failed`. Markers that never waited record nothing. |
| `eshu_dp_superseded_generation_fence_total` | Projector work stopped because its scope generation is superseded (#7130), by `failure_class`. `projector_heartbeat_generation_superseded`: Heartbeat stopped running work whose own generation a newer Ack retired, usually a worker whose lease had expired; the projection is cancelled at the first heartbeat whose supersede check gets the scope row, which is one heartbeat interval plus any interval in which ingestion, Ack or Fail holds that row. `projector_ack_generation_superseded`: Ack refused to re-activate the generation and marked the work item superseded, so superseded-generation work reached a worker and finished (for example a claim that raced a supersede). `projector_replay_generation_superseded`: a replay or dead-letter drain left such rows terminal, counted per replay call (a repeated drain counts the same rows again), or the admin replay refused explicit ids naming them. The claim itself sweeps a superseded generation's claimable rows without counting here; those rows carry `failure_class = projector_superseded_by_newer_generation` and `failure_details.generation_status = superseded`. Every supersede of a work row keeps the class and message as the supersede marker and folds the old failure into `failure_details.prior_failure` (`status`, `failure_class`, `failure_message`, `failure_details` as a JSON string, `updated_at`; `updated_at` is the old row's own `updated_at`, which is the failure time for a failed or dead_letter row but the claim or heartbeat time for a claimed or running row carrying a retry's failure; present only when the old row was failed or dead_letter or carried a non-blank failure field, #7320). Read it with `SELECT work_item_id, CASE WHEN failure_details IS JSON OBJECT THEN failure_details::jsonb -> 'prior_failure' END AS prior_failure FROM fact_work_items WHERE status = 'superseded'` (the `CASE` guard keeps a row with non-JSON details from aborting the cast; PostgreSQL 16 or newer for `IS JSON`). Rows superseded before #7320 lost that evidence and cannot be recovered. A nonzero heartbeat or Ack rate is worth tracing to the claim or replay that produced the work. To see the failure a fenced row carried before the supersede, read `latest_failure.prior_failure` on `GET /api/v0/freshness/generations` or `eshu freshness generations` (#7385). |
| `eshu_dp_projector_zombie_heal_total` | Active-generation heal attempts after a zombie refusal (#7209), by `outcome`: `healed` (the scope's current active projector row was re-opened to pending so the successor re-projects over retract damage the refused zombie may have done), `skipped_in_flight` (a live claim owns the active row; the heal must not clobber it, so a live worker that already wrote before the damage stays a residual gap), `skipped_already_open` (the active row is already pending or retrying, normally a repeated refusal), `skipped_no_write_marker` (the refused generation never marked, so it provably never wrote), `skipped_no_active_generation` (no other active generation exists), `error` (the heal statement failed; the active generation may be missing nodes). Counted once per Ack or already-superseded Heartbeat refusal; the Heartbeat replaced-by-newer path never wrote and is not counted here. Each heal logs INFO `projector zombie refusal healed the active generation` with `scope_id`, `refused_generation_id` and `healed_generation_id`; skips log DEBUG and errors log ERROR. |
| `eshu_dp_recovery_delta_active_scopes_total` | Scopes an operator refinalize (`POST /api/v0/admin/recover-generations`, `POST /api/v0/admin/refinalize`, runtime `/admin/refinalize`) re-enqueued through a delta generation (#7797), by `outcome`: `reindex_requested` (git default-branch scope; a per-repository reindex watermark was recorded in the same transaction) or `reindex_unsupported` (git ref scope or another collector's scope). A delta restores only its changed files onto a wiped graph, so a non-zero count means those scopes stay incomplete until a full generation activates. Never carries scope ids. The matching logs carry the scope ids: the first 10 scopes at WARN, the rest at INFO, each with `scope_id`, `generation_id`, `outcome`, then one summary WARN, "refinalize re-projected delta generations; graph incomplete for these scopes until a full generation activates; see the response's delta_active_scopes for samples", with the keys `delta_active_total`, `reindex_requested`, `reindex_unsupported`, `per_scope_warn_limit` (10), `per_scope_info_count`. |
| `eshu_dp_projector_delta_baseline_fence_total` | Delta-baseline fence decisions (#7319), by `phase` (`preflight`, `ack`) and `outcome`: `matched`, `unfenced` (a delta with no recorded baseline), `already_active`, `refused_active_differs`, `refused_no_active`. Passes count once, at Ack; preflight counts only refusals; full generations and an already-superseded target (`target_superseded`) are not counted -- a target superseded by a newer activation is routine projector lag, owned by `eshu_dp_superseded_generation_fence_total` instead. Separate from that counter otherwise: a baseline refusal is expected under projector lag. Since #7389 a `phase=preflight` `refused_active_differs` is expected when a newer generation arrives while an older one is writing (the writer runs to Ack); its rate should not exceed the scope's generation commit rate, and a share beyond that means the projector lags the collector; any `phase=ack` refusal (`refused_active_differs` or `refused_no_active`) means two claims were valid in one scope (logged at ERROR, `failure_class = projector_delta_baseline_mismatch_after_projection`). See [Reconciliation Sweep](../reconciliation-sweep.md#delta-baseline-fence). |
| `eshu_dp_search_document_generation_superseded_total` | Search-document projections the reducer abandoned because their generation was superseded mid-write (#7458), by `phase`: `page` (the check before a page write saw the newer active generation) or `finalize` (the check before the authoritative retire did). The handler checks freshness before every page and once before `Finalize`, then returns a `superseded` result without `Cancel` or `Finalize`; the rows already written stay for retention because every reader joins the active generation. Expect a rate no higher than the scope generation commit rate. The same events also count as `status=superseded` on `eshu_dp_reducer_executions_total{domain="eshu_search_document"}` and log INFO `eshu search document projection abandoned: generation superseded` with `pages_written` and `documents_written`; abandoned pages are not counted in `eshu_dp_canonical_writes_total`. A freshness lookup error is not counted here: it fails the item, which the queue retries only for `GenerationNotYetActiveError` and otherwise dead-letters. |
| `eshu_dp_repo_dependency_replay_skipped_total` | Workload-materialization replay requests the repo-dependency runner skipped because their scope generation is no longer the scope's active generation (#7670), by `reason`: `inactive_generation`. recover-generations retires a generation without rewriting its acceptance row, so the lane can owe a replay for a retired generation; the request counts here, logs WARN `repo dependency workload materialization replay skipped: nothing to replay` (`scope_id`, `generation_id`, `entity_key`, `fenced`, `reason`), and does not quarantine the lane. It covers the plain replay and the RUNS_ON fenced replay and counts once per scope, generation and entity per cycle even when a RUNS_ON row produces both requests; the WARN's `fenced` field says which path counted it. A retired generation is also not waited on for RUNS_ON readiness. A request on the active generation that cannot be scheduled is never counted here: it fails the cycle and counts on `eshu_dp_shared_projection_lease_quarantines_total`. A freshness lookup error fails the cycle instead of being skipped. |
| `eshu_dp_repo_dependency_generation_anomalies_total` | Repo-dependency generation states an operator should see (#7670), by `reason`: `superseded_item_on_active_generation` (a plain or RUNS_ON fenced replay on the scope's active generation found its stable `workload_materialization` item terminally `superseded`, which the queue never revives; the cycle fails closed, the lease quarantines and the intents stay pending, so any nonzero value needs an operator) `unscheduled_fenced_replay_on_active_generation` (a RUNS_ON fenced readiness replay on the active generation could not be scheduled for any other reason (a superseded item counts as `superseded_item_on_active_generation`); the cycle fails closed with `workload materialization fenced replay was not scheduled` and quarantines the lease), or `inactive_accepted_generation` (counted per row: active rows about to be written whose accepted generation is no longer the scope's active generation; they still project, the counter only exposes the stale acceptance). Each logs WARN `repo dependency generation anomaly` with `scope_id`, `generation_id`, `entity_key`, `rows`, `reason`, once per scope generation per cycle for the inactive case. |
| `eshu_dp_projections_completed_total` | Projection completion volume. |
| `eshu_dp_reducer_admission_deferrals_total` | Ingester source-local reducer intent admission deferrals by bounded reason. |
| `eshu_dp_reducer_readiness_waits_total` | Reducer cross-scope readiness-wait evaluations with missing endpoints, by bounded `domain` and `outcome` (`deferred` / `abandoned` / `settled_missing`). Ready edges commit before any `deferred`; `abandoned` fires once per settled missing set. |
| `eshu_dp_reducer_run_duration_seconds` | Reducer handler execution window. |
| `eshu_dp_search_index_mutations_total` | Persisted search index document and term mutations by bounded reducer domain, kind, operation, and result. |
| `eshu_dp_search_index_errors_total` | Persisted search index write failures by bounded reducer domain and operation. |
| `eshu_dp_search_index_write_duration_seconds` | Persisted search index write duration by bounded reducer domain, operation, and result. |
| `eshu_dp_generation_retention_generations_pruned_total` | Superseded generation cleanup volume. |
| `eshu_dp_generation_retention_rows_pruned_total` | Generation-retention row cleanup volume by bounded table/data-class label. |
| `eshu_dp_generation_retention_failures_total` | Generation-retention cleanup failures by bounded reason. |
| `eshu_dp_generation_retention_skipped_total` | Generation-retention candidate skips by bounded reason. |
| `eshu_dp_generation_retention_duration_seconds` | Generation-retention cleanup transaction cost. |
| `eshu_dp_generation_retention_batch_size` | Generation-retention batch size selected for one cleanup transaction. |
| `eshu_dp_generation_retention_phase_duration_seconds` | Generation-retention transaction time by bounded phase. |
| `eshu_dp_generation_retention_search_index_batches` | Search index pre-delete batch transactions one generation-retention pass committed before its prune transaction (#7427). |
| `eshu_dp_search_index_orphan_generations` | Gauge, by `table` (`eshu_search_index_terms`, `eshu_search_index_documents`): the `(scope_id, generation_id)` pairs in that search index table whose `scope_generations` row is gone, at the last orphan census (#7427 PR2). Each reducer replica's retention runner takes the census at most once per poll interval, after its pass. The documents value reads 0 while its generation foreign key exists; the terms key was dropped by migration 191 (#7427 PR3), so a terms value above 0 is a late writer's rows for a pruned generation, which the sweep removes in the same run. A sweep lists at most 1,000 orphan pairs per relation, so a larger backlog drains over several runs and the gauge can stay above 0 while it falls. A value that does not fall across runs means the sweep cannot drain them: check `unswept_generations` (listed orphans not drained plus those past the list limit) on the `generation retention search index orphan sweep completed` log and the per-generation `generation retention search index orphan generation swept` outcome; `outcome=ineligible` with a missing generation row means a dangling active pointer still names it, so readers see those rows and the sweep keeps them. Other orphan rows are read-invisible, because every reader joins the scope's active generation. |
| `eshu_dp_search_index_orphan_rows_swept_total` | Rows the orphan sweep deleted from a search index table, by `table`, in committed 20,000-row batches outside any retention transaction (#7427 PR2). |
| `eshu_dp_search_index_superseded_rows_swept_total` | Rows the superseded sweep deleted from a search index table, by `table` (`eshu_search_index_terms`, `eshu_search_index_documents`, `eshu_search_index_stats`), in committed 20,000-row batches outside any retention transaction (#8231). The sweep runs only when `ESHU_GENERATION_RETENTION_SEARCH_INDEX_SUPERSEDED_SWEEP_ENABLED=true` (off by default); enablement stays held until the issue's gate-(b) primary dead-tuple/bloat evidence lands. `rows_swept` not rising across runs while the sweep still lists candidates (persistent `unswept_generations` on the `generation retention search index superseded sweep completed` log) means the sweep cannot drain them: check the per-generation `generation retention search index superseded generation swept` outcome. |
| `eshu_dp_generation_retention_scope_lock_hold_seconds` | How long one generation-retention transaction held its re-locked scope rows, from the targeted re-lock of the selected set to the commit (selection and counts run unlocked since #7334). |
| `eshu_dp_active_generations` | Current active scope generation count by closed activation-age bucket `age_bucket` (`fresh`, `aging`, `draining`, `stuck`); the `stuck` bucket is the operator alarm signal, and `draining` counts blocked generations whose shared-intent domain queues are still progressing. |
| `eshu_dp_generation_liveness_recovered_total` | Wedged active generations re-driven through projector re-enqueue by the liveness sweep. |
| `eshu_dp_generation_liveness_superseded_total` | Orphaned older active generations superseded by the liveness sweep. |
| `eshu_dp_generation_liveness_failures_total` | Generation liveness recovery sweep failures by bounded reason. |
| `eshu_dp_graph_orphan_nodes` | Bounded zero-relationship graph node count by closed `node_label`, served from a background snapshot. |
| `eshu_dp_gauge_snapshot_refreshes_total` | Background graph-gauge snapshot refreshes by `gauge` and `outcome` (`success`, `error`, `timeout`). |
| `eshu_dp_gauge_snapshot_refresh_duration_seconds` | Duration of each background graph-gauge snapshot refresh by `gauge` and `outcome`. |
| `eshu_dp_gauge_snapshot_age_seconds` | Age of the snapshot each graph-backed gauge is serving. |
| `eshu_dp_canonical_write_duration_seconds` | Canonical graph/content write latency. |
| `eshu_dp_search_decay_policy_applications_total` | Search decay scoring decisions by policy id, evidence class, and outcome. |
| `eshu_dp_query_scoped_grant_denied_total` | Scoped-caller query reads decided closed in Go rather than in a Cypher predicate (#6786), by bounded `operation` and `reason` (`grant_denied` = the Go grant check admitted no candidate, counted once per request; scoped name lookups filter by grant in Cypher first, so there it counts only rows the backend should have excluded; `backend_anchor_mismatch` = a returned row did not match the request, a graph-backend regression signal that should page). |
| `eshu_dp_entity_context_resolution_total` | `GET /api/v0/entities/{entity_id}/context` requests that reached the answer decision (the graph returned a row, the content store answered, or both missed and the request answered a true 404), by `resolved_by` (#7212): `anchor` = an anchor statement, any statement that is not the unlabeled fallback, returned the row (the Neo4j indexed `CALL ()` anchor, which is the only Neo4j statement, or a labeled statement of the NornicDB per-label loop); `fallback` = the final unlabeled `MATCH (e)` statement of the NornicDB per-label loop returned the row (Neo4j sends only the schema-closed anchor since #7212, so it never records `fallback`); `content` = the graph returned no row and the content store answered; `none` = nothing answered, 404. `none / sum(rate(...))` over a window is the true-miss share. A request that ends in an error (graph read error, deadline, 5xx) or is rejected before the graph read (400, empty scoped grant) is not counted. The request span carries `eshu.entity_context.resolved_by` and `eshu.entity_context.statements_tried`; see the traces reference for when each is present. |
| `eshu_dp_language_query_metadata_rows_total` | Graph rows `POST /api/v0/code/language-query` sent to content-metadata enrichment (#7247), by `metadata_path` and `outcome`. `metadata_path=anchored` rows were looked up by their own `entity_id` (one bounded primary-key read, for labels whose graph id is the content primary key); `metadata_path=page_read` rows went through the filtered content page merged by path, type, name and start line. `outcome=enriched` rows merged content metadata; `outcome=missing` rows got none. On `anchored` a row is `missing` when there is no content row for its id, the content row's metadata holds nothing beyond fingerprint keys (for example a Go struct with no docstring), or the content row is in a different repository. On `page_read` a row is `missing` when no page row has its repository, path, label, name and start line, or the matched row's metadata holds nothing beyond fingerprint keys. `missing` alone does not separate those causes, so it is not by itself a sign of projection lag. A request with no content store, an unmapped label, or no graph rows records nothing. The request span carries the same counts as `eshu.language_query.metadata_path`, `eshu.language_query.metadata_rows_enriched` and `eshu.language_query.metadata_rows_missing`. |
| `eshu_dp_infra_search_path_total` | `POST /api/v0/infra/resources/search` graph reads by the statement that served them (#7245; the Argo CD category-only read is not counted): `search_path=fulltext` (the `infra_search_key_ft` index read) or `legacy` (the per-label scan), and `reason`. `fulltext` carries `ready`, or `ft_error` when the request was canceled during the index read (no fallback runs then). `legacy` carries why: `flag_off` (`ESHU_INFRA_SEARCH_KEY_READ` is off), `not_ready` (no readiness probe yet, or the probe failed), `index_not_online`, `unkeyed` (some infra node holds a searched field but has no search key flag), `backend` (not Neo4j), `no_query` (structured search), `query_too_long` (more than 256 code points), or `ft_error` (the index read failed and the legacy statement answered that request). A rising `legacy/ft_error` rate with the flag on is the signal to page on. |
| `eshu_dp_infra_search_key_ready` | 1 when the last infra search readiness probe (API and MCP server, at start and every 5 minutes, Neo4j only) read `infra_search_key_ft` ONLINE and found no unkeyed infra node that holds a searched field (`search.CountUnkeyedSearchable`), else 0 (#7245). Turn `ESHU_INFRA_SEARCH_KEY_READ` on only while this reads 1. |
| `eshu_dp_infra_search_key_index_online` | 1 when the last readiness probe read `infra_search_key_ft` as ONLINE, else 0: missing, POPULATING, FAILED, or the probe failed (#7245). A FAILED index needs the runbook in `go/internal/graph/README.md` ("Infra search key index runbook"). |
| `eshu_dp_infra_search_key_unkeyed_nodes` | Infra search label nodes with no `infra_search_ovf` flag that hold at least one of the twelve searched fields, at the last successful readiness probe (#7245): nodes a search could match but the full-text index cannot find. Flagless nodes with no searched field (posture and `base_refs` writes) are not counted. Non-zero keeps every search on the legacy statement; rerun the bootstrap backfill. |
| `eshu_dp_reducer_own_gate_wake_total` | Own-gate consumer wake statements (#7597), by `site` (`activation`, `fail`) and `outcome` (`reopened`, `noop`, `error`); see [Reducer And Storage Metrics](metrics-reducer-storage.md). |
| `eshu_dp_config_artifact_candidates_total` | Config-artifact candidates of the deployment-artifact load behind the repository story, repository context and service story or context routes (#7951), by `outcome`: `hydrated` (read or blank-checked under the per-repository floor of 50 and the per-request budget of 12,000 extra candidates), `unhydrated` (inside a 5,000-file list window but left unread because the request budget ran out; the response then carries `deployment_artifacts_candidates_budget_reached`), `list_truncated` (one per related repository whose file list was cut at the 5,000-file window; the response carries `deployment_artifacts_related_file_list_truncated_at_5000`). `unhydrated / (hydrated + unhydrated)` is the share the budget withheld. No repository label; the stage lines carry the per-request counts (see [logs](logs.md)). |
| `eshu_dp_config_artifact_derives_total` | Per-repository config-artifact derives (#8155), by `trigger` (`write`: the content Write derives as its last step; `backfill` and `repair`: the reducer walker) and `outcome` (`ok`, `superseded`, `error`, `panic`, `timeout`, `canceled`, `encoding_unsupported`, `fence_missing`). `canceled` is a caller cancel (shutdown, supersede, or a lost claim), kept apart from `error`. No repository label. Registered by PR3b-2c-1; the Write derive emits trigger `write` since PR3b-2c-2, the reducer walker emits triggers `backfill` and `repair` since PR3b-2. |
| `eshu_dp_config_artifact_derive_duration_seconds` | Wall time of one per-repository config-artifact derive (#8155), by `trigger`. Buckets: 0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10, 15, 30 s: 1 ms is the round-trip floor of a tiny repository, `le="2"` reads the 2 s S1 per-repository cap, 10 s is the default `ESHU_CONFIG_ARTIFACT_DERIVE_TIMEOUT`, and 15 s is that deadline plus the 5 s failure-record bound a caller can wait. No repository label. Registered by PR3b-2c-1; the Write derive emits trigger `write` since PR3b-2c-2, the reducer walker emits triggers `backfill` and `repair` since PR3b-2. |
| `eshu_dp_config_artifact_reconcile_total` | Per-repository config-artifact walker outcomes (#8155 PR3b-2), by `outcome`: `repaired` (the derive wrote rows), `match` (a backfill walk found the repository already current), `superseded` (a repair lost to a racing Write), `error`, `skipped` (over the failure limit, not derived). No repository label. Registered and emitted since PR3b-2. |
| `eshu_dp_config_artifact_stale_repos` | Config-artifact repositories needing a derive at the last walker cycle (#8155 PR3b-2), by dirty-join `reason` (`missing`, `failed`, `stale_version`, `stale_extractor`, `retry`), every reason set each cycle including zero. Bounded to the walk page budget (`PageBudget`, 500 by default): a larger backlog reads as the budget, not the true backlog. Skipped repositories stay counted under their reason. No repository label. Registered and emitted since PR3b-2. |
| `eshu_dp_config_artifact_shadow_compare_total` | Per-repository config-artifact shadow compare outcomes (#8155 PR3c), by `outcome`: `match` (stored rows equal the live rows), `mismatch` (they differ; the request still serves the live answer), `stored_missing` (no trusted stored rows, so nothing to compare). A non-zero `mismatch` rate is the operator signal that the stored read model disagrees with the live extractor. No repository label. Registered since PR3c-shadow; emitted since the stored-cutover wiring slice. A `mismatch` also logs `config_artifacts.shadow.mismatch` (WARN) with the mismatched repository ids. Requests the shared request budget truncated (`candidates_unhydrated > 0` on the stage line) skip the compare, so a mismatch never reflects live truncation. |
| `eshu_dp_config_artifact_reads_total` | Related repositories of the deployment-artifact read path served stored or live (#8155 PR3c stored cutover), by `source` (`stored`: trusted stored rows; `live`: the whole mode in live and shadow mode, the per-repository fallback in stored mode) and `reason` (`trusted`, `missing`, `stale_version`, `stale_extractor`, `failed`, `mode_live`, `stored_error`). At 3 AM a slow story shows as a high `source="live"` share, and its `reason` label names the cause. No repository label; the stage lines carry the per-request counts (`read_mode`, `stored_repository_count`, `live_repository_count`, `live_reasons`, `stored_row_count`, `stored_statements`; see [logs](logs.md)). Emitted since the stored-cutover wiring slice. |
| `eshu_dp_query_impact_scoped_paths_withheld_total` | Paths or whole answers the #5167 impact path routes (`trace-resource-to-code`, `explain-dependency-path`, `trace-exposure-path`) withheld from a scoped caller, by `route` and `reason` (`ungranted_node`, `unchecked_over_cap`, `withheld_sink_class`, `anchor_ungranted`). A rising `unchecked_over_cap` rate means pages exceed the ownership budget at callers' grant sizes, so answers come back truncated. |
| `eshu_dp_query_security_alert_empty_answers_total` | Empty security-alert reconciliation answers (list, count, first inventory page) classified from collector state (#8803), by `coverage_state` (`complete`, `target_incomplete`, `not_configured`) and `reason` (`none` for a healthy empty answer, otherwise `collector_not_configured`, `repository_outside_target_set`, `no_committed_generation`, `collector_state_unavailable`, `no_authorized_repositories` (a scoped token with no grants; no collector read), or `terminal_fetch_failure:<auth_denied\|not_found\|rate_limited\|terminal\|retryable\|attempt_budget_exhausted\|identity_mismatch\|collect_failure\|commit_failure\|unclassified>`). A probe the caller canceled is not counted. A rising non-`complete` rate means callers are getting empty answers the collector has not earned; the `supply chain query stage completed` log for stage `security_alert_collection_state` carries the same `coverage_state`, `coverage_reason`, and `collector_state`, plus `error_class` (`deadline_exceeded`, `bound_exceeded`, or `query_failed` at WARN; `canceled` at INFO) when the probe failed. |
| `eshu_dp_query_impact_ownership_check_duration_seconds` | Wall time of one impact ownership statement chunk (at most 50 keys, `ownership.ChunkSize`), by `route`, `node_label` (`WorkloadInstance`, `CloudResource`, `TerraformStateResource`), and `outcome`. The per-class cost the ownership budget is sized from; p95 near the 10 s graph-read deadline means the budget is too generous for the graph. Cost follows owner fan-in, not only key count: a chunk of widely shared nodes (thousands of owners each) is the slow case. |
| `eshu_dp_query_oci_registry_truth_truncated_total` | Bounded OCI registry-truth reads (`trace_deployment_chain`) whose tag-observation or image-by-digest statement hit `LIMIT $row_limit` and had to withhold one or more image refs (#6590), by `reason` (`tag_observation_row_limit` / `image_row_limit`). A withheld ref is disclosed in the response's `image_registry_truth_limits.truncated_image_refs`, never given a placeholder row. |
| `eshu_dp_auth_identity_store_unavailable_total` | Credentials the API could not evaluate because the identity store was unreachable (#7586), by `failure_class`: `timeout` and `unavailable` (a lost connection, a refusal for lack of resources such as too many connections) usually clear on their own, and `topology` (a promoted or restored primary, or a different cluster) needs the process restarted or the database target corrected. Each is answered with a retryable `503` instead of a `401`; see [Logs](logs.md) for the `auth.identity_store.unavailable` event. |

`eshu_dp_projector_stage_duration_seconds` uses bounded `stage` values such as
`build_projection`, `graph_write`, `content_write`, and `intent_enqueue`.

## Focused Catalogs

- [Ingestion And Collector Metrics](metrics-ingestion-collectors.md) covers
  Git ingestion, discovery pruning, Terraform-state, OCI registry, Package
  Registry, AWS, Confluence, Grafana, Prometheus/Mimir, Loki, Tempo, workflow
  coordinator, and webhook intake.
- [Reducer And Storage Metrics](metrics-reducer-storage.md) covers reducer
  execution, shared follow-up, graph writes, storage, correlation, drift,
  supply-chain impact, capacity, and memory.
- [Identity Fact Cache Metrics](metrics-identity-cache.md) covers the reducer
  identity fact epoch cache: loads, shared flights, waiter outcomes, and the
  `identity_epoch_unstable` failure class.

## Dashboard Starting Points

| Dashboard | Start with |
| --- | --- |
| Runtime health | `eshu_runtime_health_state`, `eshu_runtime_queue_outstanding`, `eshu_runtime_queue_oldest_outstanding_age_seconds`, `eshu_runtime_stage_items`, `eshu_runtime_domain_oldest_age_seconds` |
| Ingest throughput | `eshu_dp_repos_snapshotted_total`, `eshu_dp_files_parsed_total`, `eshu_dp_facts_emitted_total`, `eshu_dp_collector_observe_duration_seconds`, `eshu_dp_projector_run_duration_seconds`, `eshu_dp_reducer_run_duration_seconds` |
| Webhook intake | `eshu_dp_webhook_requests_total`, `eshu_dp_webhook_trigger_decisions_total`, `eshu_dp_webhook_store_operations_total`, `eshu_dp_webhook_request_duration_seconds`, `eshu_dp_webhook_store_duration_seconds`, `eshu_dp_webhook_trigger_claim_reaps_total`, `eshu_dp_webhook_trigger_stale_claims`, `eshu_dp_webhook_trigger_ignored_decisions_total` |
| Semantic extraction | `eshu_dp_queue_depth{queue="semantic_extraction"}`, `eshu_dp_queue_oldest_age_seconds{queue="semantic_extraction"}` |
| Shared follow-up | `eshu_dp_shared_projection_cycles_total`, `eshu_dp_shared_projection_intent_wait_seconds`, `eshu_dp_shared_projection_processing_seconds`, `eshu_dp_shared_projection_stale_intents_total`, `eshu_dp_shared_acceptance_lookup_duration_seconds` |
| Generation retention | `eshu_dp_generation_retention_generations_pruned_total`, `eshu_dp_generation_retention_rows_pruned_total`, `eshu_dp_generation_retention_over_limit_batches_total`, `eshu_dp_generation_retention_failures_total`, `eshu_dp_generation_retention_skipped_total`, `eshu_dp_generation_retention_duration_seconds`, `eshu_dp_generation_retention_batch_size`, `eshu_dp_generation_retention_oldest_eligible_age_seconds`, `eshu_dp_generation_retention_phase_duration_seconds`, `eshu_dp_generation_retention_scope_lock_hold_seconds`, `eshu_dp_generation_retention_search_index_batches`, `eshu_dp_search_index_orphan_generations`, `eshu_dp_search_index_orphan_rows_swept_total`, `eshu_dp_search_index_superseded_rows_swept_total` |
| Changed-since link writer (dark, #7127) | `eshu_dp_changed_since_ack_journal_total` (projector Ack, PR-3b), `eshu_dp_changed_since_links_total`, `eshu_dp_changed_since_link_retries_total`, `eshu_dp_changed_since_link_failures_total`, `eshu_dp_changed_since_chain_breaks_total`, `eshu_dp_changed_since_link_retrying_scopes`, `eshu_dp_changed_since_link_poisoned_scopes`, `eshu_dp_changed_since_link_duration_seconds`, `eshu_dp_changed_since_link_delta_rows`, `eshu_dp_changed_since_link_keys`, `eshu_dp_changed_since_link_backlog`, `eshu_dp_changed_since_link_lag_seconds`, `eshu_dp_changed_since_state_bytes`, `eshu_dp_changed_since_state_rows`, `eshu_dp_changed_since_deltas_bytes`, `eshu_dp_changed_since_deltas_rows`, `eshu_dp_changed_since_ledger_orphans` |
| Generation liveness | `eshu_dp_active_generations`, `eshu_dp_generation_liveness_recovered_total`, `eshu_dp_generation_liveness_superseded_total`, `eshu_dp_generation_liveness_failures_total` |
| Activation obligations (#7584) | `eshu_dp_activation_obligations`, `eshu_dp_activation_obligation_oldest_open_age_seconds`, `eshu_dp_activation_obligation_claim_age_seconds`, `eshu_dp_activation_obligation_finalize_total`, `eshu_dp_activation_obligation_woken_total`, `eshu_dp_activation_obligation_maintenance_duration_seconds`, `eshu_dp_activation_obligation_catch_up_inserted_total`, `eshu_dp_activation_obligation_pruned_total`, `eshu_dp_activation_obligation_failures_total` |
| Graph cleanup | `eshu_dp_graph_orphan_nodes`, `eshu_dp_neo4j_query_duration_seconds`, reducer logs with `failure_class=graph_orphan_sweep_error` |
| Storage pressure | `eshu_dp_postgres_query_duration_seconds`, `eshu_dp_neo4j_query_duration_seconds`, `eshu_dp_neo4j_deadlock_retries_total`, `eshu_dp_canonical_write_duration_seconds`, `eshu_dp_canonical_atomic_fallbacks_total`, `eshu_dp_canonical_repository_retirements_total`, `eshu_dp_canonical_repository_stubs_created_total`, `eshu_dp_repository_retirements_purged_total` ([Retirement Trigger And Purge](retirement-purge.md)), `eshu_dp_repository_retirements_total` ([Retirement Trigger And Purge](retirement-purge.md)), `eshu_dp_graph_oversized_index_keys_skipped_total`, `eshu_dp_graph_index_key_guard_unanalyzed_total` |

When a metric points to one repo, scope, generation, or work item, move to
[logs](logs.md) and [traces](traces.md). Do not add high-cardinality labels to
make metrics carry the full debugging payload.
