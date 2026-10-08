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
client disconnect and also stays `500`.

## Runtime Health And Backlog

Use these first when asking whether a runtime is alive, ready, or stuck:

- `eshu_runtime_info`
- `eshu_runtime_status_snapshot_available` — unlabelled per-scrape gauge: `1` when the status snapshot succeeded, `0` when it failed or timed out. OTEL metrics remain available when this is `0`; status-derived gauge values are omitted.
- `eshu_runtime_status_summary_stale{service_name,model_key}` — per-scrape gauge, rendered only while `ESHU_STATUS_SUMMARY_READ_ENABLED=true`: `0` when the scrape served a fresh stored active-work summary row, `1` when it served the newest row this process can decode, stale included and aged to the read, or the zero summary when it can decode none, because the current row could not be served fresh. A scrape never runs the live active-work statement. Alert on `== 1` held for several minutes; it means the reducer writer is down, slow, or on another statement version.
- `eshu_runtime_status_summary_age_seconds{service_name,model_key}` — per-scrape gauge, rendered with the stale gauge: the age in seconds of the active-work summary the scrape served (database clock at the read minus the row's `as_of`), growing while the writer is down; `NaN` when the process can decode no row (the zero summary), so comparisons drop it.
- `eshu_runtime_health_state`
- `eshu_runtime_scope_active`
- `eshu_runtime_scope_changed`
- `eshu_runtime_scope_unchanged`
- `eshu_runtime_refresh_skipped_total`
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
completeness.

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
| `eshu_dp_worker_pool_active` | Active worker count by pool. |
| `eshu_dp_collector_observe_duration_seconds` | Collector observe cycle cost. |
| `eshu_dp_scope_assign_duration_seconds` | Repository or source scope assignment cost. |
| `eshu_dp_fact_emit_duration_seconds` | Fact emission cost. |
| `eshu_dp_facts_emitted_total` | Collector fact output volume. |
| `eshu_dp_facts_committed_total` | Durable fact commit volume. |
| `eshu_dp_generation_fact_count` | Fact volume per scope generation. |
| `eshu_dp_collector_delta_baseline_fallback_total` | Git delta syncs that fell back to a full snapshot, by `skip_reason` (`no_projected_baseline`, `baseline_unreachable`, `baseline_lookup_error`). |
| `eshu_dp_git_repo_sync_failures_total` | Per-repository git sync operations (`clone`, `fetch`, `list_refs`) that failed and were isolated to that one repository for the cycle rather than aborting the rest of the fleet, by bounded `operation` (issue #7001). A `list_refs` spike points at remote/DNS trouble for the affected repos; a rate near the fleet size points at an auth or network outage. |
| `eshu_dp_collector_reconciliation_full_snapshots_total` | Git scopes forced to a full reconciliation snapshot to retract delta-path drift, by `reason` (including `graph_dirty`, #7389, and `reindex_requested` and `repository_reindex_requested`, #7620). |
| `eshu_dp_collector_reconciliation_suppressed_total` | Reconciliation sweep evaluations held off because a full generation is in flight or in retry backoff, by `reason`. |
| `eshu_dp_reconciliation_drift_retractions_total` | Graph nodes and edges actually retracted while applying a forced reconciliation snapshot, by bounded `domain`, `write_phase`, and `kind`. |
| `eshu_dp_workload_repository_edge_retractions_total` | Stale `DEFINES` and repository-side `EXPOSES_ENDPOINT` edges `workload_materialization` retracted on full generations (#7285), by bounded `write_phase` (`defines_retract`, `repository_endpoint_retract`). Ordinary workload or endpoint removal, not collector drift. |
| `eshu_dp_reconciliation_convergence_total` | Denormalized graph edges classified by the dual-write reconciliation pass, by bounded `domain` and `drift_kind` (`in_sync` / `stale_generation` / `orphan_resolved_id`). Non-`in_sync` values are stranded edges whose denormalized `generation_id`/`resolved_id` no longer match the authoritative Postgres generation after a swap; a sustained nonzero `stale_generation` or `orphan_resolved_id` rate means a Postgres↔graph partial failure left inconsistent edges that the pass is retracting to converge. |
| `eshu_dp_projector_run_duration_seconds` | Projector claim-and-project cycle cost. |
| `eshu_dp_projector_stage_duration_seconds` | Projector substage duration. |
| `eshu_dp_projector_ack_deferrals_total` | Projector Acks deferred because a same-scope ingestion commit held the scope row, by `outcome`: `retried`, `abandoned` (the 150-retry bound ran out), or `shutdown`. `retried` is counted before the lease renewal; a renewal that then ends the wait shows its result only in `eshu_dp_projector_ack_wait_seconds`. A rising `abandoned` rate means work is being dropped for re-projection after its lease expires. |
| `eshu_dp_component_producer_grant_decisions_total` | Producer-grant allow/deny decisions for core-owned fact kinds, by `decision` (`allow`/`deny`), `stage` (`install`/`readback`/`activation`/`emission`), closed `reason` (`granted` or a deny reason), and core `fact_kind`. Producer id is span/log only. Emitted by `collector-component-extension` (readback, activation, emission) and `workflow-coordinator` (readback, activation); the CLI, API, and MCP server emit none, so separate the two emitters by `service_name`. See [Producer-Grant Decisions](producer-grant-decisions.md). |
| `eshu_dp_projector_ack_wait_seconds` | Time a deferred projector Ack waited for a busy scope, by terminal `outcome`: `succeeded`, `abandoned`, `shutdown`, `superseded`, `claim_lost`, or `failed`. Acks that never waited record nothing. |
| `eshu_dp_projector_write_marker_deferrals_total` | Projector write-marker attempts deferred because a busy generation row held the lock, by `outcome`: `retried`, `gave_up` (the 150-retry bound ran out), or `shutdown`. A rising `gave_up` rate means generation rows stay locked for the whole bound and items go through the queue's Fail path for a later retry. |
| `eshu_dp_projector_write_marker_wait_seconds` | Time a deferred projector write marker waited for a busy generation row, by terminal `outcome`: `written`, `gave_up`, `shutdown`, `superseded`, `claim_lost`, or `failed`. Markers that never waited record nothing. |
| `eshu_dp_superseded_generation_fence_total` | Projector work stopped because its scope generation is superseded (#7130), by `failure_class`. `projector_heartbeat_generation_superseded`: Heartbeat stopped running work whose own generation a newer Ack retired, usually a worker whose lease had expired; the projection is cancelled at the first heartbeat whose supersede check gets the scope row, which is one heartbeat interval plus any interval in which ingestion, Ack or Fail holds that row. `projector_ack_generation_superseded`: Ack refused to re-activate the generation and marked the work item superseded, so superseded-generation work reached a worker and finished (for example a claim that raced a supersede). `projector_replay_generation_superseded`: a replay or dead-letter drain left such rows terminal, counted per replay call (a repeated drain counts the same rows again), or the admin replay refused explicit ids naming them. The claim itself sweeps a superseded generation's claimable rows without counting here; those rows carry `failure_class = projector_superseded_by_newer_generation` and `failure_details.generation_status = superseded`. Every supersede of a work row keeps the class and message as the supersede marker and folds the old failure into `failure_details.prior_failure` (`status`, `failure_class`, `failure_message`, `failure_details` as a JSON string, `updated_at`; `updated_at` is the old row's own `updated_at`, which is the failure time for a failed or dead_letter row but the claim or heartbeat time for a claimed or running row carrying a retry's failure; present only when the old row was failed or dead_letter or carried a non-blank failure field, #7320). Read it with `SELECT work_item_id, CASE WHEN failure_details IS JSON OBJECT THEN failure_details::jsonb -> 'prior_failure' END AS prior_failure FROM fact_work_items WHERE status = 'superseded'` (the `CASE` guard keeps a row with non-JSON details from aborting the cast; PostgreSQL 16 or newer for `IS JSON`). Rows superseded before #7320 lost that evidence and cannot be recovered. A nonzero heartbeat or Ack rate is worth tracing to the claim or replay that produced the work. To see the failure a fenced row carried before the supersede, read `latest_failure.prior_failure` on `GET /api/v0/freshness/generations` or `eshu freshness generations` (#7385). |
| `eshu_dp_projector_delta_baseline_fence_total` | Delta-baseline fence decisions (#7319), by `phase` (`preflight`, `ack`) and `outcome`: `matched`, `unfenced` (a delta with no recorded baseline), `already_active`, `refused_active_differs`, `refused_no_active`. Passes count once, at Ack; preflight counts only refusals; full generations and an already-superseded target (`target_superseded`) are not counted -- a target superseded by a newer activation is routine projector lag, owned by `eshu_dp_superseded_generation_fence_total` instead. Separate from that counter otherwise: a baseline refusal is expected under projector lag. Since #7389 a `phase=preflight` `refused_active_differs` is expected when a newer generation arrives while an older one is writing (the writer runs to Ack); its rate should not exceed the scope's generation commit rate, and a share beyond that means the projector lags the collector; any `phase=ack` refusal (`refused_active_differs` or `refused_no_active`) means two claims were valid in one scope (logged at ERROR, `failure_class = projector_delta_baseline_mismatch_after_projection`). See [Reconciliation Sweep](../reconciliation-sweep.md#delta-baseline-fence). |
| `eshu_dp_search_document_generation_superseded_total` | Search-document projections the reducer abandoned because their generation was superseded mid-write (#7458), by `phase`: `page` (the check before a page write saw the newer active generation) or `finalize` (the check before the authoritative retire did). The handler checks freshness before every page and once before `Finalize`, then returns a `superseded` result without `Cancel` or `Finalize`; the rows already written stay for retention because every reader joins the active generation. Expect a rate no higher than the scope generation commit rate. The same events also count as `status=superseded` on `eshu_dp_reducer_executions_total{domain="eshu_search_document"}` and log INFO `eshu search document projection abandoned: generation superseded` with `pages_written` and `documents_written`; abandoned pages are not counted in `eshu_dp_canonical_writes_total`. A freshness lookup error is not counted here: it fails the item, which the queue retries only for `GenerationNotYetActiveError` and otherwise dead-letters. |
| `eshu_dp_repo_dependency_replay_skipped_total` | Workload-materialization replay requests the repo-dependency runner skipped because their scope generation is no longer the scope's active generation (#7670), by `reason`: `inactive_generation`. recover-generations retires a generation without rewriting its acceptance row, so the lane can owe a replay for a retired generation; the request counts here, logs WARN `repo dependency workload materialization replay skipped: nothing to replay` (`scope_id`, `generation_id`, `entity_key`, `fenced`, `reason`), and does not quarantine the lane. It covers the plain replay and the RUNS_ON fenced replay and counts once per scope, generation and entity per cycle even when a RUNS_ON row produces both requests; the WARN's `fenced` field says which path counted it. A retired generation is also not waited on for RUNS_ON readiness. A request on the active generation that cannot be scheduled is never counted here: it fails the cycle and counts on `eshu_dp_shared_projection_lease_quarantines_total`. A freshness lookup error fails the cycle instead of being skipped. |
| `eshu_dp_repo_dependency_generation_anomalies_total` | Repo-dependency generation states an operator should see (#7670), by `reason`: `superseded_item_on_active_generation` (plain replay only: a replay on the scope's active generation found its stable `workload_materialization` item terminally `superseded`, which the queue never revives; the cycle fails closed, the lease quarantines and the intents stay pending, so any nonzero value needs an operator) `unscheduled_fenced_replay_on_active_generation` (the RUNS_ON fenced replay, which reports only a boolean, so a superseded item there cannot be told from a dead-lettered one and does not count as `superseded_item_on_active_generation`: a readiness replay on the active generation could not be scheduled; the cycle fails closed with `workload materialization fenced replay was not scheduled` and quarantines the lease), or `inactive_accepted_generation` (counted per row: active rows about to be written whose accepted generation is no longer the scope's active generation; they still project, the counter only exposes the stale acceptance). Each logs WARN `repo dependency generation anomaly` with `scope_id`, `generation_id`, `entity_key`, `rows`, `reason`, once per scope generation per cycle for the inactive case. |
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
| `eshu_dp_generation_retention_scope_lock_hold_seconds` | How long one generation-retention transaction held its scope row locks. |
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
| `eshu_dp_entity_context_resolution_total` | `GET /api/v0/entities/{entity_id}/context` requests that reached the answer decision (the graph returned a row, the content store answered, or both missed and the request answered a true 404), by `resolved_by` (#7212): `anchor` = a non-final anchor statement returned the row (the Neo4j indexed `CALL ()` anchor, or a labeled statement of the NornicDB per-label loop); `fallback` = the final unlabeled `MATCH (e)` statement returned the row; `content` = the graph returned no row and the content store answered; `none` = nothing answered, 404. `none / sum(rate(...))` over a window is the true-miss share. A request that ends in an error (graph read error, deadline, 5xx) or is rejected before the graph read (400, empty scoped grant) is not counted. The request span carries `eshu.entity_context.resolved_by` and `eshu.entity_context.statements_tried`; see the traces reference for when each is present. |
| `eshu_dp_query_impact_scoped_paths_withheld_total` | Paths or whole answers the #5167 impact path routes (`trace-resource-to-code`, `explain-dependency-path`, `trace-exposure-path`) withheld from a scoped caller, by `route` and `reason` (`ungranted_node`, `unchecked_over_cap`, `withheld_sink_class`, `anchor_ungranted`). A rising `unchecked_over_cap` rate means pages exceed the ownership budget at callers' grant sizes, so answers come back truncated. |
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

## Dashboard Starting Points

| Dashboard | Start with |
| --- | --- |
| Runtime health | `eshu_runtime_health_state`, `eshu_runtime_queue_outstanding`, `eshu_runtime_queue_oldest_outstanding_age_seconds`, `eshu_runtime_stage_items`, `eshu_runtime_domain_oldest_age_seconds` |
| Ingest throughput | `eshu_dp_repos_snapshotted_total`, `eshu_dp_files_parsed_total`, `eshu_dp_facts_emitted_total`, `eshu_dp_collector_observe_duration_seconds`, `eshu_dp_projector_run_duration_seconds`, `eshu_dp_reducer_run_duration_seconds` |
| Webhook intake | `eshu_dp_webhook_requests_total`, `eshu_dp_webhook_trigger_decisions_total`, `eshu_dp_webhook_store_operations_total`, `eshu_dp_webhook_request_duration_seconds`, `eshu_dp_webhook_store_duration_seconds` |
| Semantic extraction | `eshu_dp_queue_depth{queue="semantic_extraction"}`, `eshu_dp_queue_oldest_age_seconds{queue="semantic_extraction"}` |
| Shared follow-up | `eshu_dp_shared_projection_cycles_total`, `eshu_dp_shared_projection_intent_wait_seconds`, `eshu_dp_shared_projection_processing_seconds`, `eshu_dp_shared_projection_stale_intents_total`, `eshu_dp_shared_acceptance_lookup_duration_seconds` |
| Generation retention | `eshu_dp_generation_retention_generations_pruned_total`, `eshu_dp_generation_retention_rows_pruned_total`, `eshu_dp_generation_retention_over_limit_batches_total`, `eshu_dp_generation_retention_failures_total`, `eshu_dp_generation_retention_skipped_total`, `eshu_dp_generation_retention_duration_seconds`, `eshu_dp_generation_retention_batch_size`, `eshu_dp_generation_retention_oldest_eligible_age_seconds`, `eshu_dp_generation_retention_phase_duration_seconds`, `eshu_dp_generation_retention_scope_lock_hold_seconds` |
| Changed-since link writer (dark, #7127) | `eshu_dp_changed_since_links_total`, `eshu_dp_changed_since_link_retries_total`, `eshu_dp_changed_since_link_failures_total`, `eshu_dp_changed_since_chain_breaks_total`, `eshu_dp_changed_since_link_retrying_scopes`, `eshu_dp_changed_since_link_poisoned_scopes`, `eshu_dp_changed_since_link_duration_seconds`, `eshu_dp_changed_since_link_delta_rows`, `eshu_dp_changed_since_link_keys`, `eshu_dp_changed_since_link_backlog`, `eshu_dp_changed_since_link_lag_seconds`, `eshu_dp_changed_since_state_bytes`, `eshu_dp_changed_since_state_rows`, `eshu_dp_changed_since_deltas_bytes`, `eshu_dp_changed_since_deltas_rows`, `eshu_dp_changed_since_ledger_orphans` |
| Generation liveness | `eshu_dp_active_generations`, `eshu_dp_generation_liveness_recovered_total`, `eshu_dp_generation_liveness_superseded_total`, `eshu_dp_generation_liveness_failures_total` |
| Activation obligations (#7584) | `eshu_dp_activation_obligations`, `eshu_dp_activation_obligation_oldest_open_age_seconds`, `eshu_dp_activation_obligation_claim_age_seconds`, `eshu_dp_activation_obligation_finalize_total`, `eshu_dp_activation_obligation_woken_total`, `eshu_dp_activation_obligation_maintenance_duration_seconds`, `eshu_dp_activation_obligation_catch_up_inserted_total`, `eshu_dp_activation_obligation_pruned_total`, `eshu_dp_activation_obligation_failures_total` |
| Graph cleanup | `eshu_dp_graph_orphan_nodes`, `eshu_dp_neo4j_query_duration_seconds`, reducer logs with `failure_class=graph_orphan_sweep_error` |
| Storage pressure | `eshu_dp_postgres_query_duration_seconds`, `eshu_dp_neo4j_query_duration_seconds`, `eshu_dp_neo4j_deadlock_retries_total`, `eshu_dp_canonical_write_duration_seconds`, `eshu_dp_canonical_atomic_fallbacks_total`, `eshu_dp_canonical_repository_retirements_total`, `eshu_dp_canonical_repository_stubs_created_total`, `eshu_dp_graph_oversized_index_keys_skipped_total`, `eshu_dp_graph_index_key_guard_unanalyzed_total` |

When a metric points to one repo, scope, generation, or work item, move to
[logs](logs.md) and [traces](traces.md). Do not add high-cardinality labels to
make metrics carry the full debugging payload.
