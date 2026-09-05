# Reducer ACK and completion fanout lock ordering (#6488)

Batch ACK and cross-scope completion fanout can update the same reducer work
items across scopes. Their previous SQL left row acquisition order to the
executor. The production contention probe reproduced a PostgreSQL deadlock;
the change gives those statements a common work-item order and acquires queued
completion events only after the consumer locks.

Focused ordering and replay validation passed on the initial candidate after
the same tests failed on the baseline. A subsequent foreign-key compatibility
regression failed on that candidate; the work-lock-strength correction awaits
live validation. Actual plans confirm ordered locks and drained dependencies on
the 64-consumer fixture. Broader validation remains pending; the single timing
sample does not establish a speedup or a general performance guarantee.

## Observed failure and ownership

Performance Evidence: on PostgreSQL 18.6, the unchanged production statements at
`36f6e741c6bbec0e59113b4ff131f2d960d1257f` failed with SQLSTATE `40P01` at trial 6
of `TestReducerContentionGateAckFanoutProbe`, with an ACK batch of 64. Five
one-row overlap controls preceded the larger batches. The fixture used 64
active scopes and supply-chain consumer rows, with reverse lexical identities
relative to heap insertion. ACK and CI/CD completion fanout used separate
connections, a shared start barrier, and the actual queue methods.

The captured server detail identifies both blocked transactions and their SQL;
the probe command exited 1. Coordinator artifacts are
`/tmp/6488-probe-run.log`, `/tmp/6488-postgres-deadlock.log`,
`/tmp/6488-probe-list.log`, and `/tmp/6488-remote-probe.log`. These local artifacts
must be retained with the promotion evidence; their paths alone are not a
portable proof packet. This reproduction establishes a reachable ACK/fanout
cycle. It does not identify every participant in the earlier B-7 incident.

The implementation checkpoint is
`8be53b9cda17c551da430e757faa20301a846851`; its test-only parent is
`37e1fb548341bd337f14b591225e6c079f7007af`. Production edits are limited to
`go/internal/storage/postgres/reducer_queue_batch.go` and
`go/internal/storage/postgres/cross_scope_completion_fanout.go`. Query packages
and moving reducer families are outside this change.

## Ordering argument

Each generic, container-image, and CI/CD batch ACK selects its eligible work
rows with the existing status, owner, domain, and applicable claim-epoch
fences, then locks them in `work_item_id COLLATE "C"` order. A count dependency
drains the entire locking CTE before the target UPDATE. Target-level fences
remain in place; stale work cannot borrow another row's successful ACK.

Fanout retains the exact claimed-event lease check, then locks every eligible
current-generation consumer in the same order. It drains that consumer CTE
before locking the captured completion events. Scheduling depends on the
captured-event count, so event capture precedes consumer state updates. These
are data dependencies, not an assumption that textual CTE order controls
execution. The plan proof must show the sort feeding `LockRows` and the
aggregate dependencies consuming the full lock sets.

Work-row prelocks use `FOR NO KEY UPDATE`: these statements do not change the
referenced `work_item_id`, and the weaker lock still conflicts with competing
ACK/fanout mutations. It remains compatible with an audit foreign key's
`KEY SHARE`. The exact lease and captured event rows retain `FOR UPDATE` because
fanout deletes the captured events. Lock strength changes do not change the
shared work-item order or the full-consumption dependencies.

The participating resource order is:

1. Fanout's exact claimed event, which producer upserts cannot target.
2. Work rows in the shared bytewise identity order.
3. Queued completion events, followed by statement commit.

Producer ACK upserts only the pending/retrying event for its own domain. It
holds no queued event while acquiring further work rows. Fanout acquires no
new consumer row after acquiring queued events. Work-row wait edges therefore
follow one order, and queued-event edges cannot return to a later work-row
acquisition in these statements. Each ACK partition remains its own statement;
this change adds no global/domain lock, retry wrapper, worker cap, or batch cap.
The proof is scoped to these participating statements, not all database writers.

