# Resolution Engine

Use this page for the reducer runtime boundary, concurrency model, and operator
signals. Helm lane values live in
[Helm Runtime Values](../deploy/kubernetes/helm-runtime-values.md).

The resolution engine claims durable work, runs reducer domains, drains shared
projection lanes, writes canonical graph/read-model truth through configured
storage ports, and records retry, failure, and completion state.

| Runtime | Value |
| --- | --- |
| Binary | `/usr/local/bin/eshu-reducer` |
| Docker service name | `resolution-engine` |
| Kubernetes shape | `Deployment` |
| Source | `go/cmd/reducer/`, `go/internal/reducer/` |

## Runtime Flow

```text
start telemetry
  -> open Postgres and graph backend
  -> build DefaultRuntime domain handlers
  -> build reducer queue
  -> start main reducer loop
  -> start shared projection, code-call, and repo-dependency runners
  -> start bounded generation-retention cleanup runner
  -> start bounded graph orphan cleanup runner
```

The main loop claims reducer intents, dispatches workers, heartbeats
long-running work, and acks, retries, or fails work. Shared and dedicated
projection runners acquire leases, wait for accepted generation/readiness
state, write or retract edges, and mark intents processed.
Claim-path performance and correctness changes are gated by the
[Reducer Claim-Latency Gate](../reference/reducer-claim-latency-gate.md).
Code graph conflict-key changes must satisfy the
[Code-Graph Sub-Scope Partitioning](../reference/code-graph-subscope-partitioning.md)
contract before claiming intra-repo reducer concurrency.

The generation-retention runner prunes superseded source-generation history in
bounded Postgres transactions. It never retracts graph truth; relationship
retraction and graph orphan cleanup remain separate reducer work. Retention
events store safe hashes for scope and generation identifiers so changed-since
requests can return `retention_expired` instead of a false zero delta after
history ages out. Production Helm renders and default/production binaries reject
`ESHU_GENERATION_RETENTION_ENABLED=false`; use that disable flag only with an
explicit local `ESHU_QUERY_PROFILE` for local or test binary runs.

Each retention transaction runs `SET LOCAL work_mem = '64MB'` before its first
statement. Nothing else in the reducer sets `work_mem`, and a Helm deployment
against an external Postgres gets that server's value, commonly the 4MB default.
At 4MB the three content prunes sort on disk and run slower than they need to; at
16MB and above they plan a hash aggregate (measured at 5x on a remote host,
PostgreSQL 18.6). The setting is transaction-local: it does not change the pooled
connection or the server configuration, and there is no environment variable for
it.

