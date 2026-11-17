# #10828 NVD baseline per-year plan row: evidence

The guarded workflow admission now writes one row per baseline year into the new
table `vulnerability_nvd_baseline_plans` (migration 303), in the same
transaction as the work items it admits. Binding ruling: 2026-11-17, sections 3
and 4 (the table, the writer, the clock, the read key). This note is the tracked
record the performance and telemetry gates ask for. Design:
`docs/internal/design/10809-baseline-resume-position.md`, section "Per-year plan
row".

Scope: the table, the write in the admission transaction, the keyed read, the
telemetry. The planner wiring and the per-tick read (#10746), the adaptive
interval (#10814), and row retention (#10831) are not part of this change.
Nothing reads the rows and the planner mints no baseline chunk in production
yet, so the table stays empty in every deployment until #10746 lands.

## No-Regression Evidence:

- Conflict domain: the plan row of one `(baseline_run_id, year)`, and the
  per-instance advisory lock the admission already takes for
  `(collector_kind, collector_instance_id)`. There is no new lock, and none is
  held across planning. The plan write is the last statement before the commit,
  so lock order is advisory lock, run row, work items, slot progress, plan rows.
  Two instances that share one baseline run id do not share an advisory lock key,
  so the plan row lock is what serializes them; the writer sorts its rows by
  `(baseline_run_id, year)` so both take row locks in one order.
- Worker counts, leases, batching, and retries are unchanged. The admission is
  still one transaction with a whole-transaction retry of up to 3 attempts on
  SQLSTATE `40001` and `40P01`. The plan write's errors wrap the driver error,
  so a deadlock in it is retried like any other statement of the transaction,
  and the counter is added only for the attempt that commits.
- Hot path: an admission with no baseline chunk issues the same statements as
  before. `NVDBaselinePlans` returns nothing for any other work item, so the
  coordinator takes the same store call it took before (the plain guarded
  admission, or the slot-progress admission when slots advance), and
  `TestWorkflowControlStorePlainEntryPointsWriteNoPlanRows` shows those two entry
  points issue no statement that names the plan table. A baseline admission adds
  one upsert. The planner holds at most `max_in_flight_years` lanes open, with a
  ceiling of 4 (`NVDBaselineMaxInFlightYearsCeiling`), so a normal admission
  carries at most 4 rows; the writer itself sets no row cap.
- Row counts: the table is empty before and after this change in production.
  In the live proof, one admission of two baseline chunks and one ordinary
  target commits 3 work items and 2 plan rows, and none of the rolled-back,
  guard-dropped, insert-dropped, or terminal-run-skipped admissions leave a row.

### Plan shape on a populated fixture

Disposable PostgreSQL 18.6, `TestNVDBaselinePlanQueriesAreKeyedOnAPopulatedFixtureLive`.
The fixture is 150,025 work items (60,000 of the `vulnerability_intelligence`
kind) and 100,025 plan rows over 4,000 runs, with a 25-year target run whose
years cover every queue status, a deleted item, and a different collector kind
that reuses a plan row's scope id. The assertion reads index names and
`Index Cond`, never a cost or a timing.

- Read (`NVDBaselinePlanReadQuery`): 2 plan nodes and 100 shared buffer touches.
  The plan rows come from `vulnerability_nvd_baseline_plans_pkey`, and each
  year's in-flight probe is an index probe on
  `workflow_work_items_collector_scope_generation_updated_idx` with both
  `collector_kind` and `scope_id` in the `Index Cond`. No sequential scan of
  either table.
- The `collector_kind` equality is load-bearing. The same read without it was
  planned off the broad queue-depth index and touched 495 shared buffers, about
  five times as many, and was not keyed by `(collector_kind, scope_id)`. No index
  leads with `scope_id` alone, so the equality is what makes the probe an index
  probe.
- Write (`NVDBaselinePlanUpsertQuery`): 21 shared buffer touches, conflict
  arbiter `vulnerability_nvd_baseline_plans_pkey`.
- No secondary index is added. The primary key is the only access path of both
  statements; `TableIsABarePrimaryKeyTable` fails if one appears.

### DDL lock evidence for migration 303

PostgreSQL 18.6, a scratch schema holding stand-ins for `workflow_work_items`
and `workflow_runs`, and the real `303_vulnerability_nvd_baseline_plans.sql`.

- First application while another session held `ACCESS EXCLUSIVE` on both
  workflow tables, with `lock_timeout = 300ms`: the migration completed in
  3.87 ms. The relation locks it held before commit were `AccessExclusiveLock`
  and `ShareLock` on the new table and `AccessExclusiveLock` on its primary key
  index, and nothing on any existing relation. The table has no foreign key, so
  it needs no lock on a referenced table.
- Re-application with the table present while another session held
  `ACCESS EXCLUSIVE` on it: the `CREATE TABLE IF NOT EXISTS` returned
  `relation ... already exists, skipping` in 0.207 ms. A `SELECT count(*)` on the
  same table under the same condition failed with
  `canceling statement due to lock timeout`, so the no-wait result is real and
  not an artifact of a missing lock.
- No index is built, so there is no `CREATE INDEX CONCURRENTLY` question. A
  later index on a populated table would need the repo's concurrent-index
  migration rules.

### Runtime lock evidence

`TestWorkflowControlBaselinePlansLockingLive` reads the admission backend's own
granted locks from `pg_locks` just before the commit, once for a plain admission
and once for a plan-carrying one, and compares them.

- Advisory locks: exactly one in both, the instance planning lock.
- Locks added by the plan write: exactly `RowExclusiveLock` on
  `vulnerability_nvd_baseline_plans` and on
  `vulnerability_nvd_baseline_plans_pkey`. Nothing was removed.
- Mutants: a second `pg_advisory_xact_lock` in the plan write fails the test
  with two advisory locks; `LOCK TABLE ... IN SHARE ROW EXCLUSIVE MODE` before the
  upsert fails it with a `ShareRowExclusiveLock` among the added locks.

### Concurrency proof: two instances, one baseline run, opposite year order

`TestWorkflowControlBaselinePlansLockingLive`, first subtest. Instances
`vuln-a` and `vuln-b` share one baseline run id, so they take different advisory
locks and meet only on the plan rows. Both years already have a row. A
`BEFORE INSERT OR UPDATE` trigger on the plan table sleeps 300 ms per row event,
so each admission holds its first row before either asks for its second. The
coordinator-supplied plan order is opposite on purpose (2026, 2025 and 2025,
2026).

- With the writer's sort: both admissions commit, 2 transactions are begun in
  total (no retry), and both years name one admission's chunks.
- Mutant, sort removed: 3 transactions are begun, which is a retry after a
  deadlock. The server log of the same run reads `ERROR: deadlock detected`,
  `Process 37788 waits for ShareLock on transaction 20505; blocked by process
  38291` and `Process 38291 waits for ShareLock on transaction 20504; blocked by
  process 37788`, both backends inside the plan upsert, with `CONTEXT: while
  inserting index tuple (0,1) in relation "vulnerability_nvd_baseline_plans"`.
- The sort is in Go, over the bound arrays, and the statement carries no
  `ORDER BY`. That is safe only if PostgreSQL processes the rows in array order,
  so it was measured on PostgreSQL 18.6, on a scratch copy of the table with a
  `BEFORE INSERT OR UPDATE` row trigger that reports each row. A four-row upsert
  whose array was deliberately out of key order (2018, 2026, 2002, 2010) was
  processed in exactly that order, not key order, on the insert path and on the
  update path (rows already present, where the trigger fires twice per row, still
  in array order). The order held under the default plan, with `enable_nestloop`
  off, with `enable_nestloop` and `enable_hashjoin` off, and with
  `enable_nestloop` and `enable_mergejoin` off. The plan is Insert, Nested Loop,
  CTE Scan on the clock CTE, Function Scan on `unnest`, with no Sort node. With
  nested loops disabled the plan is the same node marked `Disabled: true`, because
  a cross join has no equality clause for a hash or merge join to use. The live
  no-sort mutant above is the direct proof on the real statement.

### Clock proof

`TestWorkflowControlBaselinePlansLive`, subtest "stamped from the database clock
after the planning-lock wait": the test holds the instance's planning lock from
another session, starts the admission so its transaction begins and waits, then
releases the lock 500 ms later. `last_planned_at` must not be earlier than the
database clock at the release. With `now()` in place of `clock_timestamp()` the
stamp was 10:05:43.323563 against a release at 10:05:43.817783, about 494 ms
early: the transaction start, which can predate the previous lock holder's stamp
and shorten the planner's pace wait.

### Atomicity and drop-case proofs

`TestWorkflowControlBaselinePlansLive` runs against a real isolated schema with
the full migration set.

- A refused plan write (a trigger that raises) leaves 0 rows in `workflow_runs`,
  `workflow_work_items`, `vulnerability_nvd_baseline_plans`, and
  `owned_target_slot_progress`; the same admission against a healthy table commits
  1, 2, 2, and 2 rows.
- A failed `Commit` leaves no plan row. A plan upsert issued on the pool instead
  of the transaction survives it, which is how the mutant is killed.
- The open-target guard drops a chunk whose target is still open under another
  run: no row is written and the existing row keeps its scope id and its
  `last_planned_at`. A fully dropped admission moves no row.
- A guard-admitted chunk dropped by the `work_item_id` primary key (a finished run
  owns the id) writes no row, while the other chunk of the same admission writes
  its row: `EligibleTargets` 2, `InsertedWorkItems` 1, `BaselinePlansRecorded` 1.
- A terminal-run skip writes no row.

## Mutants killed

Each mutant was applied to the production source, the named tests failed, and
the source was restored. The IDs are the working labels of the proof runs: M is
the plan writer and reader, S the root store (hermetic), T telemetry, C the
coordinator, and L the live admission proofs. M1 and C5 were first applied by
deleting code and failed to compile; their rows are the compiling forms.

| ID | Mutation | Killed by |
|---|---|---|
| M1 | the writer's sort comparator never reorders | `TestRecordNVDBaselinePlansWritesOneOrderedUpsert` |
| M2 | `now()` for `clock_timestamp()` | `TestRecordNVDBaselinePlansStampsWithTheDatabaseClock`; live `StampIsTheStatementClock` |
| M3 | `WHERE` guard on the update | `TestRecordNVDBaselinePlansIsLastWriterWins` (query text); live `UpsertIsLastWriterWins` (a row stamped an hour ahead of the clock still yields to the next insert) |
| M4 | `DO NOTHING` (first writer wins) | live `UpsertIsLastWriterWins`; `TestRecordNVDBaselinePlansIsLastWriterWins` |
| M5 | allow a duplicate (run, year) | `TestRecordNVDBaselinePlansRefusesAnAmbiguousBatch` |
| M6 | drop `collector_kind` from the in-flight probe (a form that keeps `$2` referenced, so the kill is semantic and not a bind-count error) | `TestReadNVDBaselinePlansIsKeyedByCollectorKindAndScope`; live `InFlightFollowsTheQueueOpenSetOnly`; `TestNVDBaselinePlanQueriesAreKeyedOnAPopulatedFixtureLive` (plain form) |
| M7 | narrow the open set to pending and claimed | live `InFlightFollowsTheQueueOpenSetOnly`; `TestReadNVDBaselinePlansIsKeyedByCollectorKindAndScope` |
| M8 | return an empty map on a read error | `TestReadNVDBaselinePlansRefusesWhatItCannotAnswer` |
| S1 | write a plan row for every supplied plan | `TestWorkflowControlStoreWritesPlansOnlyForInsertedChunks` |
| S4 | skip the pre-flight validation | `TestWorkflowControlStoreRefusesBaselinePlansItCannotTrust/duplicate_year` |
| S5 | accept any rows-written count | `TestWorkflowControlStoreRollsBackWhenThePlanWriteFails/surplus_row_count` |
| S6 | swap `recorded` and `not_inserted` | `TestWorkflowControlStoreCountsBaselinePlanRowsAfterCommit/guard_emptied_the_admission` |
| S8 | write the plan row before the slot advance | `TestWorkflowControlStoreSlotAdvanceAndBaselinePlanShareOneTransaction` |
| S9 | drop the item-kind check | `TestWorkflowControlStoreRefusesBaselinePlansItCannotTrust/item_of_another_collector_kind` |
| S10 | drop the scope-agreement check | `TestWorkflowControlStoreRefusesBaselinePlansItCannotTrust/run_disagrees_with_the_scope` |
| T1 | swap the outcome labels | `TestNVDBaselinePlanRowsCountsRecordedAndNotInserted` |
| T2 | record zero and negative counts | `TestNVDBaselinePlanRowsOutcomeSetIsClosed` |
| C1 | derive plans from the planned items, not the authorized ones | `TestCreateWorkflowWorkSendsOnlyAuthorizedBaselinePlans` |
| C2 | never take the plan-carrying path | `TestCreateWorkflowWorkRefusesBaselineChunksAgainstAStoreThatCannotRecordThem`; `TestCreateWorkflowWorkLogsTheBaselinePlanOutcome` |
| C3 | fall back to the plain path when the store cannot record plans | `TestCreateWorkflowWorkRefusesBaselineChunksAgainstAStoreThatCannotRecordThem` |
| C4 | drop the slot advance when plans are present | `TestCreateWorkflowWorkKeepsTheSlotAdvanceWithBaselinePlans` |
| C5 | derive a plan for any item kind | `TestNVDBaselinePlansIgnoresEverythingElse` |
| C6 | derive a plan from every item (ignore the scope grammar) | `TestNVDBaselinePlansDerivesOnePlanPerBaselineChunk`; `TestNVDBaselinePlansIgnoresEverythingElse` |
| C7 | drop the admission log | `TestCreateWorkflowWorkLogsTheBaselinePlanOutcome` |
| C8 | log the planned count as the recorded count | `TestCreateWorkflowWorkLogsTheBaselinePlanOutcome` |
| LA | upsert on the pool, outside the transaction | live `a failed commit leaves no plan row behind` |
| LB | write a row for every supplied chunk | live guard-dropped and primary-key-conflict subtests |
| LC | `now()` for `clock_timestamp()` | live `the plan row is stamped from the database clock after the planning-lock wait` |
| LD | write rows on the terminal-run skip | live `a terminal-run skip writes no plan row` |
| LE | write rows when the guard drops everything | live `the open-target guard drops a chunk and its plan row` |
| LF | no sort in the writer | live locking test, opposite-order subtest (3 begins, not 2) |
| LG | a second advisory lock in the plan write | live lock-differential subtest |
| LH | a `SHARE ROW EXCLUSIVE` table lock in the plan write | live lock-differential subtest |

## Observability Evidence:

- Metric: `eshu_dp_nvd_baseline_plan_rows_total`, label `outcome` with the closed
  values `recorded` and `not_inserted`, one add per admission that carried
  baseline chunks, after the commit and never for a rolled-back admission. The
  counter adds only positive counts, and a nil `Instruments` is safe. No run,
  year, or scope id is ever a label.
- Log: the coordinator emits `workflow coordinator admitted NVD baseline chunks`
  at INFO with `baseline_plans`, `baseline_plans_recorded`, and
  `baseline_plans_not_inserted`, plus the collector kind, instance id, and trigger
  kind, only when a tick carried baseline chunks.
- Failure class: a failed plan write returns the driver error under
  `create guarded workflow run: record nvd baseline plans: ...` and fails the
  admission, so the coordinator's existing admission error log carries it. A
  store without the write fails the admission with
  `workflow coordinator store cannot record NVD baseline plans`.
- Operator reading: `recorded` without `not_inserted` is a healthy baseline.
  A rising `not_inserted` means the planner keeps minting a chunk for a year that
  already has an open one; it is never a lost plan row, because a row is written
  only with the item it names. A coordinator that logs the store-missing error
  is running against a store older than this change.
- Coverage row: the NVD baseline row of
  `docs/public/observability/telemetry-coverage-collectors.md` names the new
  metric, the log, and the three owner files.

## Known limits

- The in-flight status set (`pending`, `claimed`, `failed_retryable`,
  `expired`) is the repo's non-terminal set, the one the queue-depth gauge and the
  work-item partial unique index use. It is a contract the #10746 wiring and the
  #10814 planner read; a change to it moves all three.
- `last_planned_at` is the database clock. A future-dated value from a stepped
  clock is the planner slice's concern, as it is for the trip latch.
- The last writer wins. If two coordinators mint different positions of one year
  in the same tick, the row names the later insert and the other chunk is
  untracked by the plan row. The one-chunk-per-year rule makes that a planner
  bug, not a normal path.
- Two instances configured with one baseline run id would collect the same
  chunks twice. That is a configuration hazard for the #10746 wiring, not a
  property of the row.
- Rows are never deleted by this change. Retention is #10831.
- A 303 binary against a schema older than 303 fails a baseline admission with
  an undefined-table error and leaves every other admission untouched.