Ordering work rows alone would leave a possible producer/event inversion:
container-image fanout could hold a supply-chain row and wait for a CI/CD row;
CI/CD ACK could hold that row and wait for its queued event; CI/CD fanout could
hold that event and wait for the supply-chain row. This is a source-derived
schedule requiring the event-order fix, not a second observed deadlock report.

## Completion counts, replay, and stale state

An existing queued event updated by a producer before fanout obtains its lock
must contribute its current item count after PostgreSQL's concurrent-row
recheck. The consumers are locked before that capture and scheduled afterward.
A producer reaching the queued event after capture waits for fanout's deletion
to commit; its upsert must leave a fresh pending event. An event inserted after
the statement snapshot remains outside the captured set.

Already-dirty running consumers remain in the locking set even though they need
no additional UPDATE. Otherwise an ACK could consume the replay flag, a worker
could claim the resulting pending item, and fanout could consume a later
producer completion without preserving a replay obligation for that worker.
Holding the row through capture keeps the existing obligation durable. This
adds locks compared with the previous scheduling predicate; its cost must be
measured, including an already-dirty backlog.

The locking SELECT retains row-self eligibility predicates so a waited-on row
is checked again after a concurrent commit. Tests cover a changed generic/CI/CD
owner, a changed container-image claim epoch, and a fanout consumer becoming
pending before its lock is obtained. Existing trigger behavior still turns a
successful ACK carrying a replay flag into pending work. Event emission still
counts only acknowledged rows whose returned status is actually succeeded.

## Focused proof inventory

- `TEST: TestReducerContentionGateAckFanoutProbe` exercises 40 bounded production
  overlaps with final pending/running/replay/event counts. Passing all trials
  alone is not the lock-order proof.
- `TEST: TestReducerContentionGateAckFanoutLockOrderLive` holds the first lexical key,
  waits for the exact blocking backend, and checks that a later key remains
  lockable. It covers all three ACK variants, fanout, stale-state rechecks, and
  already-dirty consumers.
- `TEST: TestReducerContentionGateProducerAckCaptureHorizonLive` uses actual
  producer ACK calls on both sides of queued-event capture for both producer
  domains. It checks exact counts, retained events, and an already-dirty
  consumer ACK waiting on the fanout before returning to pending.
- `TEST: TestReducerContentionGateAckFanoutAuditFKCompatibilityLive` holds a real
  `fact_replay_events` insert transaction open and requires all four writer
  paths to complete while its validated work-item foreign key holds `KEY SHARE`.
- `TEST: TestCrossScopeCompletionEventAfterFanoutSnapshotRemainsPendingLive`
  covers an event inserted outside the captured statement snapshot.

All four issue regressions use the `TestReducerContentionGate` prefix selected
by the contention workflow. They accept its `ESHU_POSTGRES_DSN` environment
variable and bridge it into the existing isolated-schema helper's test-DSN
setting. The CI-parity invocation must list all four and execute them with
only the workflow's DSN variable supplied.

The contention probe captures `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)` for the
actual generic ACK and fanout statements in separate rolled-back transactions
before starting concurrent writers. `ESHU_ACK_FANOUT_PLAN_ONLY=1` omits the
stress phase. Run the same helper and fixture with baseline and candidate
production files for comparable measurements; record both plans and timings.

At the checkpoint, recursive `go test -list` compiled and listed the three
new/probe tests, and focused local tests exited 0 with live cases skipped.
Those results establish compilation and local compatibility, not live GREEN.
The expanded suite on test-only baseline
`37e1fb548341bd337f14b591225e6c079f7007af` exited 1: eight later-row NOWAIT
assertions failed, already-dirty fanout never reached the held first row, and
both producer-before-capture arms found the consumer unlocked. Both
producer-after-capture controls passed. The unforced contention probe again
reported SQLSTATE `40P01` at trial 6. The coordinator retained
`/tmp/6488-baseline-run.log` and `/tmp/6488-baseline-remote.log`; its recursive
listing selected all three tests and exited 0.

