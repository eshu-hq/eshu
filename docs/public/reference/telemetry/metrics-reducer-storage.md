# Reducer And Storage Metrics

This catalog covers reducer execution, shared follow-up, graph writes, storage,
correlation, supply-chain impact, capacity, and memory metrics.

## Reducer Execution

| Metric | Type | Use |
| --- | --- | --- |
| `eshu_dp_reducer_intents_enqueued_total` | counter | Reducer intent enqueue volume by domain. |
| `eshu_dp_reducer_admission_deferrals_total` | counter | Ingester source-local reducer intent enqueue deferrals while reducer backlog is at the configured high-water mark. |
| `eshu_dp_reducer_readiness_waits_total` | counter | Evaluations of a reducer handler's commit-first cross-scope readiness wait that had missing endpoints, by `domain` and `outcome`. `deferred`: the ready edges committed, then the intent returned a retryable readiness class to wait for the rest. `abandoned`: the missing set settled at first-defer plus the bound (30 min); it fires once per (scope, domain, missing set), because the anchor lives in `reducer_readiness_waits` and survives a superseding generation. `settled_missing`: a later evaluation committed at once on an already settled set, with no wait. Emitted by `iam_can_perform_materialization` and `workload_cloud_relationship_materialization`. A steady `abandoned` or `settled_missing` rate names upstream nodes that never materialize. |
| `eshu_dp_reducer_executions_total` | counter | Reducer execution volume by domain and status. |
| `eshu_dp_reducer_run_duration_seconds` | histogram | Handler execution window after a worker starts a work item. |
| `eshu_dp_reducer_queue_wait_seconds` | histogram | Time visible in the reducer queue before handler start. |
| `eshu_dp_reducer_batch_claim_size` | histogram | Batch claim size where batched reducer claiming is used. |
| `eshu_dp_reducer_heartbeat_missed_total` | counter | Reducer lease heartbeat failures by domain, including the immediate pre-heartbeat emitted at claim time. A non-zero rate means a worker's lease may be reclaimed and re-executed by another worker. |

`eshu_dp_reducer_executions_total{status="succeeded"}` means the reducer ACK
completed. `ack_claim_rejected` marks a rejected single-item ACK;
`ack_outcome_unknown` marks a batch whose ACK may have committed only some
items, or an ACK cut short by shutdown. `ack_abandoned_to_lease_expiry` marks
an ACK, single-item or batch, that kept failing with a Postgres deadlock
(40P01) or serialization failure (40001) through its five attempts: the claim
stays leased and is reclaimed when the lease expires, and the run keeps
draining. A steady rate of it points at lock contention on the queue rows, not
at a stopping process. Inspect durable queue state before treating any of these
as completed work.

Compare queue wait with run duration before changing worker counts. High queue
age with low run duration points to claim, routing, or conflict-domain pressure.
High run duration points to the handler, store, or graph-write path.

## Package-Consumption Sidecar Repair

| Metric | Type | Use |
| --- | --- | --- |
| `eshu_dp_package_manifest_backfill_passes_total` | counter | Reducer repair attempts by closed `outcome`: `contended` (another replica owns the advisory lock), `failed`, `incomplete` (successful bounded pass, readiness still false), or `ready`. |
| `eshu_dp_package_manifest_backfill_duration_seconds` | histogram | Election plus pass wall time by the same outcome; failed attempts are included. |
| `eshu_dp_package_manifest_backfill_last_success_unixtime` | gauge | Unix second of the last successful elected pass; use `time() - eshu_dp_package_manifest_backfill_last_success_unixtime` for pass age. It is absent until the first success. |
| `eshu_dp_package_manifest_backfill_dirty_scopes` | gauge | Dirty scopes after an elected successful pass, capped at 26; 26 means at least two 25-scope repair passes remain. Sampled by one bounded SQL read per pass, never by the scrape callback. |
| `eshu_dp_package_manifest_backfill_cursor_updated_unixtime` | gauge | Unix second when the initial scope-walk cursor last advanced; zero before the first scope. A flat cursor with `ready=0` and repeated `incomplete` passes warrants checking the per-scope progress logs. |
| `eshu_dp_package_manifest_backfill_ready` | gauge | One when both sidecars are ready after an elected successful pass, zero otherwise. Contended and failed passes do not overwrite the last known value. |

The reducer emits a structured elected-pass log with `ready`,
`dirty_scopes_capped`, `cursor_updated_unixtime`, and `duration_seconds`;
contention and failure have separate structured logs. A progress-sampling
failure warns and leaves the progress gauges stale without failing the repair pass.
Storage writes key=value process logs for each completed scope during the
initial walk and dirty repair. These signals describe reducer repair progress; the API readiness
reader remains the authority for a particular request. A missing pass gauge
after startup or a growing pass age indicates the elected repair is not
completing. Metric labels never contain scope IDs or package names.

## Persisted Search Index

| Metric | Type | Use |
| --- | --- | --- |
| `eshu_dp_search_index_mutations_total` | counter | Document and term upsert/retire volume for the persisted semantic search index. |
| `eshu_dp_search_index_errors_total` | counter | Search index write failures by bounded operation. |
| `eshu_dp_search_index_write_duration_seconds` | histogram | Persisted search index write duration split by bounded operation and result. |

Search index metrics use bounded labels such as `domain`, `kind`, `operation`,
and `result`. Scope IDs, generation IDs, document IDs, paths, terms, and
provider-native identifiers stay in spans, structured logs, or durable facts.

## Generation Retention

