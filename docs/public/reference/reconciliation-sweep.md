# Reconciliation Sweep

The reconciliation sweep is the periodic full re-observation that retracts graph
drift the git delta path cannot see. It is part of the
[Incremental Freshness Model](incremental-freshness-model.md); this page
documents when the sweep forces a full snapshot, how it is bounded, and how an
operator watches it.

## Why the sweep exists

Even with a correct baseline, a delta sync can leave stale graph nodes: a missed
deletion or a retraction that failed after its delta was applied has no later
delta that mentions the path, so the delta path alone can never clean it. A
periodic reconciliation catches this drift.

Reconciliation re-uses the proven full-snapshot path rather than a bespoke
graph-diff. When a git scope has gone longer than `ESHU_REPO_RECONCILE_INTERVAL_HOURS`
(default 24) without a projected **full** observation, the next sync forces a
full snapshot for that scope regardless of any usable delta baseline. A full
generation re-emits every current file under a new `generation_id`, and the
canonical projector retracts every File/Directory/Entity node carrying a
different `generation_id` for the repository — so any path that disappeared
between the last full observation and now is deleted from the graph.

Each generation records whether it was a delta on `scope_generations.is_delta`.
The sweep reads `FullReconcileState` for the scope: the ingest time of the
newest **activated** full generation, and the newest full generation of any
status. A reconciliation generation carries an **empty** freshness hint so the
commit-time skip never elides it: it must re-project even when the content hash
is unchanged, otherwise drift in the graph would survive and the reconciliation
timer would never advance.