The candidate at `8be53b9cda17c551da430e757faa20301a846851` passed the same
recursive listing and live invocation, each with direct exit 0. All 40
contention trials, nine ordering/stale-state/dirty-consumer arms, and four
producer-capture arms passed. The coordinator retained
`/tmp/6488-candidate-remote.log` and `/tmp/6488-candidate-run.log`. The live
proof combines final queue state, exact blocking-backend observations, and
captured-event counts; the successful stress run alone is insufficient.

The plan comparison is retained in `/tmp/6488-plan-comparison.json`. Candidate
ACK locks all 64 rows through a bytewise sort feeding `LockRows`, with its
InitPlan draining the locking CTE. Candidate fanout locks 64 consumers before
capturing events through `InitPlan 3`, then gates scheduling on the captured
set through `InitPlan 5`. These measured dependencies support the ordering
argument rather than relying on textual CTE placement.

| Statement, 64 consumers | Baseline execution | Candidate execution | Baseline shared hits | Candidate shared hits |
| --- | --- | --- | --- | --- |
| Generic ACK | 1.693 ms | 2.138 ms | 1,155 | 1,225 |
| Completion fanout | 2.971 ms | 3.009 ms | 1,735 | 1,795 |

Each value is one `EXPLAIN ANALYZE` sample on the disposable fixture, with the
statement rolled back. The candidate adds sorting and locking work; these
samples show its measured cost at this fixture size. They do not establish a
speedup, a stable latency distribution, or a bound for large/already-dirty
backlogs. The deterministic dirty-consumer test proves the held replay
obligation; broader dirty-backlog performance still needs measurement.

A subsequent review found that the initial `FOR UPDATE` work prelocks were
stronger than the non-key mutations required. The audit-FK regression at
`cbaad84040c6f4eebd6e72d33d3959b91b88a1c5` exited 1 in all four writer arms,
each with SQLSTATE `55P03` after waiting for the open audit insert's `KEY SHARE`.
Artifacts are `/tmp/6488-fk-red-run.log` and `/tmp/6488-fk-red-remote.log`.
The other three contention tests passed (40 trials, nine ordering arms, four
capture arms) with only CI's `ESHU_POSTGRES_DSN` supplied and no skipped cases.
This establishes avoidable blocking, not a new production deadlock: the current
admin replay path updates work before inserting its audit events.

Only the three ACK work prelocks and fanout's consumer prelock changed to
`FOR NO KEY UPDATE`. Event locks remain unchanged. The earlier live GREEN and
single-sample plan costs apply to the initial stronger-lock candidate; the
corrected candidate `ad8a41a034020264487f5f5e8dc08d88aa26fcdf` passed the
CI-parity recursive listing and live invocation with direct exit 0. All four
tests executed without skips: 40 contention trials, nine ordering/stale-state
arms, four producer-capture arms, and four audit-FK compatibility arms. Logs
are `/tmp/6488-no-key-green-list.log`, `/tmp/6488-no-key-green-run.log`, and
`/tmp/6488-no-key-green-remote.log`. Paired scale validation remains outstanding.
The existing scale test now logs its complete metric vector before threshold
assertions, preserving measurements on failure without changing any limit.

Full-module build passed on the earlier candidate. Broader validation and final
review/preflight receipts remain outstanding; failures require baseline control
or a fix before promotion. Focused live GREEN does not establish scale readiness.

## Paired scale regression, not promotion evidence

Seven serial baseline/candidate pairs used the unchanged 900-scope,
25-generation, 67,500-work-row fixture, 57 batches, PostgreSQL 18.6, and the
same quiet 16-logical-CPU remote host. Each trial used a fresh isolated schema;
arm order reversed on even pairs. Both arms used the same 4 GiB disposable
PostgreSQL tmpfs and default buffer/WAL settings. Earlier 1 GiB fixture samples
are not substituted for this comparison. The baseline production SQL is
`37e1fb548341bd337f14b591225e6c079f7007af`; diagnostic commit
`f84496a062c5fc01fff6c48e279ac958d6b9d1f6` only moves complete metric logging
before unchanged threshold assertions. Candidate production SQL is
`ad8a41a034020264487f5f5e8dc08d88aa26fcdf`.

