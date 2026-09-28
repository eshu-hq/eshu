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
- **Disable**: set `ESHU_REPO_RECONCILE_INTERVAL_HOURS=0`.

Cost is one full re-observation and projection per scope per interval — the same
cost as a first sync, paid on a documented cadence. Each forced reconciliation
increments `eshu_dp_collector_reconciliation_full_snapshots_total`, labeled by
`reason` (`never_reconciled`, `interval_elapsed`, `in_flight_expired`,
`retry_after_unprojected`), and logs `git_reconcile_forced` with `scope_id`,
`reason`, `last_projected_full_at`, `latest_full_at`, and `latest_full_status`.
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

## Related references

- [Incremental Freshness Model](incremental-freshness-model.md)
- [Telemetry](telemetry/index.md)
