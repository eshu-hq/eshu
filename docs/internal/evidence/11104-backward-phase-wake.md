# #11104 PR 1: exact-phase wake for the cross-repo backward-evidence deferral

Issue #11104: the Ifá determinism gate lost repo-dependency and
workload-dependency cells with workload materialization stuck behind
`deployment_mapping` rows that stayed parked for the full 10-minute backstop.
The binding ruling is `.claude/arbiter-11104.md` (PR 1 of 2; PR 2, the
audit-class horizon, is a separate change).

## Cause

The `cross_repo_backward_evidence_not_ready` deferral was mapped to
`target_committed`/`audit`. The handler gates on one exact phase row: the
generation's own `cross_repo_evidence`/`backward_evidence_committed` row,
which only the deferred-backfill fan-in publishes. The fan-in's phase Upsert
woke only `phase_committed` waits on the coarse `{scope_id, generation_id}`
key, so it never reached this wait. The audit's proxy for `target_committed`
(a sibling reducer row succeeded after `first_deferred_at`) never fires when
the sibling succeeded before the park, which is the CI order. Since #10876
every parking class sits on the 10-minute backstop, longer than the gate's
3-minute drain bound.

## Change

`go/internal/storage/postgres/` and its `backfill/` child, plus two closed
`wake_site` values. No DDL, no new metric, and no new log key in the
contract:

1. `waitingKey` and `WaitingDeferral` carry `Keyspace` and `Phase`. A
   `phase_committed` row that names both parks on the exact key
   `{scope_id, generation_id, keyspace, phase}`. The kind stays
   `phase_committed`, so the migration 316 CHECK is unchanged.
2. The registry row for this class is now `phase_committed`/`in_tx`, keyspace
   `cross_repo_evidence`, phase `backward_evidence_committed`.
3. `waitingSatisfied` routes a phase-exact wait to
   `exactPhaseWaitSatisfiedQuery`, which has the activation `phaseReadyQuery`
   shape with all six primary-key columns pinned. Both the audit and the
   park-time check use it. The coarse any-phase branch is never used for an
   exact key.
4. The phase Upsert also wakes the exact key for rows whose (keyspace, phase)
   the deferral map enrolls. The set derives from the map. Rows of any other
   phase send no extra statement.
5. `backfill.Store.WakePhaseWaitsAfterCommit` is a new seam. It is wired in
   root (`backfillStore`), and `publishDeferredBackfillPartition` calls it
   after its commit. It covers both the whole pass and the targeted pass,
   because both publish through that function. It runs detached from the
   publisher's cancellation under a 5 s bound
   (`PostCommitExactPhaseWakeTimeout`, far below the 5-minute audit interval
   that covers a timed-out re-wake). A failure or timeout logs at ERROR,
   counts `outcome=error`, and is never returned. The root phase store built for the
   fan-in now carries the instruments, so its in-tx wakes are metered.

Superseded generations are unchanged: the stale sweep and the backstop still
cover them.

## Performance Evidence:

Conflict domain: one scope generation's `deployment_mapping` waiters and its
`graph_projection_phase_state` row. Lease settings in the proofs: reducer lease
5 minutes, retry delay 30 s, three `deployment_mapping` workers, one sibling
`ClaimBatch` worker (limit 3), two fan-in publishers, one auditor, and one
activation Finalize consumer.

Added cost: one wake statement inside the fan-in transaction and one after the
commit, per published partition, for the enrolled phase only. Every other
phase publish keeps the same statements (pinned by
`TestPhaseUpsertStatementsForEnrolledAndOtherPhases`: 2 statements for
non-enrolled rows, 3 with one enrolled row).

These are prove-the-theory measurements on disposable Postgres 18.6, on a
clone of the bootstrapped schema template. The waiting table was seeded with
20,000 coarse waits over 5,000 scope generations plus 2,000 exact
backward-evidence waits. The phase table was seeded with 20,000 rows.

- Exact-key wait probe (the `due` CTE of `reopenWaitingRowsQuery`,
  `FOR UPDATE`): Index Scan on `reducer_waiting_items_wake_idx`, 1 row,
  4 shared buffers, 0.070 ms. The coarse key on the same scope generation
  matched its 4 coarse rows and not the exact row, which is the JSONB-equality
  separation the design relies on.
- `exactPhaseWaitSatisfiedQuery`: Index Scan on
  `graph_projection_phase_state_generation_idx`, 3 shared buffers, 0.015 ms.

