# Coordinator N-Safety (#4586)

Issue #4586 asked for coordinator high availability: "leader election, or PROVEN
N-safe with a contention proof". This is the contention proof. It came back
N-safe, so nothing here builds leader election.

The workflow coordinator is the process that decides which collector targets get
queued. Running two of them was assumed unsafe.

## What the issue suspected

Each coordinator tick rounds the clock to a 30-second bucket and folds that
bucket into the run id, the work-item id, and the generation id. Two
coordinators ticking either side of a bucket edge therefore plan the SAME
collector target under different identifiers. Those rows are legitimately
distinct, so the primary key does not collide and the insert's
`ON CONFLICT DO NOTHING` cannot collapse them. The target would end up with two
independently claimable pending work items and get collected twice.

That shape is real. The premise check confirmed it directly against the shipped
planner: the same target 200 ms apart across the edge produced

| Field | Coordinator A | Coordinator B | Same? |
| --- | --- | --- | --- |
| `scope_id` | `oci-registry://…/library/busybox` | same | yes |
| `acceptance_unit_id` | `oci-registry://…/library/busybox` | same | yes |
| `run_id` | `…continuous-20260513T150000Z` | `…continuous-20260513T150030Z` | no |
| `generation_id` | `oci_registry:f2c5b350…` | `oci_registry:432074e7…` | no |
| `work_item_id` | derived from generation A | derived from generation B | no |

## What the code actually does

The duplicate never lands. All 21 schedulers funnel through one admission guard,
`CreateRunWithWorkItemsIfNoOpenTargets`
(`go/internal/storage/postgres/workflow_control_open_targets.go`), and that guard
opens its transaction with

```sql
SELECT pg_advisory_xact_lock($1)
```

keyed on `(collector_kind, collector_instance_id)`. The key carries no bucket,
run, or generation, so two coordinators planning the same collector instance
take the *same* lock. Inside it the guard filters planned targets against a
tuple — collector kind, instance, scope, tenant, workspace, subject class,
policy revision hash, acceptance unit — that excludes `generation_id`. The
bucket moves only identifiers the filter does not look at, so coordinator B sees
A's row and admits nothing.

The lock is what makes that work. Without it, under Read Committed both
transactions take their snapshot before either commits, both read "nothing
planned", and both insert. This is an ordinary read-then-write race; the
`NOT EXISTS` cannot close it alone.

## The arms

Every arm ran against Postgres 16.14 in a throwaway container, 25 iterations
each, concurrency as goroutines inside one process.

| Arm | What it drove | Runs attempted | Runs reproduced |
| --- | --- | --- | --- |
| Premise | Do the two buckets really derive different ids for one target? | 1 | 1 (they do) |
| A | Two coordinators racing across the bucket edge, real `scheduleOCIRegistryWork` | 25 | 0 duplicates |
| A2 | Same, strictly sequential (A commits, then B ticks) | 25 | 0 duplicates |
| A3 | Run A forced terminal, then B ticks in the next bucket | 1 | 1 re-schedule (see limits) |
| B | 8 workers claiming one pending item | 25 | 0 double claims |
| C | 8 workers reaping one expired claim | 25 | 0 double reaps |
| E | The guard body run twice, differing in exactly one statement | 25 + 25 | see below |

Arm E is the decisive one. It ran the real guard body — `lockWorkflowOpenTargets`,
`workItemsWithoutOpenTargets`, `createRunWithExecutor`,
`enqueueWorkItemBatchWithExecutor` — twice, with the advisory lock as the only
difference:

```
ARM E WITHOUT advisory lock: runs=25 duplicate_pending_runs=25
ARM E WITH advisory lock:    runs=25 duplicate_pending_runs=0
```

25 of 25 versus 0 of 25. The lock is the whole mechanism.

## What the regression test now guards

Before this change, N-safety rested entirely on that one statement and its role
was documented nowhere. Deleting the call did fail one existing hermetic test
(`TestWorkflowControlStoreGuardedRunLocksCollectorInstanceOnceForTargetBatch`,
which counts the lock statement) — but *moving* it after the planned-target
read, which reopens the race completely, passed every test in the repository:

