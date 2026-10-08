# #7670 remainder: fenced RUNS_ON replay reports the closed outcome

## Problem

The #7670 base fix gave the plain workload replay the closed outcome
(`scheduled`, `superseded`, `not_scheduled`) but left the RUNS_ON fenced
replay on a boolean. A fenced replay that found its stable
`workload_materialization` item terminally `superseded` on the scope's
active generation counted as
`unscheduled_fenced_replay_on_active_generation`, indistinguishable from a
dead-lettered item that can never be scheduled. The operator could not tell
"the queue terminally retired this item" from "nothing was schedulable".

## RED run

`TestRepoDependencyRunsOnFencedReplayCountsSupersededItemOnActiveGeneration`
ran against the unmodified code with the new outcome fake method reporting
`superseded` for the active generation:

```text
--- FAIL: TestRepoDependencyRunsOnFencedReplayCountsSupersededItemOnActiveGeneration (0.00s)
    repo_dependency_projection_replay_generation_test.go:412: superseded_item_on_active_generation counter = 0, want 1
```

It passes on the final code.

## What changed

- `ReducerQueue.ReplayWorkloadMaterializationForFenceOutcome`
  (`go/internal/storage/postgres/reducer_queue_ack.go`) runs the same
  fenced schedule as before (update first, enqueue, one update retry so a
  concurrent first insert cannot retain an older fence token) and, when all
  three match nothing, classifies the row through the shared
  `workloadMaterializationReplayOutcome` switch also used by the plain
  path. `ReplayWorkloadMaterializationForFence` keeps its boolean contract
  and delegates to it, the same shape as the plain pair.
- `WorkloadMaterializationFenceOutcomeReplayer`
  (`go/internal/reducer/repo_dependency_projection_replay.go`) is the
  optional fenced extension, mirroring
  `WorkloadMaterializationOutcomeReplayer`.
- `replayWorkloadMaterializationForFence`
  (`go/internal/reducer/repo_dependency_projection_acceptance_cycle.go`)
  switches on the closed outcome. `superseded` re-checks freshness without
  the cache and either skips the retired generation or counts the shared
  `superseded_item_on_active_generation` anomaly and fails closed. Any
  other unscheduled replay keeps today's behavior: uncached re-check, skip
  if retired, else the fenced anomaly and fail closed. The failure message
  is byte-identical (`errRepoDependencyFencedReplayNotScheduled`).

## Performance and concurrency

No-Regression Evidence: the scheduled path issues the identical statement
sequence as before (update, conditional enqueue, conditional retry). The
only added statement is the shared single-row `SELECT status` primary-key
lookup, and it runs only when the update, the enqueue and the retry all
matched nothing, i.e. terminal states and missing rows, the same branch on
which the plain path already runs it. Conflict domain: one
`workload_materialization` queue row keyed by the deterministic work item
id; the concurrent-insert plus single update-retry interleaving is
byte-identical, so the fence token correctness argument is unchanged. The
runner takes no new lock and holds its partition lease as before. Worker,
batch and lease settings are unchanged; queue row counts are unchanged.
Lane timings are NOT_CHECKED beyond the focused tests below.

Observability Evidence: no new instrument. A fenced replay whose stable
item is superseded on the active generation now counts on the existing
`eshu_dp_repo_dependency_generation_anomalies_total{reason="superseded_item_on_active_generation"}`
with the existing WARN `repo dependency generation anomaly`; any other
unscheduled fenced replay keeps
`reason="unscheduled_fenced_replay_on_active_generation"`. Fail-closed
requests still count on
`eshu_dp_shared_projection_lease_quarantines_total{domain="repo_dependency"}`.
Tests: `TestRepoDependencyRunsOnFencedReplayCountsSupersededItemOnActiveGeneration`
(shared counter increments),
`TestRepoDependencyRunsOnFencedReplayCountsDeadLetteredItemAsUnscheduled`
(dead-lettered stays fenced-unscheduled, shared counter stays zero),
`TestRepoDependencyRunsOnFencedReplayOutcomeSkipsWhenSupersedeLandsBetweenCheckAndReplay`
(retired between check and replay skips),
`TestReducerQueueReplayWorkloadMaterializationForFenceOutcome*` (scheduled
by update, missing item stays not-scheduled, per-status classification),
and the unchanged `...ForFenceRetriesConcurrentInsert` /
`...ForFenceRejectsTerminalWork` guards on the boolean contract.