Before and after row counts come from the live proofs (RED on `b1fddd0f60`,
the unchanged `origin/main` `8b6af10392` plus the tests). Before: after the
publish, the waiter stayed parked (`visible_at` = park + 10 minutes, one wait
row), and the audit reopened 0 rows. After: the waiter reopened in the
publish (`visible_at` <= now, `status = retrying`, `attempt_count` unchanged
at 1, 0 wait rows), and the audit reopened 1 row on the crash residual.

Contention: `TestBackwardEvidenceWakeContentionLive` ran once under `-race`,
then 5 more times without it with 4 to 6 gated parks per run. Every run had
0 `40P01` errors, no change in `pg_stat_database.deadlocks`, all 8 waiters
succeeded, and 0 wait rows were left with the phase present. No final audit
runs, so a missed wake would fail the run.

Lock order: the fan-in transaction takes the repository advisory locks
(sorted), then the phase row, then `reducer_waiting_items` in
`work_item_id COLLATE "C"` order, then the `fact_work_items` rows it reopens,
then the memo rows. The post-commit re-wake, the Fail park-time wake, and the
audit take waiting rows before queue rows in their own statements. Claim and
`AckBatch` lock `fact_work_items` with `SKIP LOCKED` or by their claimed ids.
Activation Finalize takes the scope row, then the obligation row, then
`fact_work_items` with `SKIP LOCKED`. No path takes a waiting row while it
holds a reopened queue row.

## Observability Evidence:

- `eshu_dp_reducer_waiting_wake_total{wake_site="phase_exact_in_tx"|"phase_exact_post_commit", outcome="reopened"|"noop"|"error"}`
  separates this wake from the coarse `in_tx` and `audit` sites. It is a
  closed label set.
- `reducer exact phase wake`, one line per exact wake statement, with
  `scope_id`, `generation_id`, `keyspace`, `phase`, `wake_site`, and
  `reopened`: INFO when the statement reopened a waiter, DEBUG for a no-op,
  so a fan-in that publishes many partitions with nobody waiting stays quiet
  at INFO. The counter records every statement either way.
- ERROR `reducer post-commit exact phase wake failed; the audit and backstop still bound the miss`,
  with `queue=reducer`, `op=phase_publish_post_commit_wake`, `scope_id`,
  `generation_id`, and `outcome=error`.
- Runbook: when
  `eshu_dp_reducer_waiting_oldest_age_seconds{failure_class="cross_repo_backward_evidence_not_ready"}`
  stays above one audit interval (300 s) while the generation's phase row
  exists, or `eshu_dp_reducer_waiting_missed_wake_total` increases, the wake
  is defective. Slow evidence is not the cause.

## Proofs

Live (disposable Postgres, `go/internal/storage/postgres`):

- `TestBackwardEvidenceParkThenPublishWakesLive`: proof 1, with the
  re-wake off, so the reopen must come from the in-tx wake.
- `TestBackwardEvidencePublishThenParkReopensLive`: proof 2.
- `TestBackwardEvidenceUnrelatedPublishesStayParkedLive`: proof 3, green
  before and after.
- `TestBackwardEvidenceOverlapPostCommitRewakeLive`: proof 4.
- `TestBackwardEvidenceMissedWakeAuditReopensLive`: proof 5.
- `TestBackwardEvidenceOverlapWithoutRewakeStrandsUntilAuditLive`: proof 4
  with the re-wake removed, then proof 5 on the real overlap.
- `TestBackwardEvidenceWokenRowConvergesLive`: proof 6.
- `TestBackwardEvidenceWakeContentionLive`: proof 7.

Hermetic: `TestExactPhaseDeferralsAreWokenByTheirPublish` (proof 8 seeded
RED/GREEN pair), `TestPostCommitExactPhaseWakeTimesOutAndMeters`,
`TestPostCommitExactPhaseWakeSurvivesCallerCancel`,
`TestExactPhaseWakeLogsInfoOnlyWhenItReopens`, `TestPhaseUpsertStatementsForEnrolledAndOtherPhases`,
`TestBackwardEvidenceDeferralParksOnTheExactPhaseKey`,
`TestWaitingSatisfiedRoutesExactPhaseToTheExactQuery`, and
`backfill.TestFanInPostCommitWakeRunsAfterCommitOnly`.

Mutation checks:

- Dropping the in-tx exact wake fails proof 1.
- Dropping the post-commit re-wake fails proof 4.
- Routing exact keys to the coarse branch fails proof 3 (the audit reopens
  the waiter on an unrelated phase).
- Removing the post-commit timeout hangs
  `TestPostCommitExactPhaseWakeTimesOutAndMeters` until the test binary
  times out; logging every wake at INFO fails
  `TestExactPhaseWakeLogsInfoOnlyWhenItReopens`.

Proof 9, the cell loop on the built binary, is reported with the PR.
