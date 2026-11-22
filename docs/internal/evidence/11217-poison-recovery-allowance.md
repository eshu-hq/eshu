# Poison Recovery Retry Allowance Evidence (#11217)

The reducer's poison-liveness auto-retry sweep (`RecoverDeadLettersQuery`)
now gives a recovered projector row the retry allowance a new row gets. It
appends `queuestore.ReplayRetryAllowanceAssignments`, the fragment the three
operator replay writers carry since #11182. It also raises a projector row's
`attempt_count` to at least 1. This note records why the sweep did not get
slower and how an operator sees the new state.

## No-Regression Evidence

No-Regression Evidence: the change adds three assignments to the SET list of
rows the sweep already locks and updates. They write `attempt_count`,
`replay_attempt_baseline` and `admission_wait_attempts`. No index names any of
those columns, so the update adds no index maintenance. `status` already
changes and is indexed, so HOT eligibility is unchanged. The selection, the
`poison_locked` lock step (`FOR NO KEY UPDATE` in `work_item_id COLLATE "C"`
order), the status recheck, the `LEAST` cap and `RETURNING` are byte-identical.

The theory shim ran on a disposable PostgreSQL 18.6 database built from the
production bootstrap (359 migrations, through 334). It seeded 208 dead letters:

- eight target rows covering projector attempt 3, attempt 0 (the operator
  skip), the admission-budget death at 48 claims and 47 waits, a previously
  replayed row, a row at its recovery budget, and reducer rows;
- 200 superseded dead letters, which have a newer generation and are never
  selected;
- 5,000 succeeded rows, followed by `ANALYZE`.

It ran `EXPLAIN (ANALYZE, BUFFERS)` of the shipped and the patched statement
inside `BEGIN`/`ROLLBACK` with `$2 = 1` and `$3 = 200`. The two plans are node
for node identical: a `diff` with timings and buffer counts stripped exited 0.
The plan is a bitmap scan of `fact_work_items_status_idx` for the
`dead_letter` subset, then a hash anti-join against `scope_generations` with
`Limit` and `Sort`. After that comes the `LockRows` step over a sort on
`work_item_id COLLATE "C"`, then the target `Bitmap Heap Scan` (status index
AND primary key).

| Statement | Rows moved | Shared hits | Execution |
| --- | --- | --- | --- |
| shipped | 7 | 174 | 0.483 ms |
| patched | 7 | 173 | 0.436 ms |

One sample each, so the timing difference is noise, not a speedup.

The per-row bound is unchanged. `poison_recovery_attempts < $2` is still the
only gate on the sweep. With budget M
(`ESHU_POISON_LIVENESS_MAX_RECOVER_ATTEMPTS`, default 1), a projector row gets
at most 3 x (1 + M) counting projection claims and 48 x (1 + M) admission
waits before it stays `dead_letter`. A non-retryable failure still
dead-letters it on the first claim. `TestPoisonSweepBudgetBoundsRecoveriesLive`
proves M = 2: the row is recovered twice, takes three projection failures each
time, and the third sweep moves nothing at attempt 9.

The concurrency contract is unchanged: no new lock, lock order or lock
strength. The proofs below race the sweep against each replay writer with each
side holding the row lock first, and race two sweeps. In every case exactly one
writer moves the row. A committed `attempt_count` change on a still-dead row
while the sweep waits on the lock gives baseline = the committed value (the
Read Committed recheck reads the newest row version). The existing
`TestPoisonRecovery*` lock-order tests pass unchanged: 150 rounds, zero 40P01.
So do the write-time race test and the reopen-stamp tests.

Live proofs (PostgreSQL 18.6, production bootstrap, real `ProjectorQueue`
Claim and Fail):

- `go/internal/query/admin/poison_sweep_allowance_live_test.go`: the
  attempt-3 regression (red before the fix: `dead_letter` on the first
  failure), the admission budget (red: died after 1 wait instead of 48), the
  M = 2 loop bound, the claim fence, the attempt-0 operator skip, and the
  first-failure backoff.
- `go/internal/query/admin/poison_sweep_race_live_test.go`: the sweep against
  the admin replay, the recovery replay and the bounded drain, each side
  holding the lock first; two sweeps; the committed change while waiting.
- `go/internal/storage/postgres/poison/liveness_retry_allowance_live_test.go`:
  the invariant table over stage {projector, reducer} x `attempt_count`
  {0, 1, 3} x admission waits {0, 2}.
- The hermetic lockstep guard `TestReplayAllowanceCarriersRaiseAttemptCount`
  reads the shipped constants and covers the sweep as the fourth carrier.

## Observability Evidence

Observability Evidence: this change adds no metric, span or log field. The
sweep keeps counting its recoveries on `eshu_dp_poison_liveness_recovered_total`
and its failures on `eshu_dp_poison_liveness_failures_total`. A recovered row
that fails again now shows up on the projector counters exactly as a new row
does, with the same labels and emit points in `ProjectorQueue.Fail`:
`eshu_dp_projector_retry_surge_total`,
`eshu_dp_projector_admission_wait_retries_total` and
`eshu_dp_queue_dead_letters_total`. Before the fix, a recovery was followed at
once by a dead letter on the next failure.

The per-row state is durable and an operator reads it directly:

```sql
SELECT status, attempt_count, admission_wait_attempts, replay_attempt_baseline,
       attempt_count - admission_wait_attempts - replay_attempt_baseline AS projection_claims_used,
       payload ->> 'poison_recovery_attempts' AS poison_recoveries
FROM fact_work_items
WHERE work_item_id = '<work_item_id>';
```

A sweep-set baseline is told apart from a replay's by
`payload ->> 'poison_recovery_attempts'` on the row and by the absence of a
replay event (`POST /api/v0/admin/replay-events/query`); the sweep writes no
replay event. At 3 AM, an operator who sees
`eshu_dp_poison_liveness_recovered_total` rise and the dead-letter gauges
(`eshu_dp_poison_dead_letter_items`) not fall reads the row. It shows how
many claims the recovery allowed and how many recoveries are left.