```
$ go test ./internal/storage/postgres/ ./internal/coordinator/ -count=1
ok  	github.com/eshu-hq/eshu/go/internal/storage/postgres	6.634s
ok  	github.com/eshu-hq/eshu/go/internal/coordinator	6.247s
```

Three tests now close that gap. All three drive the real
`CreateRunWithWorkItemsIfNoOpenTargets`, not a copy of it.

`TestWorkflowControlStoreGuardedRunTakesPlanningLockBeforeReadingTargets`
(hermetic, no database) records the statement order the guard issues and asserts
the lock comes before the planned-target read and before the insert. It is the
in-CI guard, because the two live tests skip without a DSN.

`TestWorkflowGuardedRunCreateAdmitsOneTargetForConcurrentCoordinatorsLive`
(real Postgres) is Arm E promoted to an assertion: 25 iterations of two
coordinators racing across the bucket edge, each iteration requiring exactly one
row, exactly one pending row, and the two returned counts summing to 1.

`TestWorkflowGuardedRunCreateWaitsOnPlanningLockBeforeReadingTargetsLive`
(real Postgres) is the deterministic half. An outside session holds the planning
lock, using the key from the production derivation. The guard must be observed
waiting on it in `pg_locks` before a competing coordinator's row is committed and
the lock released. A guard that reads outside the lock never appears as a waiter,
so this fails without depending on winning a race.

### Mutation proof

Moving the lock after the planned-target read in
`workflow_control_open_targets.go` (per-iteration lines elided):

```
--- FAIL: TestWorkflowGuardedRunCreateAdmitsOneTargetForConcurrentCoordinatorsLive (0.69s)
    workflow_control_open_targets_lock_live_test.go:199: duplicate admission in 25 of 25 runs; the collector-instance planning lock is not holding
--- FAIL: TestWorkflowGuardedRunCreateWaitsOnPlanningLockBeforeReadingTargetsLive (0.07s)
    workflow_control_open_targets_lock_live_test.go:297: enqueued = 1, want 0: the competing coordinator's open target committed before the lock was released
--- FAIL: TestWorkflowControlStoreGuardedRunTakesPlanningLockBeforeReadingTargets (0.00s)
    workflow_control_open_targets_lock_test.go:145: planning lock taken at statement 2, planned-target read at 1: the lock must come first, or both coordinators read before either writes (#4586)
FAIL
FAIL	github.com/eshu-hq/eshu/go/internal/storage/postgres	36.659s
```

25 of 25, the same rate Arm E measured with the lock stripped out.

Deleting the lock call outright, across every guarded-run test in the package:

```
--- FAIL: TestWorkflowGuardedRunCreateAdmitsOneTargetForConcurrentCoordinatorsLive (0.49s)
    workflow_control_open_targets_lock_live_test.go:199: duplicate admission in 25 of 25 runs; the collector-instance planning lock is not holding
--- FAIL: TestWorkflowGuardedRunCreateWaitsOnPlanningLockBeforeReadingTargetsLive (20.37s)
    workflow_control_open_targets_lock_live_test.go:267: the guard finished (enqueued = 1, err = <nil>) without ever waiting on the collector-instance planning lock: it read planned targets outside the lock, which is the #4586 duplicate-admission race: no ungranted advisory lock on classid=2689150001 objid=3858551466 after 20s
--- FAIL: TestWorkflowControlStoreGuardedRunLocksCollectorInstanceOnceForTargetBatch (0.00s)
    workflow_control_test.go:410: advisory lock exec count = 0, want one collector-instance planning lock
--- FAIL: TestWorkflowControlStoreGuardedRunComputesEligibleTargetsInOneQuery (0.00s)
    workflow_control_test.go:349: inserted = 1, want 2
--- FAIL: TestWorkflowControlStoreGuardedRunLocksTheKeyDerivedFromCollectorInstance (0.00s)
    workflow_control_open_targets_lock_test.go:195: advisory lock argument count = 0, want one key for one collector instance
--- FAIL: TestWorkflowControlStoreGuardedRunTakesPlanningLockBeforeReadingTargets (0.00s)
    workflow_control_open_targets_lock_test.go:138: the guard never took the collector-instance planning lock; without it two coordinators both read "nothing planned" and both insert the same target (#4586)
FAIL
FAIL	github.com/eshu-hq/eshu/go/internal/storage/postgres	30.654s
```

