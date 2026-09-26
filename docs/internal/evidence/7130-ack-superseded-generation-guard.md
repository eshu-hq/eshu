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
  before, and independently of, the #7120 unsafe-class refusal, and `force`
  does not bypass it. A `200` with missing ids therefore still means "not
  matched", never "silently skipped".

Replay reads the generation status without a lock, so a supersede that commits
after the replay's snapshot can still let a row reach `pending`. The Ack guard
above is the authoritative fence for that race.

## Out of scope: claim-side fence

`claimProjectorWorkQuery` still claims a pending projector row whose generation
is already superseded (its stale-supersede step covers only `pending`/`failed`
generations). #7115 is rewriting the claim SQL, so it should add that fence in
its rewrite. Until then, such a claim projects wasted work and the Ack guard
stops it from publishing.

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

Removing the activate predicate, the replay fence clause, the admin fence flag,
or the explicit-id refusal each fails at least one of these tests.

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

Observability Evidence: `eshu_dp_superseded_generation_fence_total`, by
`failure_class`. `projector_ack_generation_superseded` is recorded by
`refuseSupersededAck` through `ProjectorQueue.Instruments`.
`projector_replay_generation_superseded` is recorded by
`RecoveryStore.ReplayFailedWorkItems` (wired with `WithRecoveryInstruments` in
the ingester and API) and by the admin replay refusal. The work row keeps
`failure_class = projector_ack_generation_superseded` for triage. The
projector service logs the superseded outcome, and a refused admin replay
writes the `replay_refused_superseded_generation` governance audit event.