`work_mem` is a per-plan-node allowance, not a per-statement budget. Each sort or
hash node in a retention statement may use up to 64MB, a hash node up to twice that
(`hash_mem_multiplier`), and one statement can contain several such nodes, so size
memory headroom for the whole retention transaction, not for one figure per
statement. The row-count statement that opens each batch has not been timed warm
at 5x on the remote host. On a laptop cold shape (10 generations of 6,000 keys, no
planner statistics) its two grouping sorts spill to disk at 4MB and stay in memory
at 64MB. The earlier figure of about 4.9 s warm at 5x measured the statement it
replaced (issue #6809), not the current one.

The graph orphan cleanup runner counts, marks, and deletes only aged
zero-relationship graph nodes in the closed cleanup label set. It is not a
substitute for relationship retraction or canonical node replacement: first it
marks disconnected nodes, then it deletes only nodes that remain disconnected
after the configured TTL, and it clears the marker when a relationship returns.
Repository cleanup excludes source-local canonical repository nodes, so an empty
but active repository is not treated as an orphan. Each cleanup cycle first
claims a single Postgres partition lease, so scaled reducer deployments do not
run duplicate label-wide graph mutations at the same time.

### Changed-since link domain (dark)

The `changed_since_link` domain (#7127) keeps a changed-since ledger (migration
136). Each generation activation becomes a link: the keys that changed since
the scope's last linked generation. A later release answers `get_changed_since`
from these links. Until then nothing reads the ledger. The domain is off by
default. With `ESHU_CHANGED_SINCE_LINK_ENABLED` unset or `false` it builds no
runner and the link domain issues no SQL.

Generation retention prunes the ledger in the same transaction that prunes a
generation, with one statement. It deletes the links whose generation or prior
generation is pruned, with their link deltas and bucket counts, and the
activation rows of the pruned generations. It does this whatever the link
switch says, so rows written while it was on are still pruned after it is
off. The key-state table and the cursor describe the active generation and
are never pruned.

Ledger rows count toward `ESHU_GENERATION_RETENTION_BATCH_ROW_LIMIT`. The
limit caps a batch of two or more generations. A generation over it only
because of its ledger rows is pruned in a batch of its own, which may exceed
the limit by that generation's ledger rows and nothing else; while another
batch is being filled it is deferred with reason `row_limit_ledger` and leads
a later batch. Each such batch adds one to
`eshu_dp_generation_retention_over_limit_batches_total`, and the cycle log
reports `rows_over_batch_row_limit`. A generation whose rows outside the
ledger exceed the limit is still skipped with reason `row_limit` (ADR #2248).
A batch of one over the limit holds one scope row and one generation row
while it deletes: it gives back the other rows its selection locked before the
delete (a savepoint rollback) and re-locks only its own. The cycle log's
`locked_scope_rows` counts the scope rows held by the pruned batch, not the
selection's full lock set: in a general batch the selection can also hold
other scope rows it locked while choosing, up to `BatchGenerationLimit`.

The measured size of such a batch, with the `shared_buffers` it was measured
under: PostgreSQL 18.6 in a container on an 18-CPU host with at least half its
CPUs idle throughout every run (at its start, its end and its in-run peak), each
run paired with a control run of the previous code, in the same round, that
finished within 2 s; six valid rounds per size; the retention transaction's
`work_mem` of 64MB and `BatchRowLimit` 100,000. Cells give the median and worst
hold. The Docker Compose default inherits the second row.

| `shared_buffers` | 771,201-row link | 1,542,402-row link |
| --- | --- | --- |
| 2GB (the link-delta table fits) | 1.11 s median, 1.31 s worst | 2.90 s median, 3.53 s worst |
| 128MB (Postgres default, which the Docker Compose stack runs) | 1.67 s median, 1.88 s worst | 4.76 s median, 5.78 s worst |

The delete's buffer work is exactly linear: 4.06 buffer touches per deleted
row at both sizes. It also writes WAL, mostly full-page images of the heap
pages it touches: up to about 300 bytes per deleted row (286-301 measured, about
0.45 GB, at 1.54M rows), so on any deployment the hold lasts at least as long
as writing that WAL takes; how much of the measured time that is has not been
isolated. On the measurement host the hold grew 2.62 times at 2GB and 2.85
times at 128MB for twice the rows, close on both (the
standard deviation of their same-round ratios is 0.34-0.41), so the excess
over linear is not mainly the buffer cache. The smaller runs wrote fewer
full-page images per row (68-216 bytes of WAL per row); the cause of the
excess was not isolated. On a CPU-saturated host (load at or above the CPU
count), runs that the rule above excludes took several times the quiet-host
figures with identical buffer work (measured before #7279 changed the
retention transaction).

The envelope has an edge. At 1,542,402 rows the delete's in-memory list of
row ids uses 61,308 kB of the transaction's 65,536 kB (64MB) `work_mem`. A
link of about 1.65 million rows or more spills that list to temporary files,
which has not been measured. A scope with more than about 770k keys can write
a link of that size.

While a batch of one holds its scope row, the projector's Ack for that scope
waits in 2 s slices and retries (about 300 s of tolerance), which shows as a
rise in `eshu_dp_projector_ack_deferrals_total` and
`eshu_dp_projector_ack_wait_seconds` during a retention cycle; the ingester's
commit for that scope waits, and so does the projector's failure path, which
has no lock timeout. The wait ends when the retention transaction commits.
Watch `eshu_dp_generation_retention_rows_pruned_total{table="changed_since_link_deltas"}`
and the `eshu_dp_changed_since_deltas_bytes` gauge: a delta table that grows
while retention prunes nothing from it means retention is not keeping up.
No ledger row outlives a generation it names: the link writer locks both
generations a link names, so retention skips them until the link commits.
When a link's prior was already pruned, the writer rebases instead: it moves
the state by the same diff, records a root link, and reports the
`prior_pruned` chain break. `eshu_dp_changed_since_ledger_orphans` watches
this and stays at zero, apart from a deleted scope until its purge.

A cycle does four things:
- It journals active generations that have no activation row.
- It backfills retained chains for scopes that have no journal rows.
- It links each scope's backlog, with at most
  `ESHU_CHANGED_SINCE_LINK_WORKERS` scopes at once.
- It deletes the ledger rows of deleted scopes.

A link transaction takes its scope's cursor row, then its generation row, then
for a full link the generation it links from (the prior), then one of
`ESHU_CHANGED_SINCE_LINK_SLOTS` database-wide advisory slots. The cursor lock
and the slot never wait. Each generation lock waits at most 250 ms: PostgreSQL
can wait on a row's update chain despite `SKIP LOCKED`, so the link sets a
transaction-local `lock_timeout` and a timeout is the non-counting
`generation_lock_timeout`. A miss rolls back and is retried on the next cycle.
The journal pass's backfill runs under the same bound. A
full link reads the activating generation once at `work_mem` 256MB (backend
memory up to about 1 GiB) under `ESHU_CHANGED_SINCE_LINK_STATEMENT_TIMEOUT`.

A delta generation is recorded as a chain break and the scope keeps its
state. The overlay link for delta generations is not shipped.

A lock miss never counts. A failure once the link statement ran
(`statement_timeout`, `connection_lost`, `sql_error`, `internal`) is counted on
the scope's cursor and retried after min(30 min, 30 s × 2^(n-1)). At
`ESHU_CHANGED_SINCE_LINK_MAX_ATTEMPTS` (default 5) the activation becomes a
`link_poisoned` chain break: the scope keeps its state, is marked poisoned, and
links again at its next full generation.

### Activation obligations (#7584, off by default)

A quiet repository generation can be activated by `ProjectorQueue.Ack` after
the ingester's last deferred-maintenance pass already ran. Nothing then
publishes that generation's own `backward_evidence_committed` phase, and its
`deployment_mapping` rows wait on the not-ready retry schedule. `Ack` now writes
one `activation_obligations` row for the generation it activates, inside the Ack
transaction (migration 160; `go/internal/storage/postgres/activation`).

`maintenance.ActivationObligationRunner` is the consumer. Each worker claims
one obligation (`FOR NO KEY UPDATE SKIP LOCKED`, lease owner and a claim token
on the database clock), finalizes it, calls the `ActivationMaintainer` port only
when the exact generation's phase is missing, and finalizes again. Finalize
locks the scope row before the obligation row (the Ack lock order), retires the
obligation as `obsolete` when the scope's pointer moved to another generation
or is NULL (the generation failed), retires it as `inapplicable` with one index
seek when the phase is absent and the generation has no repository fact (cloud
and cluster scopes), wakes at
most 32 waiting `deployment_mapping` rows of that exact generation per call,
keeps the obligation open while a handler for the generation is claimed or
running, and completes it under the lease fence. One worker per process also
runs a bounded catch-up page (active repository generations with neither an
obligation nor their phase), a bounded prune of finished rows and the census
gauges each cycle. Replicas and workers never share an obligation. The
maintenance port can answer `ErrActivationInapplicable` (no repository maps to
the owed partition: the row retires `inapplicable` after one callback) or
`ErrActivationCatalogChanged` (held: the lease stays, the retry comes at lease
cadence, no fallback pass runs, and the epoch whole pass triggered by the
catalog-changing commit publishes the phase). `inapplicable` rows are never
pruned.

The consumer starts only when `ESHU_ACTIVATION_OBLIGATION_CONSUMER_ENABLED=true`
(default `false`). Its maintainer is the partition-scoped deferred maintenance
pass on the obligation's own (scope, generation)
(`postgres.ActivationMaintainer`); the whole-corpus pass stays on the ingester's
drain epoch and is never run by the consumer. A pass refusal of
`catalog_changed`, `no_memo_baseline` or `closure_too_deep` is a hold under that
reason. While the consumer is off, obligations stay `pending`, one per activated
generation, and generation retention deletes them with their generation
(`ON DELETE CASCADE`).

The wake writes `visible_at` on the database clock, while the reducer's claim
compares it with the reducer host's clock. A database clock running ahead of
the host by δ delays a woken row by δ, and the claim-age histogram is off by
the same δ. Both are bounded by NTP sync. Migration 160 must be applied
(`eshu-bootstrap-data-plane`) before projector, ingester or bootstrap-index
binaries that write obligations start; see the activation package README
"Rollout order".

## Domains And Projection

The default runtime processes workload identity, deployable-unit correlation,
cloud asset resolution, deployment mapping, workload materialization, code-call
materialization, semantic entity materialization, SQL relationships,
inheritance, Terraform config-vs-state drift, package source correlation,
container image identity, CI/CD run correlation, SBOM/attestation attachment,
supply-chain impact, and AWS runtime drift domains.

Shared projection is split across:

- `SharedProjectionRunner` for partitioned canonical edge domains
- `CodeCallProjectionRunner` for high-volume `code_calls`
- `RepoDependencyProjectionRunner` for source-repo dependency projection

The partitioned runner handles `platform_infra`, `workload_dependency`,
`sql_relationships`, and `inheritance_edges`.

## Concurrency Model

- The main reducer loop is concurrent by default. NornicDB uses `NumCPU`
  workers and a claim window equal to workers. Neo4j uses `min(NumCPU, 4)`
  workers and a larger bounded claim window.
- `SharedProjectionRunner` uses up to `min(NumCPU, 4)` partition workers by
  default. Tune `ESHU_SHARED_PROJECTION_WORKERS` only when telemetry proves
  shared projection is the bottleneck.
- Each shared-projection and code-call process owner includes hostname, PID,
  and a boot nonce. `ESHU_SHARED_PROJECTION_LEASE_OWNER` and
  `ESHU_CODE_CALL_PROJECTION_LEASE_OWNER` change the readable prefix, not the
  process suffix. A restarted reducer therefore waits for an active dead-owner
  lease to expire instead of renewing it as the same owner.
- The main loop, shared projection runner, code-call runner, and
  repo-dependency runner run as concurrent goroutines inside `Service.Run()`.
- Repo-dependency projection accepts `1`, `2`, or `4` fixed acceptance-unit
  shards. A source repository stays on one shard, while unrelated repositories
  can overlap. Each process owner includes hostname, PID, and a boot nonce.
  Unsafe timing fails startup unless the `5m` lease exceeds the `45s` cycle
  deadline plus `ESHU_CANONICAL_WRITE_TIMEOUT` and `30s`. An error, cycle
  deadline, heartbeat failure, or ambiguous commit quarantines only that
  shard until lease expiry. A process shutdown that interrupts a cycle before
  its acceptance-unit transaction opened releases the lease immediately;
  after that point the quarantine holds, since the commit outcome is unknown.
- `ESHU_REPO_DEPENDENCY_RETRACT_STATEMENT_TIMING` is retained for
  compatibility but no longer changes behavior: repo-dependency retracts
  always execute their three role statements
  (`repository_relationship_edges`, `runs_on_relationships`,
  `evidence_artifacts`) sequentially, each in its own transaction with a
  per-statement timing log, because grouped DELETEs under-apply on NornicDB
  v1.1.11.
- The generation-retention runner runs beside those loops and relies on
  Postgres row locks plus bounded batch and row limits. Do not reduce reducer
  worker counts to make retention safe.
- The graph orphan cleanup runner runs beside those loops with bounded per-label
  graph writes and no reducer worker-count change. Do not use lower worker
  counts or batch-size `1` as a substitute for idempotent relationship
  retraction.
- Queue rows carry `conflict_domain` and `conflict_key`; claim SQL fences only
  rows sharing an active durable conflict key so unrelated work can overlap.

Do not lower worker counts as a shipped fix for non-idempotent writes or graph
MERGE races. Diagnose the conflict key, retry, and write path.

Retry scheduling uses exponential backoff plus jitter, not a fixed delay:
`visible_at = now + ESHU_REDUCER_RETRY_DELAY*(1<<attempt) +
rand(0, ESHU_REDUCER_RETRY_DELAY*ESHU_REDUCER_RETRY_JITTER_FRACTION)`, capped
at `ESHU_REDUCER_MAX_RETRY_DELAY`. A fixed delay let many work items that fail
at the same instant reconverge on the identical `visible_at` and self-reinforce
into a retry storm that starves new work; the exponential term and jitter both
exist to break that synchronization. `eshu_dp_reducer_retry_surge_total`
(labeled by `failure_class`) tracks the rate of scheduled retries.

`failure_class="generation_activation_not_ready"` on that counter means a
reducer intent was claimed for a newer generation before the projector
acknowledged it, so the generation is still `pending` while an older one is
active. The intent is not superseded. It waits for the projector's Ack and then
runs its handler. This class does not count toward `ESHU_REDUCER_MAX_ATTEMPTS`
and never dead-letters: `attempt_count` stays frozen, so the retry delay stays
constant at about twice `ESHU_REDUCER_RETRY_DELAY` plus jitter. Such rows sit
in `retrying` with this `failure_class` and should drain within one projector
window, once the generation activates, fails, or is superseded by a newer one.
A count that persists for the same scope means a generation is stuck `pending`
with no projector work making progress; check that scope's projector work item
and `scope_generations` row.

`failure_class="shared_edge_target_not_ready"` means a
`deployable_unit_correlation` edge write found an endpoint `Repository` node
that another scope has not committed to the graph yet. It is also
non-counting, with the same frozen `attempt_count` and constant retry delay,
because the node arrives later with no ordering against this intent (#7268).
The wait is bounded by elapsed time, not attempts: 30 minutes after the
intent's repair cycle began (`COALESCE(reopened_at, created_at)`), the handler
logs `shared edge target absent past the wait bound` on the reducer's
structured logger with `elapsed_since_cycle_start` and `max_wait`, and fails
with a counting error.
From then on the row spends `ESHU_REDUCER_MAX_ATTEMPTS` and dead-letters, so a
target that never appears fails loudly. A failing existence probe is not a
readiness miss. It counts from the first attempt.
`workload_materialization_deployment_source_target_not_ready` follows the same
rule for the workload materializer's deploy `Repository` target (#6759).

## Configuration

Important env vars:

- `ESHU_REDUCER_RETRY_DELAY`
- `ESHU_REDUCER_MAX_ATTEMPTS`
- `ESHU_REDUCER_MAX_RETRY_DELAY`
- `ESHU_REDUCER_RETRY_JITTER_FRACTION`
- `ESHU_REDUCER_WORKERS`
- `ESHU_REDUCER_BATCH_CLAIM_SIZE`
- `ESHU_REDUCER_SEMANTIC_ENTITY_CLAIM_LIMIT`
- `ESHU_SHARED_PROJECTION_WORKERS`
- `ESHU_SHARED_PROJECTION_PARTITION_COUNT`
- `ESHU_SHARED_PROJECTION_POLL_INTERVAL`
- `ESHU_SHARED_PROJECTION_LEASE_TTL`
- `ESHU_SHARED_PROJECTION_LEASE_OWNER`
- `ESHU_SHARED_PROJECTION_BATCH_LIMIT`
- `ESHU_CODE_CALL_PROJECTION_ACCEPTANCE_SCAN_LIMIT`
- `ESHU_CODE_CALL_PROJECTION_PARTITION_COUNT`
- `ESHU_CODE_CALL_PROJECTION_WORKERS`
- `ESHU_REPO_DEPENDENCY_PROJECTION_WORKERS`
- `ESHU_REPO_DEPENDENCY_PROJECTION_LEASE_TTL`
- `ESHU_REPO_DEPENDENCY_PROJECTION_CYCLE_TIMEOUT`
- `ESHU_REPO_DEPENDENCY_PROJECTION_LEASE_OWNER`
- `ESHU_REPO_DEPENDENCY_RETRACT_STATEMENT_TIMING`
- `ESHU_GENERATION_RETENTION_ENABLED`
- `ESHU_GENERATION_RETENTION_POLL_INTERVAL`
- `ESHU_GENERATION_RETENTION_MIN_SUPERSEDED_GENERATIONS`
- `ESHU_GENERATION_RETENTION_MAX_SUPERSEDED_AGE`
- `ESHU_GENERATION_RETENTION_HARD_MAX_SUPERSEDED_AGE` (default `2160h`; hard history ceiling — ordinary superseded history older than this is eligible even within the retained count; unset resolves to `2160h`, or to `ESHU_GENERATION_RETENTION_MAX_SUPERSEDED_AGE` when that is longer; an explicit value below the soft window is rejected at startup)
- `ESHU_GENERATION_RETENTION_BATCH_GENERATION_LIMIT`
- `ESHU_GENERATION_RETENTION_BATCH_ROW_LIMIT`
- `ESHU_CHANGED_SINCE_LINK_ENABLED` (default `false`; dark domain, see
  above), `ESHU_CHANGED_SINCE_LINK_SLOTS` (default `2`),
  `ESHU_CHANGED_SINCE_LINK_STATEMENT_TIMEOUT` (default `120s`),
  `ESHU_CHANGED_SINCE_LINK_POLL_INTERVAL` (default `5s`),
  `ESHU_CHANGED_SINCE_LINK_MAX_ATTEMPTS` (default `5`),
  `ESHU_CHANGED_SINCE_LINK_WORKERS` (default `4`),
  `ESHU_CHANGED_SINCE_LINK_BACKFILL_SCOPES_PER_CYCLE` (default `10`)
- `ESHU_INFRA_INVENTORY_RECONCILE_ENABLED` (default `true`). The loop is also
  the only thing that repairs rolling-upgrade fence marks; with it off, one
  write from an older binary or manual SQL keeps unscoped infra aggregate reads
  on the graph (`eshu_dp_infra_inventory_dirty_repos`, admin status
  `infra_inventory`).
- `ESHU_INFRA_INVENTORY_RECONCILE_INTERVAL` (default `5m`, wait between cycles)
- `ESHU_INFRA_INVENTORY_RECONCILE_REPO_BUDGET` (default `500`, repositories per cycle)
- `ESHU_STATUS_SUMMARY_WRITER_ENABLED` (default `false`): run the periodic
  status summary writer (#7009), which stores the active-work summary in
  `status_summary_snapshots`. One replica computes per tick under a
  transaction advisory lock; the others skip.
- `ESHU_STATUS_SUMMARY_WRITER_INTERVAL` (default `10s`, minimum `5s`; values
  below `5s` or unparsable values fail startup)
- `ESHU_GRAPH_ORPHAN_SWEEP_ENABLED`
- `ESHU_GRAPH_ORPHAN_SWEEP_POLL_INTERVAL`
- `ESHU_GRAPH_ORPHAN_SWEEP_LEASE_OWNER`
- `ESHU_GRAPH_ORPHAN_SWEEP_LEASE_TTL`
- `ESHU_GRAPH_ORPHAN_SWEEP_TTL`
- `ESHU_GRAPH_ORPHAN_SWEEP_BATCH_LIMIT`
- `ESHU_GRAPH_ORPHAN_SWEEP_COUNT_LIMIT`
- `ESHU_CODE_VALUE_FLOW_STALE_CLEANUP_ENABLED`
- `ESHU_CODE_VALUE_FLOW_STALE_CLEANUP_POLL_INTERVAL`
- `ESHU_CODE_VALUE_FLOW_STALE_CLEANUP_LEASE_OWNER`
- `ESHU_CODE_VALUE_FLOW_STALE_CLEANUP_LEASE_TTL`
- `ESHU_CODE_VALUE_FLOW_STALE_CLEANUP_SCOPE_BATCH_LIMIT`
- `ESHU_CODE_VALUE_FLOW_STALE_CLEANUP_DELETE_BATCH_LIMIT`
- `ESHU_REDUCER_HANDLES_ROUTE_PRESENCE_GATE_ENABLED`

`ESHU_REDUCER_HANDLES_ROUTE_PRESENCE_GATE_ENABLED` defaults to `true` and gates
the symbol→runtime edges on their target having committed, so they cannot drop on
a cold first generation: `Function-[:HANDLES_ROUTE]->Endpoint` on its target
`(repo_id, path)` `:Endpoint` (#2809), and `Function-[:RUNS_IN]->Workload` on the
repo having a committed `:Workload` (#2855). Both share one presence store and
this one flag. It is independent of
`ESHU_REDUCER_SECRETS_IAM_GRAPH_PROJECTION_ENABLED`: the secrets/IAM flag never
enables or disables these gates, and this kill switch never widens uid presence
writes onto the cloud/Kubernetes materializers. Set it to a false value to restore
the pre-gate behavior. A handler whose target never materializes (a route with no
endpoint, or a repo with no workload) is drained with no edge
(`terminal_no_endpoint` with its `domain` in the shared projection cycle log),
never deferred, so the backlog cannot stall.

Raise `ESHU_CODE_CALL_PROJECTION_ACCEPTANCE_SCAN_LIMIT` only after the reducer
reports the explicit acceptance-cap failure and discovery evidence shows the
repo is dominated by authored source that should remain indexed. Do not use it
for graph write deadlines, slow canonical phases, or ordinary backlog.

## Telemetry

Start with:

- spans: `reducer.run`, `canonical.write`
- histograms: `eshu_dp_reducer_run_duration_seconds`,
  `eshu_dp_canonical_write_duration_seconds`,
  `eshu_dp_queue_claim_duration_seconds{queue=reducer}`
- counters: `eshu_dp_reducer_executions_total`,
  `eshu_dp_shared_projection_cycles_total`,
  `eshu_dp_generation_retention_generations_pruned_total`,
  `eshu_dp_generation_retention_rows_pruned_total`,
  `eshu_dp_generation_retention_failures_total`,
  `eshu_dp_generation_retention_skipped_total`
- retention histograms: `eshu_dp_generation_retention_duration_seconds`,
  `eshu_dp_generation_retention_batch_size`,
  `eshu_dp_generation_retention_oldest_eligible_age_seconds`,
  `eshu_dp_generation_retention_phase_duration_seconds{phase}`,
  `eshu_dp_generation_retention_scope_lock_hold_seconds`
- infra read model reconcile: `eshu_dp_infra_inventory_reconcile_total{outcome}`,
  `eshu_dp_infra_inventory_reconcile_duration_seconds`, span
  `reducer.infra_inventory_reconcile`
- status summary writer: `eshu_dp_status_summary_writer_passes_total{model_key,outcome}`,
  `eshu_dp_status_summary_writer_pass_duration_seconds{model_key,outcome}`,
  `eshu_dp_status_summary_writer_overrun_total{model_key}`,
  `eshu_dp_status_summary_writer_up{model_key}`, span `reducer.status_summary.pass`
- changed-since link domain (dark): `eshu_dp_changed_since_links_total{link_kind,outcome}`,
  `eshu_dp_changed_since_link_retries_total{reason}`,
  `eshu_dp_changed_since_link_failures_total{failure_class}`,
  `eshu_dp_changed_since_chain_breaks_total{reason}`,
  `eshu_dp_changed_since_link_retrying_scopes`, `eshu_dp_changed_since_link_poisoned_scopes`,
  `eshu_dp_changed_since_link_backlog`, `eshu_dp_changed_since_link_lag_seconds`,
  `eshu_dp_changed_since_ledger_orphans{kind}`, span `reducer.changed_since_link`
  (retry reasons: `cursor_locked`, `generation_locked`,
  `generation_lock_timeout`, `slot_busy`; chain-break reason `prior_pruned` is
  a rebase that also counts as a linked root)
- activation obligations (#7584, consumer off by default):
  `eshu_dp_activation_obligations{status}`,
  `eshu_dp_activation_obligation_oldest_open_age_seconds`,
  `eshu_dp_activation_obligation_finalize_total{outcome}`,
  `eshu_dp_activation_obligation_woken_total`,
  `eshu_dp_activation_obligation_maintenance_duration_seconds{outcome}`,
  `eshu_dp_activation_obligation_failures_total{reason}`; see the
  [reducer/storage metric catalog](../reference/telemetry/metrics-reducer-storage.md)
- graph cleanup gauge: `eshu_dp_graph_orphan_nodes`
- graph-backed gauge snapshot health: `eshu_dp_gauge_snapshot_refreshes_total`,
  `eshu_dp_gauge_snapshot_refresh_duration_seconds`,
  `eshu_dp_gauge_snapshot_age_seconds`
- logs: reducer execution result logs and shared projection cycle logs with
  domain, worker, route, row count, and failure class
- repo-dependency quarantine logs: `lease_quarantined=true`,
  `quarantine_duration_seconds`, duration, retryability, and failure class

## Related Docs

- [Service Runtimes](../deployment/service-runtimes.md)
- [Collector And Reducer Readiness](../reference/collector-reducer-readiness.md)
- [Reducer Claim-Latency Gate](../reference/reducer-claim-latency-gate.md)
- [Code-Graph Sub-Scope Partitioning](../reference/code-graph-subscope-partitioning.md)
- [Telemetry Overview](../reference/telemetry/index.md)
- [Local Testing](../reference/local-testing.md)
