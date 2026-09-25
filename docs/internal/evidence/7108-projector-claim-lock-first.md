# #7108 projector claim: lock-first maintenance CTEs

Root-Cause Evidence: the ops-qa Postgres log for 2026-09-24/25 shows four
`deadlock detected` reports on `fact_work_items`, and every participant is the
projector claim statement (`WITH source_scoped_projector_work AS ...`). The
03:11:07Z report is `ShareLock on transaction` edges plus one
`ExclusiveLock on tuple (4206,9)` edge. Before this change the claim had four
blocking, data-modifying CTEs: stale-duplicate reclaim, supersede,
scope-generation supersede and sibling reclaim. When `$4 = ''` these scan every
projector row. Only `candidate` used `SKIP LOCKED`. Concurrent claimers therefore
waited on each other's maintenance row locks in plan order and closed wait
cycles. A local reproduction confirmed this. `TestProjectorClaimConcurrentLoadHasNoDeadlock`
(`projector_queue_claim_deadlock_live_test.go`) ran 16-32 concurrent
`ProjectorQueue.Claim`/`Ack`/`Fail` workers, 150 ms leases and a concurrent
generation producer against postgres:16 with the full bootstrap schema. The
shipped statement produced 30, 35, 26 and 3 SQLSTATE 40P01 per run, and all 101
participants in the first run's log were the claim statement. The production
failure was then fatal because `projector.Service` cancelled every worker on any
`Claim` error.

## Change

- Every maintenance CTE in `projector_queue_claim_sql.go` now selects its rows
  with `FOR NO KEY UPDATE ... SKIP LOCKED`. The locking SELECT repeats the
  row-self predicates (status, stage, lease expiry), and the UPDATE touches only
  the ids that SELECT locked and applies the same predicates again.
- Supersede locks the `scope_generations` row first and then the work row.
  Every work row the statement locks there is therefore also superseded there.
- The candidate excludes the snapshot's `supersedable_projector_generations`
  set, not only the rows this statement superseded. A stale row whose
  generation or work row another transaction holds is never claimed. The
  oldest-ready-row subquery still skips only rows superseded here, so that scope
  yields no claim this round rather than a newer generation.
- `NO KEY UPDATE` is the lock the UPDATE itself takes. It stays compatible with
  the `KEY SHARE` locks that `fact_records` foreign-key inserts hold on
  `scope_generations`.
- The claim statement now never waits on a row lock, so it cannot join a wait
  cycle.
- Defense in depth: `ProjectorQueue.Claim` retries 40P01/40001 up to three times
  with jittered backoff. Each retry increments
  `eshu_dp_queue_claim_conflict_retries_total{queue,failure_class}` and logs a
  warning with `failure_class`. When every attempt conflicts, `Claim` returns
  `failure.ErrWorkClaimConflict`, and `projector.Service` logs it and polls again
  instead of cancelling its workers. Every other Claim error stays fatal.

Semantics kept, and proven on both the old and new statement by
`TestProjectorClaimMaintenanceSemantics`:

- stale pending and dead-lettered generations are superseded along with their
  generation rows;
- an expired duplicate beside a live lease is reclaimed to `retrying`;
- expired siblings of the claimed scope are reclaimed to `retrying`.

The existing projector live suites (supersession, attempt fence, attempt reclaim
contention, heartbeat and ingestion lock order, stranded retry recovery) pass
unchanged.

## Proof

No-Regression Evidence: a contention A/B on the same postgres:16 container,
32 workers, 40 s per run, with interleaved order and alternating first mover.
Deadlocks are read from `pg_stat_database.deadlocks`, because `Claim` now
retries a 40P01 internally.

| run | shipped: server deadlocks | lock-first: server deadlocks |
| --- | --- | --- |
| 1 | 35 | 0 |
| 2 | 13 | 0 |
| 3 | 97 | 0 |
| 4 | 0 | 0 |

An earlier batch gave 112/74/119/4 for the shipped statement and 0 x 4 for the
rewrite. Five 60 s soak runs of the rewrite at 16 and 32 workers also gave 0.

The following behaviour is new and each case is RED on the shipped statement:

- `TestProjectorClaimDoesNotWaitOnLockedMaintenanceRows` holds row locks on a
  stale work row, a stale generation row, an expired duplicate and an expired
  sibling. The claim returns in about 10 ms; the shipped statement blocks until
  the test's 3 s deadline.