`ComputesEligibleTargetsInOneQuery` is collateral, not a lock assertion: its fake
serves exec results in order, and dropping the lock statement shifts that queue
by one. Ignore it when reading the list above.

## The accounting fix

The guard returned `len(eligible)` — targets that passed the eligibility read —
not rows the database accepted. The insert ends in `ON CONFLICT DO NOTHING` and
`workflow_work_items` carries partial unique indexes, so those two numbers can
differ. When they do, the coordinator's `workflow coordinator skipped duplicate
workflow work` log never fires (`go/internal/coordinator/scheduled_work.go`
compares `enqueued < len(authorizedItems)`), and an operator reads an enqueued
count for work that is not in the queue.

The reachable case is the `terraform_state` candidate index
(`workflow_control_schema_sql.go`), unique on
`(collector_instance_id, scope_id, generation_id)` for non-terminal candidate
rows. The guard's tuple also includes `acceptance_unit_id`, so it can consider
two rows distinct targets while the index considers them one. Observed:

```
ARM F2 (two acceptance units, one generation): guard returned enqueued=2 | actual rows=1 units=[acceptance-repo-1]
ARM F2 ACCOUNTING GAP: guard returned enqueued=2 but only 1 row(s) landed; the dropped acceptance unit is silent
```

`enqueueWorkItemBatchWithExecutor` now returns `RowsAffected` and the guard sums
it across batches. Both tests were written first and failed on the old code:

```
=== RUN   TestWorkflowGuardedRunCreateReportsRowsActuallyInsertedLive
    workflow_control_open_targets_lock_live_test.go:373: enqueued = 2, actual rows = 1: the guard reported work an operator cannot find in the queue, and the coordinator's "skipped duplicate workflow work" log never fires (#4586)
