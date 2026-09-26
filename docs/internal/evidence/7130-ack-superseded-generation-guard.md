# #7130: projector Ack must not revive a superseded generation

## Problem

`superseded` is a terminal generation status (`allowedGenerationTransitions`
in `go/internal/scope`), but SQL did not enforce it. `ProjectorQueue.Ack` ran
`updateProjectorScopeGenerationQuery` (repoint
`ingestion_scopes.active_generation_id`) and `activateProjectorGenerationQuery`
(`status = 'active'`) with no status predicate. A sibling projector row (for
example a `refinalize_*` row) on a generation that a newer Ack had already
superseded could be replayed from `dead_letter`, claimed, and acked. That Ack
repointed the scope to the old generation, superseded the published one through
`supersedeProjectorActiveGenerationQuery`, and re-activated the old one, so
graph and content truth regressed.

Replay had no generation fence either: the `RecoveryStore` predicate
(`buildReplayPredicate`) and the admin API replay selection
(`buildMutatingWorkItemsQuery`) filtered only on stage, scope and failure class.

Root-Cause Evidence: on base commit `84b70f5640`, the live regressions in
`go/internal/storage/postgres/projector_queue_ack_superseded_live_test.go`
failed against PostgreSQL 16:

```text
TestProjectorAckRefusesSupersededGeneration:
  Ack of superseded-generation work = <nil>, want ErrWorkSuperseded
TestProjectorAckRefusesGenerationSupersededAfterClaim:
  Ack after concurrent supersede = <nil>, want ErrWorkSuperseded
```

`TestReplayLeavesSupersededGenerationProjectorWork`
(`recovery_replay_superseded_live_test.go`) failed with `projector backlog
depth = 2, want 1`, because the superseded-generation row counted as replayable.

## Fix

### Ack

`activateProjectorGenerationQuery` now carries `AND status <> 'superseded'`, and
`ProjectorQueue.activateAckGeneration` reads its row count. Zero rows means the
target generation is superseded. `ProjectorQueue.refuseSupersededAck` then:

1. rolls the Ack transaction back, which undoes the scope repoint, the work
   `succeeded` mark, and any supersede of the published generation;
2. runs `markProjectorAckSupersededQuery` as one autocommit statement. It marks
   the owned work row `superseded` with `failure_class =
   projector_ack_generation_superseded`, checking lease owner, attempt, the
   `claimed`/`running` status, and `generation.status = 'superseded'`;
3. returns `failure.ErrWorkSuperseded`. Zero marked rows returns
   `ErrProjectorClaimRejected` instead.

Callers already handle that error from Ack. `projector.Service` routes it
through `recordSupersededWork` as a superseded outcome, not a completion or a
failure. `bootstrap-index` records `superseded` and drops the item.
`AckWhenScopeFree` returns it without retrying, and `ackWaitOutcome` maps it to
`superseded`.

A rollback plus one statement was chosen over a `SAVEPOINT`, so the common Ack
path creates no subtransaction. If the mark statement fails, the item stays
claimed until its lease expires, and the next attempt's Ack refuses again, so
the outcome converges.

### Lock order is unchanged

Ack's statements and their order are the same as before:

| # | Statement | Row locks taken |
| --- | --- | --- |
| 1 | `set_config('lock_timeout')` | none |
| 2 | `updateProjectorScopeGenerationQuery` | scope row |
| 3 | `ackProjectorWorkItemQuery` | work row |
| 4 | `supersedeProjectorObsoleteGenerationsQuery` | stale work and generation rows |
| 5 | `supersedeProjectorActiveGenerationQuery` | other active generation rows |
| 6 | `activateProjectorGenerationQuery` (now with the predicate) | target generation row |

The predicate adds no statement and no lock. An UPDATE locks only rows that pass
its quals, and statement 6 already locked the same row. The #7108/#7115
"scope row first, then work rows" order and the `lock_timeout` deferral
(`ErrWorkAckDeferred`) are untouched. The refusal path's mark statement runs
after the rollback released every lock, and it locks only the work row, so it
cannot close a wait cycle.

### Why the check sits on the generation row lock

A check under the scope lock alone would not close the TOCTOU. The claim
statement's stale-generation branch (`locked_stale_scope_generations` in
`claimProjectorWorkQuery`) supersedes `pending`/`failed` generation rows while
holding only generation and work row locks, never the scope row. Statement 6 is
Ack's first lock on the target generation row. Under Read Committed, when that
row was updated by a transaction that committed after statement 6's snapshot,
PostgreSQL re-evaluates the UPDATE's quals against the newest row version
(EvalPlanQual), so `status <> 'superseded'` sees the committed supersede. Once
Ack holds the row, the claim branch's `SKIP LOCKED` skips it.
`TestProjectorAckRefusesGenerationSupersededAfterClaim` holds exactly that
interleaving: it waits in `pg_locks` until Ack waits on the superseding
transaction's xid, commits it, and asserts the refusal.