- A second claim made while those locks are held returns no work. It must not
  take the stale row whose generation is locked.
- After the locks are released, the next claims supersede and reclaim every
  held row.

`TestProjectorClaimLockRechecksRowsChangedAfterSnapshot` is the EvalPlanQual
proof. A test-only trigger pauses the claim inside its first UPDATE, after the
statement snapshot. While it is paused, another session claims the row the
candidate would pick and renews the lease the duplicate reclaim would take.
The claim drops both rows and claims the next eligible generation.

Mutation probes, each killed by a named test:

- removing `SKIP LOCKED` from any of the four lock CTEs;
- excluding only superseded rows in the candidate;
- dropping the lease-expiry predicate from the duplicate reclaim or from the
  candidate;
- a one-attempt conflict retry;
- dropping 40001 from the retry classifier.

Claim cost was measured with `EXPLAIN (ANALYZE, BUFFERS)` inside
BEGIN/ROLLBACK. There were 11 interleaved runs per statement with alternating
first mover. The seeded backlog has, per scope, 8 historical generations, 3
reducer rows per generation, and a mix of pending, stale, expired and live
leases.

| backlog | shipped median | lock-first median | shared hits shipped -> lock-first |
| --- | --- | --- | --- |
| 2,000 scopes, 20k projector rows, 60k reducer rows | 112.35 ms | 115.01 ms | 179,744 -> 182,268 (+1.4%) |
| 20,000 scopes, 200k projector rows, 600k reducer rows | 12.30 s | 14.87 s | 11,664,539 -> 11,693,079 (+0.24%) |

Wall time at 20k scopes is inconclusive under host load, and buffer hits are
the proxy this note relies on. An independent interleaved re-measurement (21
runs at 2k scopes, 9 at 20k scopes, a different seed) gave a 20k median of
484.9 ms shipped versus 484.6 ms lock-first, a ratio of 0.999.

The 20k wall times ranged 7.6-19.5 s (shipped) and 7.1-21.4 s (lock-first) on a
host with load average 31-55, so their medians are not comparable. Buffer
counts, which do not depend on host load, differ by 0.24%. In both plans the
maintenance CTEs cost about 0.4-0.5 s. The candidate CTE costs 7.4-9 s, spent in
its per-row correlated subplans (in-flight source count and the
oldest-ready-row subquery). This change leaves the candidate unchanged. That
pre-existing claim cost at 200k projector rows is a separate finding.

Observability Evidence: `eshu_dp_queue_claim_conflict_retries_total` (`queue`,
`failure_class` = deadlock | serialization_failure) counts every storage-level
retry. Each retry logs a `projector claim conflict; retrying claim` warning with
`failure_class`, `attempt`, `max_attempts` and `lease_owner`, which an operator
can join to the Postgres deadlock report. When a worker survives exhausted
retries, it logs `failure_class=projector_claim_conflict`. Claim latency stays on
`eshu_dp_queue_claim_duration_seconds{queue="projector"}`. After this change the
retry counter should stay at zero; a nonzero rate means a new lock-order
conflict to investigate.

Known remaining risk: the harness observes two overlapping projector leases in
one scope, and it observes them more often after this change than before it.
Measured distinct overlapping-lease pairs, shipped statement versus lock-first:

| run set | shipped | lock-first |
| --- | --- | --- |
| author, 4 x 40 s, 32 workers | 5 and 3 pairs (two batches) | 8 and 5 pairs |
| reviewer, 5 x 30 s, 32 workers, 40 scopes | 1 pair in 3,208 claims | 11 pairs in 4,470 claims |
| reviewer, 2 x 60 s, 10k-scope backlog | 0 pairs in 1,020 claims | 2 pairs in 2,837 claims |

The double-lease rate in the harness is higher after this change. The race is
the same pre-existing one and is tracked in #7115; this change does not fix it.
Two claimers whose snapshots disagree on the scope's oldest ready row, for
example a retrying older generation versus a freshly enqueued newer one, lock
different rows. The snapshot-only in-flight guard sees neither uncommitted
claim. Closing it needs a scope-level exclusion.

Hypothesis, not proven: the shipped statement's blocking waits serialised
claimers, and removing the waits raises concurrency enough to unmask the race
more often. The mechanism was not isolated. The harness shape is extreme (32
workers on 40 scopes with 150 ms leases); production pods run far fewer
workers, so the absolute exposure there is smaller. Whether #7115 lands before
or with this change is an owner decision.
