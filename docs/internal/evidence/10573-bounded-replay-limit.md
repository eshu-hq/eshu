# #10573 Bounded Dead-Letter Replay Evaluates Its Limit Once

The bounded recovery replay (`RecoveryStore.ReplayFailedWorkItems` with
`Limit > 0`, `replayFailedWorkItemsBoundedTemplate` in
`go/internal/storage/postgres/projector_queue_sql.go`) carried its LIMITed
locking SELECT as a plain `IN`-subquery. The planner may pull that subquery
into an unmaterialized nested-loop semi join, re-executing the `Limit` once
per outer row while each execution sees the command's own prior updates and
picks successive candidates.

## Fix

The bounded set is selected through a `candidates AS MATERIALIZED` CTE, so
the `LIMIT $2 ... FOR UPDATE SKIP LOCKED` selection evaluates exactly once
independent of plan shape. `ORDER BY work_item_id` and `SKIP LOCKED`
semantics are unchanged, so concurrent drains still skip rows a peer holds
instead of fighting over them. The outer UPDATE rechecks terminal status and
probes candidates by primary key
(`work_item_id = ANY(ARRAY(SELECT work_item_id FROM candidates))`),
mirroring the unbounded template, which is byte-identical.

## Proof

No-Regression Evidence: before the fix, `EXPLAIN (ANALYZE, BUFFERS)` of the
rendered bounded UPDATE over a 2-row reducer drain (`Limit: 1`, Postgres 18.6
on an isolated disposable database) showed `Limit loops=3` and the statement
moved 2 rows (`Replayed=2`); after the fix the same shape shows
`Limit loops=1` and moves exactly 1 row. The RED reproduction is
`TestTerminalReopenWritersStampReopenedAtLive/bounded_dead-letter_drain`
(`r-drain-b = {status:pending ...}, want untouched dead_letter beyond the
drain bound`); it now asserts `Replayed == 1` with
`WorkItemIDs == [r-drain-a]` and passes with all 7 reopen subtests green.
The hermetic `recovery_replay_limit_test.go` suite and the replay lock-order
suites pass unchanged: `TestRecoveryReplayConcurrentReplaysNeverDeadlockLive`
ran 150 rounds with 0 deadlocks (`40P01 scoped=0 stage-only=0 bounded=0`) and
0 accounting failures, and the two-session interleave plus the EvalPlanQual
drop-row proofs pass.

No-Observability-Change: the replay emits no new spans, metrics, or logs.
Its outcome already surfaces through `recovery.ReplayResult` (`Replayed`,
`WorkItemIDs`, `SkippedSupersededGeneration`) and the existing call sites,
so operators observe the bound through the result they already consume.