| Metric | Type | Use |
| --- | --- | --- |
| `eshu_dp_generation_retention_generations_pruned_total` | counter | Superseded generations pruned by bounded retention cleanup. |
| `eshu_dp_generation_retention_rows_pruned_total` | counter | Rows pruned by bounded table/data-class label. |
| `eshu_dp_generation_retention_failures_total` | counter | Cleanup failures by bounded reason: `store_error`, or `key_index_unavailable` when a cycle is refused because a retention key index is missing or invalid. |
| `eshu_dp_generation_retention_skipped_total` | counter | Candidate generations skipped by bounded reason: `row_limit` (its rows outside the changed-since ledger exceed `BatchRowLimit`, or the batch was full), `row_limit_ledger` (it exceeds the limit only because of its changed-since ledger rows: deferred to a batch of its own). |
| `eshu_dp_generation_retention_over_limit_batches_total` | counter | Retention batches of one generation admitted over `BatchRowLimit` by its changed-since ledger rows. A rising rate means the limit is small for the links being written; the cycle log's `rows_over_batch_row_limit` gives the excess of one batch, and its `locked_scope_rows` the scope rows held by the pruned batch, not the selection's full lock set. |
| `eshu_dp_generation_retention_duration_seconds` | histogram | Cleanup transaction duration. |
| `eshu_dp_generation_retention_batch_size` | histogram | Superseded generation count selected by one cleanup batch. |
| `eshu_dp_generation_retention_oldest_eligible_age_seconds` | histogram | Oldest selected superseded generation age in one batch. |
| `eshu_dp_generation_retention_phase_duration_seconds` | histogram | Cleanup transaction time by bounded `phase`: `key_index_check`, `select_candidates`, `count_rows`, `record_events`, `delete_shared_projection_intents`, `prune_content_file_references`, `lock_infra_repositories`, `prune_content_entities`, `delete_infra_orphans`, `prune_content_files`, `delete_changed_since_ledger` (the changed-since ledger delete, #7127), `delete_scope_generations`, `commit`. A batch of one narrowed after its selection times its targeted re-lock under `select_candidates` and its recount under `count_rows`. |
| `eshu_dp_generation_retention_scope_lock_hold_seconds` | histogram | How long one cleanup transaction held its `ingestion_scopes` row locks. A fact insert into one of those scopes waits up to this long. |
| `eshu_dp_changed_since_links_total` | counter | Changed-since link attempts (#7127, dark) by `link_kind` (root, incremental, none) and `outcome` (linked, break, failed, poisoned, canceled; `canceled` is a link cut short by reducer shutdown, not counted as a failure). A rebase over a pruned prior is `root`/`linked`. |
| `eshu_dp_changed_since_link_retries_total` | counter | Non-counting changed-since link misses by `reason` (cursor_locked, generation_locked, generation_lock_timeout, slot_busy; generation_locked covers the activating generation and the link's prior; generation_lock_timeout is a generation lock that waited 250 ms in PostgreSQL's update-chain walk or on a table lock and gave up, from a link or a journal pass); nothing written, the cursor did not move. A steady generation_lock_timeout rate means a transaction holds generation rows for long: check `pg_stat_activity` and `pg_blocking_pids` (usually a retention batch, or a migration). |
| `eshu_dp_changed_since_link_failures_total` | counter | Counting changed-since link failures by `failure_class` (statement_timeout, connection_lost, sql_error, internal); each is recorded on the scope cursor with backoff. |
| `eshu_dp_changed_since_chain_breaks_total` | counter | Chain breaks by `reason`. pruned_before_link, delta_without_root, prior_mismatch, overlay_unproven and link_poisoned advance the activation without a link and keep the state. prior_pruned is a rebase (#7127 PR-3e): the state's generation was pruned, so the state moved by the same diff and a root link was recorded. |
| `eshu_dp_changed_since_link_retrying_scopes` | gauge | Scopes with a counted failure pending on their head activation, computed in SQL (fleet-wide). |
| `eshu_dp_changed_since_link_poisoned_scopes` | gauge | Scopes carrying the link_poisoned marker until their next full link, computed in SQL. |
| `eshu_dp_changed_since_link_duration_seconds` | histogram | Committed root or incremental link duration by `link_kind`. |
| `eshu_dp_changed_since_link_delta_rows` | histogram | Link delta rows one link wrote, by `link_kind`. |
| `eshu_dp_changed_since_link_keys` | histogram | Effective keys of the generation a link reached, by `link_kind`. |
| `eshu_dp_changed_since_link_backlog` | gauge | Activation rows above their scope cursor. |
| `eshu_dp_changed_since_link_lag_seconds` | gauge | Age of the oldest activation above its scope cursor. |
| `eshu_dp_changed_since_state_bytes` | gauge | Total size of `changed_since_key_state`. |
| `eshu_dp_changed_since_state_rows` | gauge | Planner row estimate of `changed_since_key_state`. |
| `eshu_dp_changed_since_deltas_bytes` | gauge | Total size of `changed_since_link_deltas`. Generation retention bounds it; a value that only grows means retention is not pruning the ledger. |
| `eshu_dp_changed_since_deltas_rows` | gauge | Planner row estimate of `changed_since_link_deltas`. |
| `eshu_dp_changed_since_ledger_orphans` | gauge | Orphan probe by `kind`: `link` (a link naming a pruned generation or prior), `activation` (an activation of a pruned generation), `bucket_group` (bucket counts with no link). Sampled at most once a minute. Zero while every ledger writer locks the generations it names (#7127 PR-3e); non-zero only briefly for a deleted scope, so a value that persists is a writer defect. |

A cleanup cycle checks that `fact_records_content_entity_key_idx` and
`fact_records_file_key_idx` (migrations 146 and 147) are valid before it locks
anything. Its content prunes probe those indexes once per candidate key; without
them each probe would scan `fact_records` while the scope locks are held. When
either index is missing, invalid (a failed concurrent build), or a different
shape, the cycle is refused: `eshu_dp_generation_retention_failures_total{reason="key_index_unavailable"}`
increments and the reducer logs `failure_class=generation_retention_key_index_unavailable`
with the index name. Rebuild the index (a restart reruns the migration, which
drops an invalid index first) and the next poll proceeds.

Retention metrics intentionally do not label raw scope IDs, generation IDs,
repository paths, source names, or provider identifiers. Use the retention event
table's safe hashes and structured logs for authorized drilldown.

Each `generation_retention_events` row carries `row_counts`, the rows its
generation's pruning removes by table. A content row shared by several
generations in one batch is counted once, on the newest of them. For the
content tables (`content_entities`, `content_files`, `content_file_references`),
the generation-owned tables that carry an event count, and the changed-since
ledger tables (`changed_since_links`, `changed_since_link_deltas`,
`changed_since_link_bucket_counts`, `changed_since_activations`; a link naming
two pruned generations is counted on the newer), a batch's event
counts sum to the rows that `eshu_dp_generation_retention_rows_pruned_total`
adds for that table, in the absence of concurrent writes between the count and
the deletes. `scope_generations` and `shared_projection_unroutable_intents` have
no event count. The `infra_resource_entities` delete also removes mirror rows
that were already orphaned before the batch, so the counter can exceed the event
sum for that table.

## Status Summary Writer

The reducer's periodic writer of the `status_summary_snapshots` read model
(#7009), on only when `ESHU_STATUS_SUMMARY_WRITER_ENABLED=true`. One pass
writes two rows in one transaction: `active_work_summary` and `terraform_state`.
Every metric carries `model_key` (`active_work_summary` or `terraform_state`).

| Metric | Type | Use |
| --- | --- | --- |
| `eshu_dp_status_summary_writer_passes_total` | counter | Writer passes by `model_key` and `outcome`, one sample per model per pass (a skipped or failed pass counts for both): `ok` (row advanced), `skipped_lock` (another replica held the advisory lock this tick; expected on every replica but one), `skipped_missing_table` (migration 161 not applied yet), `rejected_guard` (a stored row was as new or newer; nothing changed), `error` (rolled back; the next tick retries). |
| `eshu_dp_status_summary_writer_model_compute_seconds` | histogram | Time one model's statement or statements took inside a pass, by `model_key`. The `terraform_state` samples are the cost of the two Terraform-state statements and their Go decode; a rise there, not in the pass total, points at `fact_records`. |
| `eshu_dp_status_summary_writer_pass_duration_seconds` | histogram | Duration of one whole pass transaction (both models), by `outcome`, labeled with the first model's key. The `ok` samples are the cost the writer adds to the primary every interval. |
| `eshu_dp_status_summary_writer_overrun_total` | counter | Passes longer than `ESHU_STATUS_SUMMARY_WRITER_INTERVAL`. The next pass then starts on the following interval boundary, so a steady rise means the stored row ages past one interval. |
| `eshu_dp_status_summary_writer_up` | gauge | 1 while this reducer's writer loop runs, 0 after it stops. Absent when the writer is disabled. |

## Status Summary Reader

The status snapshot's read of the stored `status_summary_snapshots` row (#7009),
on only when `ESHU_STATUS_SUMMARY_READ_ENABLED=true`. Emitted by every process
that builds a status store with instruments: the API, the MCP server, and the
hosted runtimes. With the flag off, each read counts once as `source=live`,
`reason=flag_off`.

| Metric | Type | Use |
| --- | --- | --- |
| `eshu_dp_status_summary_read_total` | counter | Status snapshot reads of a stored summary by `model_key` (`active_work_summary`, `terraform_state`), `source` (`model`: the stored row was served; `live`: the reader is off; `live_fallback`: the live statement answered because the row could not be served) and `reason` (`fresh`, `flag_off`, `missing`, `not_installed`, `version`, `row_count`, `stale`, `decode`). A steady `live_fallback` share means the writer is down, slow, or on another statement version; `stale` points at a slow or stopped writer, `version` at a rolling upgrade. |
| `eshu_dp_status_summary_read_age_seconds` | histogram | Age of the stored row, from the database clock, at each read that served it (`source=model` only). Compare its upper buckets with `ESHU_STATUS_SUMMARY_STALE_AFTER` (default `33s`): a p95 near the limit predicts fallbacks. |
| `eshu_dp_status_summary_scrape_total` | counter | Runtime `/metrics` scrapes of the active-work summary by `model_key`, `source` (`model`: a fresh stored row; `last_row`: the newest row this process can decode, now stale, including a stale row found at startup; `zero`: the empty summary because the process can decode no row) and `reason` (`fresh`, `missing`, `not_installed`, `version`, `row_count`, `stale`, `decode`). A scrape never runs the live statement, so `last_row` or `zero` is the whole signal that the writer is down, slow, or on another version; the per-scrape gauges `eshu_runtime_status_summary_stale` and `eshu_runtime_status_summary_age_seconds` carry the same state. Emitted only while the reader is on. |

On the runtime `/metrics` scrape the same span attributes carry `source` `model`, `last_row`, or `zero`, and a scrape that serves a row other than a fresh one logs
`status summary row not served fresh on the metrics scrape; serving the newest decodable row or the zero summary` at Warn, at most once a minute per reason per process, with
`model_key`, `source`, `reason`, `age_seconds`, and `failure_class=status_summary_scrape_stale`.

The `postgres.status_snapshot` span carries `status.active_work.source`,
`status.active_work.as_of_age_seconds`, `status.active_work.as_of_age_signed_seconds`
(the age before the clamp at zero), and, on a fallback,
`status.active_work.fallback_reason`; the Terraform-state model sets the same
four attributes under `status.terraform_state.`. A fallback logs
`status summary row not served; running the live statement` at Warn,
at most once a minute per model and reason per process, with `model_key`, `source`,
`reason`, `age_seconds`, and `failure_class=status_summary_fallback`.

## Infra Read Model Reconcile

| Metric | Type | Use |
| --- | --- | --- |
| `eshu_dp_infra_inventory_reconcile_total` | counter | Repositories the reducer's infra read model reconcile checked, by `outcome`: `match`, `suspect` (differed once; re-checked next cycle), `repaired` (differed on two checks one interval apart, then re-derived), `fenced` (re-derived because a binary that does not derive, or manual SQL, wrote its content rows; unscoped reads stay on the graph while any repository waits for this), `error` (a repository check, or a whole cycle, failed). |
| `eshu_dp_infra_inventory_reconcile_duration_seconds` | histogram | Wall time of one reconcile cycle, failed cycles included (at most `ESHU_INFRA_INVENTORY_RECONCILE_REPO_BUDGET` repositories). |
| `eshu_dp_infra_inventory_dirty_repos` | gauge | Repositories carrying a rolling-upgrade fence mark (#6793), sampled at the start of each reducer reconcile cycle whether or not the backfill marker exists. Unscoped infra aggregate reads stay on the graph while it is above zero. |
| `eshu_dp_infra_inventory_dirty_oldest_age_seconds` | gauge | Age of the oldest fence mark at each reconcile cycle; 0 with none. A value that keeps growing means nothing is repairing marks (reconcile disabled, or the reducer is still an older release). |

Repository ids appear only in the `infra_inventory.reconcile.drift` and
`infra_inventory.reconcile.failed` logs, never as metric labels.

## Generation Liveness

| Metric | Type | Use |
| --- | --- | --- |
| `eshu_dp_active_generations` | observable gauge | Current active scope generation count by closed activation-age bucket `age_bucket` (`fresh`, `aging`, `draining`, `stuck`). |
| `eshu_dp_generation_liveness_recovered_total` | counter | Wedged active generations re-driven through projector re-enqueue by the liveness sweep. |
| `eshu_dp_generation_liveness_superseded_total` | counter | Orphaned older active generations superseded by the liveness sweep. |
| `eshu_dp_generation_liveness_failures_total` | counter | Generation liveness recovery sweep failures by bounded reason. |

The `eshu_dp_active_generations{age_bucket="stuck"}` series is the operator alarm
signal: a non-zero `stuck` count means generations are activating but not
completing, and none of their actionable outstanding shared intents sits in a
`projection_domain` queue that completed any intent inside
`ESHU_GENERATION_LIVENESS_PROGRESS_WINDOW`. The `draining` bucket counts the
same blocked generations while their domain queues are still moving; the sweep
leaves them alone and spends no recovery budget. A generation whose domain queue
never goes quiet stays `draining`; recover it by hand with
`POST /api/v0/admin/recover-generations` if it is genuinely wedged.
`ESHU_GENERATION_LIVENESS_PROGRESS_WINDOW` defaults to `10m` and is raised to
the sweep poll interval when set lower.

Each re-drive logs `generation liveness re-drove wedged generation` at Info
with `scope_id`, `generation_id`, `liveness_recovery_attempts`,
`reason="no_intent_progress_within_window"`, and `progress_window`. Skipped
(`draining`) generations are not logged individually; the gauge bucket is
their signal. Read it against the recovered and superseded counters to
separate self-healing from a backlog the sweep cannot clear; a rising
`eshu_dp_generation_liveness_failures_total` means the sweep itself is failing,
and the bounded failure reason lives in reducer logs.

## Activation Obligations

The activation obligation consumer (#7584,
`go/internal/reducer/maintenance/activation_obligation_runner.go`) settles the
exact-generation obligations `ProjectorQueue.Ack` writes. No scope or generation
id is ever a label; the per-obligation log line carries them.

| Metric | Type | Use |
| --- | --- | --- |
| `eshu_dp_activation_obligations` | gauge | Obligation rows by `status` (`pending`, `leased`, `completed`, `obsolete`, `inapplicable`), sampled once per consumer cycle. |
| `eshu_dp_activation_obligation_oldest_open_age_seconds` | gauge | Age of the oldest pending or leased obligation, sampled once per cycle. |
| `eshu_dp_activation_obligation_claim_age_seconds` | histogram | Obligation age when a consumer claimed it. |
| `eshu_dp_activation_obligation_finalize_total` | counter | Finalize attempts by `outcome` (`completed`, `phase_not_ready`, `work_pending`, `obsolete`, `inapplicable`, `not_owner`, `missing`, `lease_lost`, `error`). |
| `eshu_dp_activation_obligation_woken_total` | counter | `deployment_mapping` rows made visible by committed wakes. |
| `eshu_dp_activation_obligation_maintenance_duration_seconds` | histogram | Maintenance callback duration by `outcome` (`success`, `error`); its count is the callback count. |
| `eshu_dp_activation_obligation_catch_up_inserted_total` | counter | Obligations owed by catch-up to already-active generations missing their phase. |
| `eshu_dp_activation_obligation_pruned_total` | counter | Finished obligations deleted by the bounded prune. |
| `eshu_dp_activation_obligation_failures_total` | counter | Consumer step failures by `reason` (`claim`, `finalize`, `finalize_lock_timeout`, `maintenance`, `maintenance_timeout`, `catalog_changed`, `no_memo_baseline`, `closure_too_deep`, `catch_up`, `prune`, `stats`). The three hold reasons are held refusals, logged at Info, not errors. |

Alert on `eshu_dp_activation_obligation_oldest_open_age_seconds` above one epoch
whole-pass latency plus one consumer lease (default 2 minutes). A
`catalog_changed` hold is expected after any repository-catalog change and a
`no_memo_baseline` hold when no currently active partition holds a memo row
(before the first whole pass, or when the only memo-bearing scopes advanced
past their memos); for both
the consumer holds the lease and retries at lease cadence, runs no fallback
pass, and the epoch whole pass republishes the phase, so the obligation
completes on the next attempt after that pass. `closure_too_deep` means the
partition-scoped pass's dependent closure did not settle within its round
bound; it is also held until a whole pass publishes the phase. Past that bound, the epoch pass is not running or not reaching the
scope. `inapplicable` rows are expected for cloud and cluster scopes (no
repository fact) and for a repository whose repo_id another scope owns; they
are terminal and never reclaimed.

A rising `oldest_open_age_seconds` with a flat `finalize_total{outcome="completed"}`
means obligations are owed but not settling: read
`finalize_total{outcome="phase_not_ready"}` (the callback ran but the phase is
still absent) against `failures_total{reason="maintenance"}` (the callback
failed). `failures_total{reason="finalize_lock_timeout"}` (logged at Warn)
counts a Finalize that waited past its 1 s lock timeout behind a long
ingestion commit or Ack holding the same scope row: expected contention,
nothing is written, and the next claimer settles the obligation after its
lease. `reason="finalize"` (logged at Error) is every other Finalize failure.
A `no_memo_baseline` hold ages until the next commit triggers an epoch whole
pass: no partition-scoped retry can publish the phase before then. `work_pending` counts obligations held open while a handler for the
generation is still claimed or running, or while more than one wake batch (32
rows) waits. Each finalize logs `activation obligation finalized` at Info with
`scope_id`, `generation_id`, `outcome`, `woken` and `claim_token`; failures log
`activation obligation step failed` with
`failure_class=activation_obligation_<reason>`.

## Graph Orphan Sweep

| Metric | Type | Use |
| --- | --- | --- |
| `eshu_dp_graph_orphan_nodes` | observable gauge | Current zero-relationship node count by closed `node_label`, served from a background snapshot (up to `ESHU_GRAPH_GAUGE_REFRESH_INTERVAL` old). |
| `eshu_dp_gauge_snapshot_refreshes_total` | counter | Background graph-gauge snapshot refreshes by `gauge` and `outcome` (`success`, `error`, `timeout`). |
| `eshu_dp_gauge_snapshot_refresh_duration_seconds` | histogram | Duration of each background snapshot refresh by `gauge` and `outcome`. |
| `eshu_dp_gauge_snapshot_age_seconds` | observable gauge | Age of the snapshot each graph-backed gauge is serving; absent until the first successful refresh. |

The graph orphan sweep counts only the closed label set used by the reducer
cleanup path: `Repository`, `Platform`, and `EvidenceArtifact`. Counts are
capped by `ESHU_GRAPH_ORPHAN_SWEEP_COUNT_LIMIT` per label, so the gauge is a
dashboard signal, not an exact audit record. Sweep cycle logs carry per-label
counts, marks, deletes, duration, and `failure_class=graph_orphan_sweep_error`
without repository paths, resource identifiers, or generation ids.

## Shared Follow-Up And Acceptance

| Metric | Type | Use |
| --- | --- | --- |
| `eshu_dp_shared_projection_cycles_total` | counter | Shared projection partition cycles by domain and partition key. |
| `eshu_dp_shared_projection_intent_wait_seconds` | histogram | Maximum selected intent age for a partition cycle. |
| `eshu_dp_shared_projection_processing_seconds` | histogram | Graph-write and completion duration after partition selection. |
| `eshu_dp_shared_projection_step_seconds` | histogram | Shared projection substeps such as selection, load, retract, write, replay, and mark-completed. |
| `eshu_dp_shared_projection_stale_intents_total` | counter | Stale shared projection intents drained during processing, by `domain`, `runner`, and a closed `reason`: `acceptance_mismatch` (the intent's generation differs from the accepted generation for its acceptance key) or `generation_superseded` (the intent was blocked on a prerequisite phase row and its scope generation is superseded, so the row will never publish). A steady `generation_superseded` rate is orphan cleanup, not a stall. The shared projection log line `shared projection skipped intents until their prerequisite graph phase is committed` and its `blocked_intent_wait_seconds` cover only intents on generations that are not superseded, so a large wait there is a real prerequisite-phase stall. |
| `eshu_dp_shared_projection_partition_heartbeat_missed_total` | counter | Shared projection partition lease heartbeat failures by domain. A non-zero rate means a slow partition cycle's lease may be reclaimed by another worker while the original holder is still processing. |
| `eshu_dp_shared_projection_lane_blocked_total` | counter | Partition cycles a lane-wide gate held shut before lease claim (#7133). Labels: `domain` (`code_calls`, `repo_dependency`) and `reason` (`canonical_code_quiescence`, `reducer_graph_work_active`). A steady rate while that domain's pending intents stay flat is a wedged lane; the `code call projection lane blocked` warning names the blocking scope ids. |
| `eshu_dp_shared_projection_lane_blocking_scopes` | gauge | Scopes last seen holding the lane-wide gate, by `domain` and `reason`. The code-call runner samples it when a blocked episode starts and at most once a minute after that, and sets that reason's series to zero on release or when the lane switches to a different blocked reason; the `code call projection lane released` line records either close with the closed `blocked_reason` and `blocked_seconds`. |
| `eshu_dp_shared_acceptance_lookup_duration_seconds` | histogram | Shared acceptance lookup latency. |
| `eshu_dp_shared_acceptance_lookup_errors_total` | counter | Shared acceptance lookup failures. |
| `eshu_dp_shared_acceptance_upsert_duration_seconds` | histogram | Shared acceptance write latency. |
| `eshu_dp_shared_acceptance_upserts_total` | counter | Shared acceptance write volume. |
| `eshu_dp_shared_acceptance_stale_writes_total` | counter | Shared acceptance writes skipped because the stored row already carries a newer generation (ordered by `(generation_ingested_at, generation_id)`), by `domain` (`unknown` if a key maps to no intent). Non-zero means late or out-of-order acceptance writers; each skip kept the newer generation. |
| `eshu_dp_shared_acceptance_rows` | observable gauge | Durable shared acceptance row count. |
| `eshu_dp_shared_edge_write_groups_total` | counter | Shared edge write group volume (labels: bounded `domain`, `execution_mode` — `group`, `artifact-sequential`). |
| `eshu_dp_shared_edge_write_group_duration_seconds` | histogram | Shared edge write group latency (labels: bounded `domain`, `execution_mode`). |
| `eshu_dp_shared_edge_write_group_statement_count` | histogram | Statements per shared edge write group (labels: bounded `domain`, `execution_mode`). |
| `eshu_dp_shared_edge_target_miss_total` | counter | Shared edge write batches deferred on an absent graph target (label: bounded `domain`). |
| `eshu_dp_shared_edge_runs_on_retract_omissions_total` | counter | Impossible `RUNS_ON` retract roles omitted by bounded `domain` and `reason`; use the structured omission log for source and repository context. |
| `eshu_dp_code_call_edge_batches_total` | counter | Isolated code-call edge batch volume. |
| `eshu_dp_code_call_edge_batch_duration_seconds` | histogram | Isolated code-call edge batch latency. |

These metrics are domain-scoped. Use traces and logs when you need repository
or generation context.

## Storage And Graph Writes

| Metric | Type | Use |
| --- | --- | --- |
| `eshu_dp_postgres_query_duration_seconds` | histogram | Postgres query and exec latency from the instrumented wrapper, measured until `QueryContext` or `ExecContext` returns (not until the rows are read). Labeled by `operation` (`read` or `write`) and `store`, a fixed name per wired store (never a query string). `store="status_snapshot"` covers the API status snapshot reads behind the status, collector, readiness, index, and ingester routes (#6794); each read also emits a `postgres.query` span, and a caller-labeled read adds a bounded `db.query.summary` span attribute naming the read. |
| `eshu_dp_status_snapshot_read_duration_seconds` | histogram | Duration of each status snapshot read (`StatusStore.ReadStatusSnapshotFiltered`), one sample per read measured until the reader returns (rows scanned and decoded), labeled by `read` (closed set: `scope_counts`, `generation_counts`, `generation_transitions`, `active_work_summary`, `producer_activity`, `collector_generation_dead_letters`, `coordinator`, `registry_collectors`, `aws_cloud_scans`, `aws_freshness`, `infra_inventory`, `vulnerability_sources`, `collector_fact_evidence`, `terraform_state`, `semantic_extraction`, plus `active_work_summary_model` for the stored summary row read when `ESHU_STATUS_SUMMARY_READ_ENABLED` is on) and `outcome` (`success`, or `error` for any query, iteration, scan, or decode failure). Emitted by every process whose status store carries instruments, including the hosted runtimes and the API and MCP servers. Replaced `eshu_dp_status_stage_counts_cache_total`, retired with the stage-counts cache in #6794. |
| `eshu_dp_neo4j_query_duration_seconds` | histogram | Neo4j/NornicDB Bolt query latency. Logical reads use `operation="read"` and bounded `outcome` values: `success`, `slow`, `recovered`, `deadline`, `caller_deadline`, `unavailable`, `canceled`, or `error`. |
| `eshu_dp_iac_resource_list_duration_seconds` | histogram | Bounded IaC resource list (`GET /api/v0/iac/resources`) handler latency, labeled by `iac.kind`. |
| `eshu_dp_iac_resource_list_errors_total` | counter | Bounded IaC resource list handler errors, labeled by `iac.kind` and `reason`. |
| `eshu_dp_neo4j_deadlock_retries_total` | counter | Legacy graph-write retry counter labeled by bounded `write_phase` and `reason` (`connectivity_error`, `transient_error`, `write_conflict`, or `commit_unique_conflict`) for deadlocks, lock timeouts, driver connectivity failures, and retryable NornicDB commit conflicts. Repository, node, statement, and raw error values stay out of labels. |
| `eshu_dp_neo4j_batch_size` | histogram | Grouped graph write batch size. |
| `eshu_dp_neo4j_batches_executed_total` | counter | Grouped graph write batch execution volume. |
| `eshu_dp_canonical_writes_total` | counter | Canonical graph write batch volume. |
| `eshu_dp_canonical_write_duration_seconds` | histogram | Canonical graph/content write latency. |
| `eshu_dp_canonical_atomic_writes_total` | counter | Atomic canonical write attempts. |
| `eshu_dp_canonical_atomic_fallbacks_total` | counter | Atomic write fallbacks. |
| `eshu_dp_canonical_repository_retirements_total` | counter | Different-id `Repository` nodes the canonical writer retired because they still held the path it was re-projecting (`repository_cleanup` phase, the only statement allowed to delete a `Repository`; #7324). Labeled by `outcome`: `clean` (the backend deleted no relationships with it) or `dropped_relationships` (it deleted at least one). Counted from the backend's own write summary at most once per committed retirement. The last reported attempt wins, so a driver or executor retry does not double-count. If the retirement committed but the driver still returned an error, the retry's cleanup matches nothing, so that retirement is not counted. A projection whose retirement matched nothing (every steady-state retry) records nothing, and first-generation and delta projections never run it. The relationship count covers BOTH directions, including the retired node's own `REPO_CONTAINS`/`CONTAINS` edges (a live Neo4j retirement deleted 20: 11 incoming reducer edges plus 9 projector edges), so `dropped_relationships` does not by itself mean an incoming edge was lost. Splitting by direction and type needs a graph read and is a follow-up. Each increment pairs with a `canonical repository retired` log (WARN when relationships were deleted, INFO when clean) carrying `scope_id`, `generation_id`, `repo_id` (the repository being projected), `path`, `nodes_deleted`, `relationships_deleted`, `deletes_counted` and `outcome`. When the executor reported no write summary, whether anything was retired is unknown: the writer logs `canonical repository retirement not counted: executor reported no write summary` at DEBUG with `deletes_counted=false`, never the retired line, and moves no counter. The production projector, ingester and bootstrap Bolt executors always report one. A steady-state projection logs nothing at INFO or WARN. On NornicDB, whose Bolt summary counters are measured as unreliable, a real retirement can report zero deletes and so record nothing here. Any non-zero `dropped_relationships` rate means reducer edges other scopes wrote into the retired node are gone and nothing re-arms them. Trace the `path` to the repository re-key (see `docs/internal/design/7324-cross-scope-writer-rearm.md`). |
| `eshu_dp_canonical_repository_stubs_created_total` | counter | `Repository` nodes a shared-edge writer MERGE-created by id because no node held that id (#7446). The `repo_dependency` upserts (`DEPENDS_ON` and the typed repository relationships) and the `submodule_pin` `PINS_SUBMODULE` upsert keep MERGE-by-id on the target, so a write naming an id the path-conflict retirement removed re-creates a path-less stub (`path` null; a `repo_dependency` stub carries that writer's `evidence_source`, a `submodule_pin` stub has none). Labeled by `writer`: `repo_dependency` or `submodule_pin`. The value is the backend's own `NodesCreated` for those statements, counted once per committed execution unit (an atomic group or one auto-commit statement): a driver or executor retry re-reports every statement, and only the last attempt's entries count. A write whose targets all exist records nothing. If a stub committed but the driver still returned an error, the retry's MERGE matches the node that already exists and reports zero created, so that stub is not counted and not logged: a missing count can mean a retried commit, not a missing stub. Each non-zero statement pairs with a `canonical repository stub created` INFO log carrying `writer`, `evidence_source`, `nodes_created`, `batch_rows` and, for a one-row statement, `source_repo_id`, `target_repo_id`, `generation_id` with `attributed=true` (the created node is the target or, for the by-id source MERGE, the source, so the pair names the candidates); a multi-row statement lists up to 10 `source->target@generation` `candidates` with `attributed=false`, because the backend counts per statement, not per row. When the executor reported no write summary the writer logs `canonical repository stubs not counted: executor reported no write summary` at DEBUG with `stubs_counted=false` and moves no counter. The reducer's Bolt runner reports `NodesCreated` on Neo4j and NornicDB alike (#6783 measured it correct on both), so the counter emits on both backends; only Neo4j has a live proof (the live test is pinned to Neo4j in the ledger). With `ESHU_DIFFERENTIAL_CAPTURE` on, the capture recorder replaces the write-count collector below the edge writer, so nothing is counted and the DEBUG line fires. The stub is truthful while an owner asserts an edge into it and is reaped by the `Repository` orphan sweep once its owners retract by `evidence_source`. A sustained rate means writers keep naming retired ids: correlate with `eshu_dp_canonical_repository_retirements_total` (see `docs/internal/design/7324-cross-scope-writer-rearm.md`). |
| `eshu_dp_graph_oversized_index_keys_skipped_total` | counter | Graph node writes dropped before they reach the backend because a schema index key would exceed 8000 UTF-8 bytes (`graph.MaxIndexKeyBytes`, under Neo4j's range-index key limit), labeled by `node_label` and `property` (both from the Go-owned graph schema, so closed sets). Emitted by the canonical materialization guard (WARN `canonical row skipped: indexed value exceeds key size limit`) and by the statement guard every graph write passes through (WARN `graph write skipped: indexed value exceeds key size limit`, with `operation`, `repo_id`, `entity_id`, `file_path`, `key_bytes`, and a 64-byte `value_prefix`). It counts once per write attempt: a work-item retry that rebuilds the write counts the same node again, a driver-level transient retry does not. Any non-zero rate is a parser or collector bug emitting an unbounded identity value (#7058); the rest of the write still lands. |
| `eshu_dp_graph_index_key_guard_unanalyzed_total` | counter | Graph write attempts to a schema-indexed node label in a Cypher shape the oversized-index-key analyzer cannot read, labeled by `node_label` (a schema-indexed label) and `reason` (`unbound_label`, `unresolved_value`, or `unparsed_write`); both are closed sets. The rows are written as-is, so an oversized value in that shape would fail the Neo4j write unguarded. Counted once per write attempt by `InstrumentedExecutor`; the first attempt of each distinct statement also logs the WARN `graph write shape not analyzed by the index-key guard` with `operation`, `node_label`, `reason`, and a `statement_head` capped at 200 characters (never parameter values). Any non-zero rate means a writer took a shape the analyzer needs to learn (#7058). |
| `eshu_dp_canonical_projection_duration_seconds` | histogram | Canonical projection phase cost. |
| `eshu_dp_graph_write_backpressure_engaged_total` | counter | Graph writes that blocked for an in-flight permit (write-path backpressure engaged), labeled by operation and gate (`canonical` or `semantic`; the projector has a single pool and always reports `canonical`). |
| `eshu_dp_graph_write_backpressure_wait_seconds` | histogram | Time a graph write blocked waiting for an in-flight permit, labeled by operation and gate (`canonical` or `semantic`). |

Use graph/storage metrics before tuning NornicDB row caps, Neo4j batch sizes, or
worker counts. A non-zero `eshu_dp_graph_write_backpressure_engaged_total` rate
means the write path hit its concurrency ceiling and is slowing intake rather
than letting concurrent writes time out and flood the dead-letter queue; rising
`eshu_dp_graph_write_backpressure_wait_seconds` p95 is the precursor to write
timeouts. On the reducer, `ESHU_GRAPH_WRITE_CANONICAL_MAX_IN_FLIGHT` and
`ESHU_GRAPH_WRITE_SEMANTIC_MAX_IN_FLIGHT` (each falling back to
`ESHU_GRAPH_WRITE_MAX_IN_FLIGHT` when unset; issue #4448) size two independent
pools, so the two `gate` label values saturate independently — check both
before assuming the whole write path is bottlenecked.

## Correlation, Drift, And Relationship Work

| Metric | Type | Use |
| --- | --- | --- |
| `eshu_dp_correlation_rule_matches_total` | counter | Match-phase activity by rule pack and rule. |
| `eshu_dp_correlation_drift_detected_total` | counter | Admitted Terraform config/state drift by pack, rule, and drift kind. |
| `eshu_dp_correlation_drift_intents_enqueued_total` | counter | `config_state_drift` reducer intents ACTUALLY inserted (not attempted -- a value skipped by another producer's `ON CONFLICT DO NOTHING` is not counted), labeled `source=bootstrap_index` (bootstrap-index Phase 3.5), `source=ingester_runtime_trigger` (the ingester's runtime delta-trigger, fired when a `terraform_state_snapshot` scope generation activates outside a bootstrap-index pass, issue #5593), or `source=reducer_catch_up_sweep` (the reducer's bounded background re-scan of active `state_snapshot:*` scopes, default 5-minute interval, that converges a generation the runtime delta-trigger failed to enqueue; a nonzero value here means the runtime trigger or bootstrap missed a generation this sweep had to pick up). |
| `eshu_dp_config_state_drift_runtime_trigger_failures_total` | counter | The ingester's `config_state_drift` runtime delta-trigger failing to enqueue, labeled `outcome=trigger_error` (the downstream `Enqueue` call itself errored -- network/timeout/transient Postgres) or `outcome=bootstrap_wiring_rejected` (the trigger was wired on bootstrap-index's `ProjectorQueue`, which MUST NOT happen; the guard refused to fire and this is a wiring bug to fix, not a runtime condition to wait out). |
| `eshu_dp_correlation_orphan_detected_total` | counter | AWS runtime resources without Terraform-state backing. |
| `eshu_dp_correlation_unmanaged_detected_total` | counter | AWS/Terraform-state resources missing current Terraform config backing. |
| `eshu_dp_drift_unresolved_module_calls_total` | counter | Terraform module calls the drift loader could not resolve locally. |
| `eshu_dp_drift_schema_unknown_composite_total` | counter | State composite attributes dropped because provider schema coverage was missing or unsafe. |
| `eshu_dp_gcp_materialization_facts_total` | counter | GCP resource and relationship materialization input cardinality by reducer domain and fact kind. |
| `eshu_dp_gcp_materialization_graph_writes_total` | counter | GCP materialization graph-write cardinality by reducer domain and write kind (`node` or `edge`). |
| `eshu_dp_gcp_materialization_duration_seconds` | histogram | GCP materialization stage duration by reducer domain and write phase. |
| `eshu_dp_gcp_relationship_edges_total` | counter | GCP relationship edge outcomes by relationship type and join mode. |
| `eshu_dp_iac_reachability_rows_total` | counter | IaC usage rows materialized after projection drains. |
| `eshu_dp_iac_reachability_materialization_duration_seconds` | histogram | Corpus-wide IaC reachability materialization cost. |
| `eshu_dp_cross_repo_resolution_duration_seconds` | histogram | Cross-repo relationship resolution latency. |
| `eshu_dp_cross_repo_evidence_loaded_total` | counter | Evidence rows loaded for cross-repo resolution. |
| `eshu_dp_cross_repo_edges_resolved_total` | counter | Cross-repo edges resolved and routed for materialization by `relationship_type`. |
| `eshu_dp_cross_repo_edges_dropped_total` | counter | Cross-repo edges withheld at the ownership partition by `relationship_type` and bounded `reason` (`foreign_owned`). |
| `eshu_dp_deferred_backfill_batch_duration_seconds` | histogram | Wall time of each per-repository batch transaction inside the deferred backward-evidence backfill. Watch batch-by-batch progress instead of waiting for the whole pass. |
| `eshu_dp_deferred_backfill_batches_completed_total` | counter | Committed per-repository batches in the deferred backward-evidence backfill. Rising during a pass is the operator-visible backfill progress signal. |
| `eshu_dp_deferred_backfill_targeted_duration_seconds` | histogram | Wall time of one partition-scoped deferred maintenance pass (#7584) by pass `outcome`: `completed`, `suppressed` (every owed partition was inapplicable while the catalog guard would refuse, so the pass wrote nothing), a refusal (`catalog_changed`, `no_memo_baseline`, `closure_too_deep`), or `error`. Its sample count is the pass count. The pass reuses the whole pass's loader, batch and fan-in code with instruments off, so the whole pass's `eshu_dp_deferred_backfill_*` series do not count it. |
| `eshu_dp_deferred_backfill_targeted_outcomes_total` | counter | Owed-partition results of the partition-scoped pass by `outcome` (`published`, `not_active`, `inapplicable`, `retry`), one per owed partition and never per pass. A refused pass does not count its held owed partitions as `retry`; read refusals from the duration histogram's `outcome` (a rising `catalog_changed` or `no_memo_baseline` count means obligations wait for the next epoch whole pass). |
| `eshu_dp_deferred_backfill_targeted_reopened_total` | counter | Succeeded reducer work items the partition-scoped pass reopened, by `domain`. Correlation domains are reopened only in the affected partitions; the fleet-wide correlation replay stays on the epoch whole pass. |
| `eshu_dp_evidence_facts_discovered_total` | counter | Evidence facts discovered during ingestion. |
| `eshu_dp_iam_can_perform_edges_total` | counter | IAM CAN_PERFORM edges committed by bounded resolution mode. |
| `eshu_dp_iam_can_perform_skipped_total` | counter | IAM CAN_PERFORM catalog-action evaluations withheld by bounded skip reason. |
| `eshu_dp_iam_can_perform_conditioned_total` | counter | Condition-gated IAM CAN_PERFORM evidence classified by bounded confidence. |
| `eshu_dp_iam_can_perform_cross_scope_targets_total` | counter | IAM CAN_PERFORM exact target ARNs looked up in sibling AWS service scopes of the same account, by `outcome` (`resolved`, `unresolved`, `not_ready`, `scope_unregistered`, `abandoned`, `glob_local_only`). Emitted only by evaluations that commit, so it does not scale with readiness polls. `scope_unregistered` means the scope the ARN names (account, region, service) is not registered yet, so the intent waits after committing its ready edges. `abandoned` means the target was committed as unresolved because its wait settled. A rising `abandoned` or `not_ready` rate means target scopes are stuck, not that policies lost grants. |
| `eshu_dp_incident_routing_evidence_total` | counter | PagerDuty incident-routing graph evidence outcomes by reducer domain, outcome, source class, and slot kind. |

No-Regression Evidence: #2409 adds nil-safe OTEL counter/histogram recording to
the existing GCP materialization handlers without changing queue claims, graph
writers, Cypher, worker counts, or terminal row counts. Baseline graph-write
shape stays `gcp_cloud_resource` 2 facts -> 1 node row and
`gcp_cloud_resource` 2 facts + `gcp_cloud_relationship` 2 facts -> 1 edge row;
after measurement is `go test ./internal/reducer -run
'TestImplementedDefaultDomainDefinitionsWiresGCP(Resource|Relationship)MaterializationInstruments|TestGCP(Resource|Relationship)MaterializationRecordsPrometheusSignals|TestGCPMaterialization(SkipsNoOpGraphWriteDurations|SignalsReachPrometheusExposition)|TestGCPRelationshipMaterializationMetricCarriesRelationshipTypeAndJoinMode'
-count=1` on the in-memory OTEL SDK manual reader and the same Prometheus
handler mounted by Compose runtimes, with NornicDB/Neo4j backend behavior
unchanged. Observability Evidence: the same test proves
`eshu_dp_gcp_materialization_facts_total`,
`eshu_dp_gcp_materialization_graph_writes_total`, and
`eshu_dp_gcp_materialization_duration_seconds` reach `/metrics` with bounded
`domain`, `fact_kind`, `kind`, and `write_phase` labels; no-op graph-write and
first-generation retract phases do not dilute duration histograms.
`eshu_dp_gcp_relationship_edges_total` now matches AWS edge telemetry by
emitting bounded `relationship_type` and `join_mode` labels. Completion logs
remain available for exact scope/generation diagnosis.

`eshu_dp_drift_unresolved_module_calls_total` uses the closed reasons
`external_registry`, `external_git`, `external_archive`, `cross_repo_local`,
`cycle_detected`, `depth_exceeded`, and `module_renamed`.

## Package, Image, CI/CD, And Supply Chain Correlation

| Metric | Type | Use |
| --- | --- | --- |
| `eshu_dp_package_source_correlations_total` | counter | Package source-correlation decisions by reducer domain and outcome. |
| `eshu_dp_package_consumption_repo_edges_total` | counter | Repo-to-repo `DEPENDS_ON` edge intents derived from package consumption-to-owner correlations, by reducer `domain` (`repo_dependency`) and `outcome` (`projected` for emitted edges, `skipped_no_owner` for consumers whose packages resolve no owner and instead emit a refresh/retraction). |
| `eshu_dp_code_import_repo_edges_total` | counter | Repo-to-repo `DEPENDS_ON` edge outcomes derived from per-file external import sources correlated to package-registry ownership (evidence_source `projection/code-imports`), by reducer `domain` (`repo_dependency`) and `outcome` (`considered`, `written`, and the conservative skip reasons `skipped_relative`, `skipped_unresolved`, `skipped_ambiguous`, `skipped_no_owner`, `skipped_self`). |
| `eshu_dp_container_image_identity_decisions_total` | counter | Container image identity decisions by reducer domain and outcome. |
| `eshu_dp_container_image_identity_retirements_total` | counter | Container image identity retirement safety outcomes by reducer domain and bounded outcome: `retirement_attempted`, `legacy_deleted`, `held_config_blob_unavailable`, `held_tag_list_truncated`, or `held_missing_manifest_digest`. An attempted retirement can still lose the fact-store freshness fence, so do not interpret it as a committed tombstone. |
| `eshu_dp_ci_cd_run_correlations_total` | counter | CI/CD run correlation decisions by reducer domain and outcome. |
| `eshu_dp_service_catalog_correlations_total` | counter | Service catalog correlation decisions by reducer domain and outcome. |
| `eshu_dp_service_catalog_correlation_guardrails_total` | counter | Service catalog correlation guardrail events by reducer domain and bounded guardrail. |
| `eshu_dp_search_decay_policy_applications_total` | counter | Search decay scoring decisions by policy id, evidence class, and outcome. |
| `eshu_dp_sbom_attestation_attachments_total` | counter | SBOM and attestation attachment decisions by reducer domain and outcome. |
| `eshu_dp_supply_chain_impact_findings_total` | counter | Supply-chain impact findings by reducer domain and outcome. |
| `eshu_dp_supply_chain_impact_findings_retracted_total` | counter | Superseded supply-chain impact finding rows a reducer pass tombstoned because its complete finding set for the (scope, generation) no longer derives them, by reducer domain. |
| `eshu_dp_supply_chain_impact_evidence_truncated_total` | counter | Supply-chain impact evidence truncations, one increment per pass per reason, for a cause that can hide a live finding so the pass retracted nothing, by reducer domain and reason (`active_expansion_rounds` or `evidence_budget`). A scope that increments it on every pass keeps its stale findings; the WARN log line carries the scope and generation. |
| `eshu_dp_supply_chain_impact_write_superseded_total` | counter | Supply-chain impact passes rejected at write admission because a fresher pass (a higher fencing token) was already admitted for the same scope and generation, by reducer domain. The pass fails retryable and non-counting and the queue re-runs it with a fresher token; a scope that stays superseded means two workers keep overtaking each other, or the sequence `supply_chain_impact_fencing_token_seq` lags the admitted watermark after a restore or manual reset (repair: stop every reducer replica, run the seed in migration 154 by hand, then restart them; the seed is not live-safe, and the sequence must never be reset on its own). |

Package names, image digests, run IDs, commit SHAs, environment names, and
artifact identifiers stay in logs, traces, or durable facts.

## Documentation Truth

| Metric | Type | Use |
| --- | --- | --- |
| `eshu_dp_documentation_entity_mentions_extracted_total` | counter | Documentation entity mention extraction by source system and outcome. |
| `eshu_dp_documentation_claim_candidates_extracted_total` | counter | Non-authoritative claim candidates after exact subject resolution. |
| `eshu_dp_documentation_claim_candidates_suppressed_total` | counter | Claim candidates intentionally suppressed before exact finding emission. |
| `eshu_dp_documentation_drift_findings_total` | counter | Read-only documentation drift findings by outcome. |
| `eshu_dp_documentation_drift_generation_duration_seconds` | histogram | Documentation drift finding generation latency. |

Ambiguous or unmatched mention outcomes usually point to catalog or alias
problems, not writer problems.

## Capacity And Memory

| Metric | Type | Use |
| --- | --- | --- |
| `eshu_dp_pipeline_overlap_seconds` | histogram | Overlap between major pipeline phases. |
| `eshu_dp_gomemlimit_bytes` | observable gauge | Effective Go memory limit exposed at startup. |

Use `eshu_dp_pipeline_overlap_seconds` when parallelism increases memory overlap
or storage contention. Use `eshu_dp_gomemlimit_bytes` with container RSS to
decide whether a service is undersized, over-concurrent, or missing cgroup-based
memory limit detection.