### Replay fence

Both replay paths leave a projector row terminal when its generation is
superseded. Reducer rows are not fenced.

- `RecoveryStore` (runtime `/admin/replay`, `DrainBacklog`):
  `buildReplayPredicate` appends `NOT supersededProjectorGenerationFence`, so
  `CountDeadLetterBacklog` and the replay still select the same rows. After a
  projector replay, `countSupersededReplaySkips` counts the fenced rows into
  `ReplayResult.SkippedSupersededGeneration`, `DrainResult`, and the
  `skipped_superseded_generation` response field.
- Admin API `/api/v0/admin/replay`: `buildMutatingWorkItemsQuery` adds the same
  fence for replay but not for the dead-letter mutation. Explicit
  `work_item_ids` naming fenced rows get a `422` from
  `refuseSupersededExplicitReplay` via `SupersededReplayTargets`. It runs
  after the request-level unsafe `failure_class` check and before the #7120
  explicit-id unsafe-class refusal, and `force` does not bypass it. A `200` with missing ids therefore still means "not
  matched", never "silently skipped".

Replay reads the generation status without a lock, so a supersede that commits
after the replay's snapshot can still let a row reach `pending`. The Ack guard
above is the authoritative fence for that race.

## Claim-side fence

### The hazard

The Ack guard is the last line, not the fix. Before this change
`claimProjectorWorkQuery` still claimed a pending projector row whose
generation was already superseded: its stale-supersede step covered only
`pending`/`failed` generations with a newer sibling. Such a row appears without
any replay. A liveness-recovery `projector_<scope>_<gen>` row or a
`refinalize_<scope>_<gen>` row sits pending on active `gen-old`; `gen-new`'s
Ack sweeps only rows on `pending`/`failed` generations, then retires `gen-old`
without touching its rows.

Claiming that row is not wasted work. The canonical writer retracts every other
generation's nodes for the repository (`generation_id <> $generation_id` in
`canonicalNodeRetractFilesCypher`, `canonicalNodeRetractRemovedFilesCypher` and
`canonicalNodeRetractEntityTemplate`,
`go/internal/storage/cypher/canonical_node_cypher.go`), and graph reads
do not filter on generation. So projecting `gen-old` deletes `gen-new`'s
canonical graph and content and writes `gen-old`'s before Ack refuses. After the
Ack guard, the scope still points at `gen-new` while the graph is `gen-old`'s,
and nothing re-projects `gen-new`. A live probe on the Ack-guard head showed
Claim returning `gen-old attempt=1`, Heartbeat returning nil, and only Ack
refusing.

### The fix

- **Claim.** `supersedable_projector_generations` gains a second branch: a
  `superseded` generation is terminal by itself, with no newer-sibling test.
  The branch takes the generation's claimable rows, `pending` and `retrying`
  plus expired-lease `claimed`/`running` rows, which the reclaim rank would
  otherwise re-claim. It leaves `failed`/`dead_letter` rows alone: they are
  never claim candidates, replay already leaves them terminal, and sweeping
  them would overwrite their triage `failure_class` and put an unbounded legacy
  backlog into one claim statement (2,200 legacy rows cost 1.55 s in one
  claim; see Evidence). A live lease is left to its heartbeat.
  `locked_stale_scope_generations` accepts `superseded`, and
  `locked_stale_projector_generations` and the supersede UPDATE repeat the
  widened row predicate so EvalPlanQual drops a lease renewed after the
  snapshot. `superseded_stale_scope_generations` still matches only
  `pending`/`failed`, so it never rewrites a superseded generation's
  `superseded_at`. The swept rows get
  `failure_class = projector_superseded_by_newer_generation` and
  `failure_details` now carries `generation_id` and `generation_status`, so a
  `superseded` value marks this sweep. Every lock stays SKIP LOCKED, so the
  claim adds no wait and no lock order. Legacy rows in deployed databases are
  swept on the first claim that reaches them; no migration.