| Metric, median of seven trial values | Baseline | Ordered candidate |
| --- | --- | --- |
| Identity batch ACK p95 | 1.988 ms | 3.002 ms |
| CI/CD batch ACK p95 | 4.650 ms | 8.055 ms |
| Identity fanout | 129.047 ms | 130.569 ms |
| CI/CD fanout | 63.962 ms | 66.050 ms |
| Convergence wall | 1.037372481 s | 1.382575575 s |
| WAL bytes | 15,744,312 | 15,595,168 |

All fourteen gate invocations exited 1. Baseline CI/CD p95 ranged
4.581–4.810 ms; candidate ranged 7.982–8.126 ms, exceeding the unchanged 5 ms
bound in every trial. Both arms also exceeded the identity fanout 100 ms and
one-second convergence limits. Terminal truth assertions passed before metrics
were reported. The candidate adds a repeatable ACK regression; baseline gate
failures do not excuse it. No no-regression or merge-ready claim is supported.

Raw per-trial logs are `/tmp/6488-scale-{baseline,candidate}-{1..7}.log`;
the full runner status is `/tmp/6488-paired-scale-remote.log`, and the extracted
fourteen vectors and summaries are `/tmp/6488-paired-scale-summary.json`.
The opt-in `TEST: TestReducerAckFanoutScalePlanProbe` captures rolled-back
production SQL plans at the same fixture size for diagnosis. Instrumented plan
runs are distinct from these uninstrumented timings.

## Operator signals and limits

No-Observability-Change: `eshu_dp_queue_depth` and
`eshu_dp_queue_oldest_age_seconds` expose completion backlog using the bounded
`cross_scope_completion.<producer_domain>` queue name. The completion runner
logs successful domain, event count, producer item count, consumer count, and
`fanout_duration_ms` at debug level. Failure logs include the fenced producer
domain, event, claim epoch, attempt, and wrapped error before retry.

`postgres.InstrumentedDB` is wired with `StoreName: "reducer"` for these stores.
Its `postgres.exec` and `postgres.query` spans record errors returned by those
calls and error status. The query span ends when `QueryContext` returns; it
does not cover errors discovered later by row iteration or scanning. `eshu_dp_postgres_query_duration_seconds` records the existing
`store="reducer"` and `operation="write"`/`"read"` dimensions. These aggregate
storage timings are not a dedicated ACK/fanout lock-wait histogram; fanout uses
QueryContext and therefore retains the wrapper's `read` operation label despite
mutating rows. The runner's duration covers its complete Fanout call.

Batch handler success is recorded before the batched ACK. Consequently,
`eshu_dp_reducer_executions_total` success is not proof that a batch ACK
committed, and the single-item `ack_failed` counter is not emitted per item by
this batch path. Batch ACK errors propagate the batch context and PostgreSQL
error to the service failure path. A stalled batch is diagnosed through queue
age, storage spans/duration, the returned SQLSTATE, and server-side
`pg_blocking_pids`, `pg_stat_activity`, and deadlock detail. Confirm progress with
committed fanout counts and queue drain, not handler success alone.

No new telemetry source is introduced by this ordering change. The focused live tests observe storage completion, blocked backends, and durable
queue state. They do not exercise the hosted runner's exported telemetry;
runtime signal emission remains part of broader validation. A static coverage
check cannot substitute for that evidence.

PostgreSQL references used for the design:
[CTE evaluation](https://www.postgresql.org/docs/18/queries-with.html),
[ordered row locking](https://www.postgresql.org/docs/18/sql-select.html), and
[Read Committed concurrent-row rechecks](https://www.postgresql.org/docs/18/transaction-iso.html).