--- FAIL: TestWorkflowGuardedRunCreateReportsRowsActuallyInsertedLive (0.67s)
=== RUN   TestWorkflowControlStoreGuardedRunReportsRowsActuallyInserted
    workflow_control_open_targets_lock_test.go:227: inserted = 2, want 1: the count must be rows the database accepted, not the 2 targets planned (#4586)
--- FAIL: TestWorkflowControlStoreGuardedRunReportsRowsActuallyInserted (0.00s)
```

Both pass after the change.

### Reporting the two shortfalls separately

Returning only the accepted-row count fixes the number and breaks the reason.
The coordinator logs one line, `workflow coordinator skipped duplicate workflow
work` with `reason=target_already_planned`, whenever the returned count is below
what it planned. Feed it the insert count and every row a unique index refused
gets reported as a target some other run already owns — lost work, filed as a
harmless duplicate. Codex caught this on the PR.

The guard now returns both numbers (`workflow.RunAdmission`): the targets that
cleared the open-target read, and the rows the insert accepted. The coordinator
logs the gap it actually saw. A target the guard dropped stays the Info
duplicate-skip line; a row the guard admitted and the database refused is a
Warn, `workflow coordinator lost admitted workflow work at insert`, with
`reason=insert_conflict_dropped_row` and a `dropped_work_items` count.

Both new tests were written first and fail when the production line is broken.
Collapsing the condition back to the single count (`admission.InsertedWorkItems
< len(authorizedItems)`) reproduces the exact misreport:

```
--- FAIL: TestCreateWorkflowWorkIfNoOpenTargetsReportsInsertConflictSeparately (0.00s)
    service_test.go:434: logs = {"level":"INFO","msg":"workflow coordinator skipped duplicate workflow work",...,"skipped_work_items":0,"reason":"target_already_planned"}
        , want no already-planned reason: the guard admitted both targets, so nothing was skipped as a duplicate (#4586)
```

Dropping the eligible count in the store (`workflow.RunAdmission{}` instead of
`{EligibleTargets: len(eligible)}`) fails the hermetic and the live test:

```
--- FAIL: TestWorkflowControlStoreGuardedRunReportsRowsActuallyInserted (0.00s)
    workflow_control_open_targets_lock_test.go:267: EligibleTargets = 0, want 2: the open-target guard admitted both targets, and reporting fewer blames it for a row the database dropped (#4586)
--- FAIL: TestWorkflowGuardedRunCreateAdmitsOneTargetForConcurrentCoordinatorsLive (1.77s)
    workflow_control_open_targets_lock_live_test.go:198: iteration 1: admitted sum = 0 (0 + 0), want exactly 1 target past the open-target guard
```

One INSERT with `ON CONFLICT DO NOTHING` cannot write more rows than it was
handed, so the batch helper checks `RowsAffected` against the batch size before
narrowing it to `int`. That keeps the value inside a range `int` holds on any
platform, and a driver reporting anything else is a broken count rather than a
number to pass to an operator. Removing the check:

```
--- FAIL: TestWorkflowControlStoreGuardedRunRejectsImpossibleRowsAffected (0.00s)
    workflow_control_open_targets_lock_test.go:294: CreateRunWithWorkItemsIfNoOpenTargets() error = nil (admission = {EligibleTargets:2 InsertedWorkItems:3}), want a rejected rows-affected count
```

## Limits

State these next to any claim built on this page.

- Only the OCI registry scheduler was driven end to end. The other 20 schedulers
  share the guard by code reading, not by execution.
- Concurrency was goroutines in one process against one Postgres, not separate
  hosts. The guard's safety argument is a database lock, which does not care, but
  no multi-host run happened here.
- 25 iterations per arm, Postgres 16.14 at the default Read Committed isolation.
  A different isolation level was not tested.
- The proof covers admission, claiming, and expired-claim reaping. It does not
  cover every coordinator side effect.

## Known, out of scope, needs its own work

**The `terraform_state` index is not exact.** It keys on
`(collector_instance_id, scope_id, generation_id)`; the guard keys on that plus
tenant, workspace, subject class, policy revision hash, and acceptance unit.
Neither is a subset of the other. When two repositories share one Terraform state
object, the index collapses two rows the guard considers distinct targets, and
one repository's work silently disappears. The accounting fix makes that visible
in the coordinator log — as a warning about a dropped insert, not as a duplicate
skip — instead of invisible; it does not fix the collapse. That needs its own
measurement and its own change.

**Phase skew is an owner decision, not a bug.** Arm A3: with run A terminal, a
coordinator in a later phase can start the next collection for the same target
sooner than the nominal interval, because the open-target predicate only excludes
targets held by a non-terminal run. Two coordinators can therefore collect more
often than one would. There is never more than one pending item for the target at
a time, and every arm confirms that. Whether faster-than-interval collection is
acceptable is a product call.

**Leader election is not needed and was not built.** The evidence says N-safe.
Adding an election would contradict it.

## Markers

No-Regression Evidence: the change adds no query, no index, and no round trip on
the admission path. `enqueueWorkItemBatchWithExecutor` now reads `RowsAffected`
off the result it already had. Arm A (two coordinators, real scheduler, 25 runs)
and Arm E (guard body, 25 runs) both completed with the shipped lock in place and
produced zero duplicates; the concurrent regression test runs its 25 iterations
in 2.37s against a local Postgres 16.14, so the guard is not a throughput
concern at coordinator tick rates. The lock is the shipped behavior, not new
serialization: this change adds tests and an accurate count, and does not narrow
concurrency anywhere.

Observability Evidence: the accounting fix restores an existing operator signal
and splits it in two, because the two shortfalls need different responses.
`workflow coordinator skipped duplicate workflow work` (Info,
`go/internal/coordinator/scheduled_work.go`) still fires when the open-target
guard drops a planned target, carrying `collector_kind`,
`collector_instance_id`, `planned_work_items`, `enqueued_work_items`,
`skipped_work_items`, and `reason=target_already_planned` — an open run already
owns that target, which is the guard working. `workflow coordinator lost
admitted workflow work at insert` (Warn) is new and fires when the database
refuses a row the guard admitted, carrying the same identity fields plus
`admitted_work_items`, `dropped_work_items`, and
`reason=insert_conflict_dropped_row` — that one is work nobody will collect, and
it points at the `terraform_state` index collapse described above. Before this
change neither line could fire for a dropped insert, because the guard counted
those as enqueued. No metric, span, or knob was added or renamed.