The obligation is measured on the last projected full generation, and the
throttle on the latest full attempt (#7288). With `Interval` =
`ESHU_REPO_RECONCILE_INTERVAL_HOURS`, the first matching row decides; the reason
is the bounded label on the sweep's counters and log:

| Scope state | Decision | Reason |
| --- | --- | --- |
| newest activated full generation is younger than `Interval` | hold | `fresh` |
| newest full generation is `pending`, younger than `Interval` | hold | `reconcile_in_flight` |
| newest full generation is `pending`, `Interval` old or older | force | `in_flight_expired` |
| newest full generation failed, or was superseded before it activated, and is younger than `Interval / 4` | hold | `reconcile_retry_backoff` |
| the same, `Interval / 4` old or older | force | `retry_after_unprojected` |
| no full generation ever activated | force | `never_reconciled` |
| otherwise | force | `interval_elapsed` |

A clock-skewed (future) ingest time counts as younger than every bound and holds.
A resolver read error holds as well, so a Postgres outage does not also trigger
a fleet of forced full snapshots.

Without the in-flight rule, a projector that ran behind the selection cycle
(about six minutes) made the sweep force a new full snapshot of the same commit
every cycle, and each one superseded the previous before it could activate.
Without the backoff, a full generation that failed or was superseded by a delta
before activating would make its scope due again on the next cycle. No single
generation suppresses the sweep for longer than `Interval`, and the per-scope
worst case is one forced full per `Interval` while fulls stay pending, or four
while they keep failing. The retry backoff is derived from `Interval`; there is
no separate setting. A sweep that stays ineffective because nothing ever
activates is a projection outage, visible through the counters below.

The sweep is bounded and scheduled, not a full re-index:

- **Scheduled**: it rides the normal sync cadence; a scope is re-observed fully
  once per interval, then resumes delta sync.
- **Bounded per cycle**: `ESHU_REPO_RECONCILE_MAX_PER_CYCLE` (default 10) caps how
  many overdue scopes one selection cycle may force to full, so a fleet that all
  comes due together does not stampede into simultaneous full snapshots. The
  remainder are picked up on later cycles.
- **Disable**: set `ESHU_REPO_RECONCILE_INTERVAL_HOURS=0`. A
  [reindex request](#reindex-requests) is still honored.

Cost is one full re-observation and projection per scope per interval — the same
cost as a first sync, paid on a documented cadence. Each forced reconciliation
increments `eshu_dp_collector_reconciliation_full_snapshots_total`, labeled by
`reason` (`never_reconciled`, `interval_elapsed`, `in_flight_expired`,
`retry_after_unprojected`, `graph_dirty`, `reindex_requested`), and logs `git_reconcile_forced` with `scope_id`,
`reason`, `last_projected_full_at`, `latest_full_at`, and `latest_full_status`
(plus `reindex_requested_at` for `reindex_requested`).
The log is WARN for `in_flight_expired` and `retry_after_unprojected`: repeats of
either mean projection is not keeping up with the sweep. Each evaluation the
sweep holds off increments `eshu_dp_collector_reconciliation_suppressed_total`,
labeled by `reason` (`reconcile_in_flight`, `reconcile_retry_backoff`). A
sustained suppression rate with few forced reconciliations points at projector
lag, not at the sweep.

When the canonical graph writer applies that forced snapshot, successful stale
cleanup statements also increment
`eshu_dp_reconciliation_drift_retractions_total` with bounded labels:
`domain="canonical_graph"`, `write_phase`, and `kind="node"` or `kind="edge"`.
The full-snapshot counter answers "did reconciliation run?"; the drift
retraction counter answers "did it actually delete stale graph state?" Alert on
sustained nonzero retractions after the first reconciliation window, and use
projector logs/spans keyed by `scope_id`, `repo_id`, and `generation_id` for the
specific source. Those identifiers are intentionally not metric labels.

## Reindex requests

`POST /api/v0/admin/reindex` (or `eshu admin reindex`) forces a full re-parse
of every git repository, for example after an upgrade that changes what the
parsers emit (#7620). It records a fleet **reindex watermark**:
`runtime_ingester_control.reindex_request_requested_at` for the `repository`
ingester, stamped by Postgres and never moved backward. The response returns it
as `requested_at`.

Every git ingester shard reads the watermark once per sync cycle, one
primary-key read. A scope the sweep leaves `fresh`, with no `graph_dirty`
writer, is then checked against it:

| Scope state | Decision | Reason |
| --- | --- | --- |
| newest activated full generation ingested at or after the watermark | hold | `fresh` |
| in-flight or recently failed full (the throttle rows above) | hold | `reconcile_in_flight` or `reconcile_retry_backoff` |
| otherwise, including a scope with no activated full | force | `reindex_requested` |

A forced reindex takes the same path as any reconciliation: an empty freshness
hint, no delta, and a slot in `ESHU_REPO_RECONCILE_MAX_PER_CYCLE`, so a large
fleet re-parses over several cycles. The request is never claimed and has no
completion status. It is satisfied scope by scope: a forced full is ingested at
the cycle's start time, which is at or after the watermark, so once it activates
the scope stays `fresh`. A later request moves the watermark forward and starts
a new pass.

- A shard whose cycle started before the watermark (clock skew between the
  ingester and Postgres) skips it that cycle and logs INFO
  `git_reindex_watermark_deferred`. Otherwise every forced full would land
  before the watermark and be forced again.
- A failed read is ignored for that cycle and logs WARN
  `git_reindex_watermark_read_failed`.
- Each cycle with an active watermark logs INFO
  `git_reindex_watermark_active` with `repo_shard_index`, `repo_shard_count`,
  and `reindex_requested_at`.
- With `ESHU_REPO_RECONCILE_INTERVAL_HOURS=0` the watermark is still honored,
  with the throttle bounds of the 24-hour default. The interval reasons and
  `graph_dirty` stay off. While a watermark is set, each cycle reads the
  per-scope state the default sweep reads.
- A webhook-only ingester reaches only the repositories it is triggered for.
  Filesystem source mode does not read the watermark.

Watch progress with
`eshu_dp_collector_reconciliation_full_snapshots_total{reason="reindex_requested"}`
and the `git_reconcile_forced` log. When that counter stops rising while the
suppression counter is flat, no scope the sync visits is still due. A scope the
sync never visits is not covered (#7625).

## Delta baseline fence

A delta generation is the diff from the scope's active commit, read when the
collector starts its cycle, to the new remote head. Another generation can
activate while that delta is parsed, committed, or projected (#7319). The
collector therefore records the commit it diffed from on
`scope_generations.delta_baseline_commit_sha`, and the projector enforces one
invariant:

> A delta generation activates only while
> `active(scope).source_commit_sha == delta.delta_baseline_commit_sha`.

The projector checks it twice with the same decision table. The preflight check
runs before the work item loads facts or writes the graph. The Ack check runs
inside the Ack transaction as its own statement, after Ack has taken the scope
row, so it sees every Ack that committed while it waited. Only commits are
compared, never generation ids.

| Read | Outcome |
| --- | --- |
| full generation (no baseline) | proceeds, not counted |
| delta written before migration 148 (no baseline) | proceeds as `unfenced` |
| the delta's own generation is already active | proceeds as `already_active` |
| the delta's own generation is already superseded | proceeds, not counted, as `target_superseded` |
| active commit equals the baseline | proceeds as `matched` |
| active commit differs, or is empty | refused as `refused_active_differs` |
| no active generation | refused as `refused_no_active` |

A target already superseded by a newer generation's activation is not a fence
refusal: it is the normal, expected outcome of projector lag, already owned by
the pre-existing superseded-generation path (Ack's own activation predicate
refuses it there exactly as it would a raced full generation, logging at INFO
with `failure_class = projector_ack_generation_superseded`; a preflight read
proceeds and lets Ack or a heartbeat catch it, the same way an unfenced full
generation already does). The fence checks target-superseded before comparing
commits, so a delta that can never activate again is never scored against a
baseline it has no further claim on.

A refused delta's work row and generation become `superseded`, never `failed`,
so replay and dead-letter drains leave it alone. The work row carries
`failure_class = projector_delta_baseline_mismatch` when preflight refused it
(the graph was not touched) or `projector_delta_baseline_mismatch_after_projection`
when Ack refused it, and `failure_details` names the scope, generation, baseline
commit, active commit (or `none`), active generation, and phase. The next
collector cycle diffs from the new active commit, so recovery needs no trigger.

What the fence does not enforce on its own: that the graph content equals the
baseline commit. The write-through contract (#7389) closes that gap. Before
its first graph or content write, after it loads facts, a projector attempt
sets its generation's `projection_write_started_at` marker (the latest write
start, fenced on its claim). The heartbeat never supersedes a generation whose
marker is set for a newer one, so a writer runs to Ack; a newer delta diffed
from the same active commit is then refused at preflight before it writes, and the
collector's next delta is diffed from the writer's commit. A generation that
wrote and still never activated keeps its marker. The heartbeat never
abandons a started write to a newer generation, but other paths still retire
one: the claim path's stale-generation supersede and Ack's obsolete supersede
(a marked pending generation whose work was retrying), a dead letter, a lease
expiry, or an Ack refusal. Each is healed by a forced full: while such a
writer is uncovered, the sweep forces the scope with reason `graph_dirty` (WARN
log `git_delta_baseline_graph_dirty`), held off only by the in-flight and
retry-backoff throttle, never by a fresh full, and counted against
`ESHU_REPO_RECONCILE_MAX_PER_CYCLE` (a scope over budget stays on its delta and
is forced a cycle later). Setting `ESHU_REPO_RECONCILE_INTERVAL_HOURS=0` also
disables this heal. A marked writer is released only by its own completion,
failure, or a lease expiry after its process dies; on Neo4j
`ESHU_CANONICAL_WRITE_TIMEOUT` is unbounded by default, so a hung write with a
live heartbeat freezes that scope's freshness until the worker restarts.
The heal is paced by that throttle: a pending heal full holds the scope for up
to the reconcile interval, and a heal full superseded or failed before
activation backs the scope off a quarter of the interval. The claim path
supersedes a pending full when a newer delta arrives before it is claimed, so a
hot repo under projector backlog repeats the heal in quarter-interval steps
rather than at the next sync (about 28 consecutive attempts at the defaults
before a 168-hour retention prune could remove the marker). Retention of an uncovered writer before its heal
is a tracked follow-up.

`eshu_dp_projector_delta_baseline_fence_total` counts decisions by `phase`
(`preflight`, `ack`) and `outcome`. Passes are counted once, at Ack. A
preflight `refused_active_differs` is expected whenever a newer generation
arrives while an older one is writing; its rate should not exceed the scope's
generation commit rate. A rising refusal share beyond that means the projector
lags the collector. Any `phase=ack` refusal
(`refused_active_differs` or `refused_no_active`) means two claims were valid
in one scope and logs at ERROR; a preflight refusal logs at WARN. This holds
because a target already superseded by a newer activation never reaches a
refused outcome here -- it is `target_superseded` (see above), so it cannot be
mistaken for the two-valid-claims signal. `unfenced` should fall to zero once
every collector writes the baseline. Commit SHAs appear in logs and
`failure_details`, never as labels. The collector's `git repository sync
completed` log carries `delta_baseline_commit_sha` for a delta sync.

Rollout order is migrations 148-150, then binaries. Deltas written by an older
collector, or in flight during the rollout, have no baseline and are not fenced.

## Related references

- [Incremental Freshness Model](incremental-freshness-model.md)
- [Telemetry](telemetry/index.md)