- **Heartbeat.** `supersedeRunningProjectorWorkQuery` treats the work's own
  `superseded` generation as a trigger without the newer-sibling test, writes
  `failure_class = projector_heartbeat_generation_superseded`, and returns the
  generation status it stopped the work under. The outer generation UPDATE now
  also matches `superseded` (its CASE leaves the row unchanged), so the
  statement returns a verdict row for every trigger. Heartbeat reads that row
  instead of `RowsAffected`, returns an error wrapping `ErrWorkSuperseded` that
  names the failure class, and counts the fence metric. The lock set is
  unchanged: scope row (SKIP LOCKED), own work row, own generation row.
  `projector.Service` already cancels the projection on that error.

### Residual window

A worker whose lease expired can still be projecting `gen-old` when `gen-new`
is claimed and acked. It keeps writing until a heartbeat runs its supersede
check: one heartbeat interval, plus every interval in which that check is
deferred. The check takes the scope row with SKIP LOCKED, so while ingestion,
Ack or Fail holds the scope row the heartbeat renews the lease instead, and a
same-scope ingestion streaming facts can extend the window for as long as it
holds the row. The retract may already have removed `gen-new`'s canonical
nodes. The fences stop the deterministic path; healing the graph
after such a refusal is follow-up #7209 (re-project the active generation),
agreed by the arbiter ruling as a P2 that does not block #7130.

### Accepted side effects

- An expired `claimed`/`running` row on a superseded generation, beside a live
  lease in the same scope, matches both the duplicate-lease reclaim
  (`reclaimed_stale_projector_duplicates`, to `retrying`) and the superseded
  sweep. PostgreSQL applies only one of two updates to the same row in a
  statement and does not say which. Both outcomes converge: a `retrying` row on
  a superseded generation is swept by the next claim, and neither is claimed.
- `failed` and `dead_letter` rows on a superseded generation are never
  claimable and never replayed, so nothing moves them until generation
  retention deletes them. The status snapshot still counts them: the queue
  `dead_letter_count`/`failed_count`, the stage counts, the per-domain backlog
  and the latest-failure row read `active_fact_work_items`, whose
  stale-generation exclusion covers reducer rows only
  (`activeFactWorkItemsFromWhere`). `eshu_dp_queue_depth` does not count them,
  because `queueDepthQuery` reads only pending, claimed, running and retrying
  rows, and the poison dead-letter gauges skip dead letters whose scope has a
  newer generation. `CountDeadLetterBacklog` and drain exclude them, and replay
  reports them as `skipped_superseded_generation`.

## Tests

- `TestProjectorAckRefusesSupersededGeneration`: the issue scenario, plus a
  repeated Ack that stays a claim rejection.
- `TestProjectorAckRefusesGenerationSupersededAfterClaim`: the EvalPlanQual race.
- `TestProjectorAckSupersededGenerationRollsBackAndMarksWork`,
  `TestProjectorAckSupersededGenerationLostClaimIsClaimRejected`,
  `TestProjectorAckActivatedGenerationCommits`: hermetic statement and
  telemetry contract.
- `TestReplayLeavesSupersededGenerationProjectorWork`: unbounded and bounded
  drain, backlog count parity, the skip count and counter, and the reducer row
  still replaying.
- `TestAdminStoreSupersededGenerationReplayFenceLive`,
  `TestReplayExplicitIDsOnSupersededGenerationRefusedEvenWithForce`,
  `TestReplayBroadSelectorSkipsSupersededRead`,
  `TestReplayFencesSupersededProjectorGenerationsButDeadLetterDoesNot`,
  `TestSupersededReplayTargetsQueryShapeAndScan`: the admin path.

- `TestProjectorClaimSweepsSupersededGenerationRow`: pending, retrying and
  both expired-lease zombie rows on a superseded generation are swept, never
  claimed; the sweep frees the scope for a newer pending generation in the same
  claim; a live lease is left alone; `failed` and `dead_letter` rows keep their
  status and triage `failure_class`. `TestProjectorClaimStillClaimsLiveGenerations`
  holds the failed/active/pending controls.
- `TestProjectorClaimSupersededSweepDropsLeaseRenewedAfterSnapshot`: a lease
  renewed and committed while the claim is paused mid-statement survives the
  sweep (EvalPlanQual on the widened predicate).
- `TestProjectorHeartbeatRefusesSupersededGeneration` (live lease and expired
  zombie) and `TestProjectorHeartbeatRenewsLiveGeneration` (renewal and the
  newer-pending class), plus the hermetic heartbeat tests.
- `TestProjectorAckSupersededMarkKeepsOwnerAndAttemptFences`: the mark
  statement refuses a row reclaimed by another owner or re-claimed at
  `attempt_count + 1` (review F2).
- `TestServiceLogsSupersededWorkWithSourceFailureClass`: the service logs the
  failure class the queue recorded (review F3).
- `TestProjectorClaimConcurrentLoadHasNoDeadlock` now also seeds refinalize
  rows on superseded generations and heartbeats expired workers, and fails if
  any such row is ever claimed.

Removing the activate predicate, the replay fence clause, the admin fence flag,
the explicit-id refusal, the claim's superseded branch, its expired-lease
inclusion, its exclusion of `failed`/`dead_letter` rows, its lock-step lease
recheck, the heartbeat trigger, or the widened
heartbeat outer UPDATE each fails at least one of these tests.

## Evidence

No-Regression Evidence: EXPLAIN (ANALYZE, BUFFERS) on PostgreSQL 16 with 20,000
generations (half superseded) and 200,000 work items (26,667 dead-lettered
projector rows). The Ack activate UPDATE stays an index scan on
`scope_generations_pkey` for one row; the predicate only joins the filter
(`(status <> 'superseded') AND (scope_id = ...)`), 0.139 ms before and 0.040 ms
after, 11 and 8 shared buffers. No Ack statement was added, removed, or
reordered. The bounded replay selection (LIMIT 100) went from 0.111 ms and 154
buffers to 2.556 ms and 448 buffers, from one hashed SubPlan over the 10,000
superseded generations built once per statement. PostgreSQL falls back to a
per-row primary-key probe when that hash would not fit in `work_mem`. The skip
count took 10.5 ms (bitmap scan on `(stage, status)` hash-joined to superseded
generations). Replay and drain are operator and admin actions, not a
per-work-item path.

No-Regression Evidence (claim fence): EXPLAIN (ANALYZE, BUFFERS) of the whole
claim statement inside BEGIN/ROLLBACK on PostgreSQL 16, 20,000 git scopes,
85,000 projector rows (5,000 pending on pending generations, 60,000
generations superseded), 300,000 reducer rows and 2,000 legacy dead-letter
rows on superseded generations. Six interleaved runs per cell, alternating
which statement ran first. Median milliseconds, #7115 claim vs this claim:
all sources, custom plan 169.2 vs 173.7 (+4.4); all sources, generic plan
166.8 vs 171.7 (+4.9); `source_system = git`, custom 164.4 vs 173.0 (+8.6);
git, generic 199.9 vs 185.3 (-14.7, noise). Shared buffers rise by about
20,000 per claim: one primary-key probe per ready row for its generation
status. The first branch is the #7115 text byte for byte, so its plan is
unchanged. Rejected shapes, measured on the same data: ORing the two branches
added about 30-40 ms, because the newer-sibling EXISTS under an OR stays a
per-row subplan instead of a semi-join, and the status index is lost; one shared
candidate scan feeding both branches added about 20 ms, because the CTE row
estimate of 1 turned the semi-join's inner index scan into a bitmap scan;
joining branch two to the 85,000-row source CTE added about 17 ms. One-time
sweep: 200 legacy pending rows on superseded generations added about 140 ms to
the first claim (about 0.7 ms per swept row); an earlier draft that also swept
2,000 dead-letter rows took 1.55 s, which is why the branch skips them.
Heartbeat adds no statement; the supersede statement now returns one column.

Concurrency Evidence: `TestProjectorClaimConcurrentLoadHasNoDeadlock`, 40 s,
32 workers, 150 ms leases: 3,960 claims, 0 server deadlocks, 0 claim deadlock
errors, 0 overlapping leases, 487 refinalize rows seeded on superseded
generations and 0 of them claimed. With the superseded branch deleted, a 15 s,
16-worker run claimed 269 of 287 such rows and failed. The heartbeat refusal did not fire under
this load (0), because the claim's sibling reclaim usually demotes an expired
row before its worker heartbeats; its lock set is unchanged and
`TestProjectorHeartbeatRefusesSupersededGeneration` covers it deterministically.

Observability Evidence: `eshu_dp_superseded_generation_fence_total`, by
`failure_class`. `projector_ack_generation_superseded` is recorded by
`refuseSupersededAck` and `projector_heartbeat_generation_superseded` by
`ProjectorQueue.Heartbeat`, both through `ProjectorQueue.Instruments`. The
claim sweep is not counted, because the claim statement returns only the
claimed row; its rows carry `failure_details.generation_status = superseded`.
`projector_replay_generation_superseded` is recorded by
`RecoveryStore.ReplayFailedWorkItems` (wired with `WithRecoveryInstruments` in
the ingester and API) and by the admin replay refusal. The work row keeps the refusing path's `failure_class` for triage, and the
projector service logs the superseded outcome with that same class, and a refused admin replay
writes the `replay_refused_superseded_generation` governance audit event.
