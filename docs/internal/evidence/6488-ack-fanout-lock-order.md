# Reducer ACK and completion fanout lock ordering (#6488)

Batch ACK and cross-scope completion fanout can update the same reducer work
items across scopes. Their previous SQL left row acquisition order to the
executor. The production contention probe reproduced a PostgreSQL deadlock;
the change gives those statements a common work-item order and acquires queued
completion events only after the consumer locks.

The last live-validated production checkpoint is
`87860a397ce6e5b3c73c3da7a76b22cacf8c7993`. All seven focused live tests pass,
including the correction from exclusive work-row locks to `NO KEY UPDATE` for
foreign-key compatibility. The historical RED and intermediate performance
failures below explain the revisions. Seven paired scale trials produced six candidate passes and one ACK p95
failure; final build/vet, full golden-corpus proof and promotion review remain
pending.
Neither a single timing sample nor focused correctness establishes readiness.

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

The initial implementation checkpoint was
`8be53b9cda17c551da430e757faa20301a846851`; its test-only parent is
`37e1fb548341bd337f14b591225e6c079f7007af`. Production edits are limited to
`go/internal/storage/postgres/reducer_queue_batch.go` and
`go/internal/storage/postgres/cross_scope_completion_fanout.go`. Query packages
and moving reducer families are outside this change.

## Ordering argument

Each generic, container-image, and CI/CD batch ACK selects its eligible work
rows with the existing status, owner, domain, and applicable claim-epoch
fences, then locks them in `work_item_id COLLATE "C"` order. A count dependency
drains the entire locking CTE before the target UPDATE. That SELECT rechecks
eligibility after a concurrent update and holds the admitted rows through the
statement. The UPDATE targets their immutable work IDs; it does not repeat the
index-driving eligibility filters. Stale work cannot enter the locked set or
borrow another row's successful ACK.

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
`/tmp/6488-no-key-green-remote.log`. The subsequent paired scale result is recorded below.
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

## Measured target-selection correction

Before revising production SQL, checkpoint
`882d2d68eb3cb3ccb547de72a6998f258a2e988c` compared two rollback-only SQL
shims against the same 900-scope fixture. The immutable locked-ID shim retained
all eligibility predicates in the drained locking SELECT and removed only
redundant UPDATE-level filters. Its UPDATE plan used primary-key lookups:
60 shared hits for 15 rows instead of 914 hits in the broad CI/CD target scan.
The first CI/CD sample changed from 7.979 ms to 4.002 ms; the middle-batch sample
changed from 7.199 ms to 4.130 ms. These ordered single-sample diagnostics
identify the extra scan, not an uninstrumented p95 or speedup distribution.

The alternative physical-tuple shim was rejected. Three positive concurrent-row
recheck arms changed the row's physical version while keeping owner, status and
epoch eligible. All tuple-target arms silently acknowledged zero rows; all
shipped-ID controls and immutable locked-ID shims acknowledged exactly one and
preserved producer counts. The aggregate diagnostic exited 1 because those
three tuple arms failed, not because a fixture failed. Raw plans and results
are `/tmp/6488-target-shims-run.log` and `/tmp/6488-scale-plans-shims.json`.

Only the measured immutable-ID target change is adopted. Production contains
no physical tuple identifier. `TEST: TestReducerContentionGateAckEligibleEPQLive`
retains the three positive concurrent-update arms against the actual builders;
negative owner/epoch/status and lock-order tests remain required. At `6c1ab322c0234a03c046a57ad62bc7a694bd7da5`, recursive listing selected
all seven focused tests and the CI-parity live run exited 0 without skips:
40 overlaps, nine ordering/stale-state arms, four capture arms, four FK arms,
three positive concurrent-row rechecks, and both telemetry tests. Logs are
`/tmp/6488-locked-id-list.log` and `/tmp/6488-locked-id-run.log`.

A second seven-pair uninstrumented comparison still failed every scale gate.
Median CI/CD ACK p95 was 4.693 ms baseline versus 5.056 ms candidate; all seven
candidate trials exceeded 5 ms (range 5.036–5.167 ms). Median convergence was
1.051741525 s versus 1.092046414 s; identity fanout remained above 100 ms in
both arms (131.347 ms versus 133.667 ms medians). The repeated outer scan is
removed, but the remaining added lock cost still exceeds the contract. This
is unresolved, not a passing no-regression result. Artifacts are
`/tmp/6488-locked-id-pairs-remote.log`,
`/tmp/6488-locked-id-scale-{baseline,candidate}-{1..7}.log`, and
`/tmp/6488-locked-id-scale-summary.json`.

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

No new telemetry source is introduced by this ordering change. The live
`TEST: TestReducerContentionGateAckFanoutTelemetryLive` passed at diagnostic
checkpoint `882d2d68eb3cb3ccb547de72a6998f258a2e988c`: actual ACK and fanout
calls emitted reducer read/write duration histograms and spans, completion
gauges went from depth 1 to 0, and the real runner emitted its bounded success
log with committed counts. `TEST: TestReducerContentionGateAckFanoutTelemetryErrors`
also passed, observing both synchronous error spans, exception events, positive
durations and the runner's fenced error context for a controlled SQLSTATE.
The error is injected for telemetry coverage, not another deadlock reproduction.
These are SDK collection and structured-log proofs, not hosted exporter or
collector deployment validation. Raw output is `/tmp/6488-target-shims-run.log`.

PostgreSQL references used for the design:
[CTE evaluation](https://www.postgresql.org/docs/18/queries-with.html),
[ordered row locking](https://www.postgresql.org/docs/18/sql-select.html), and
[Read Committed concurrent-row rechecks](https://www.postgresql.org/docs/18/transaction-iso.html).

## Active-generation fanout plan, pending runtime revalidation

The opt-in lookup diagnostic at `af4b30134fc8420991702b0b71aa747b04270862`
ran 22 rollback plans against 900 scopes and 25 retained generations. Listing
and live execution exited 0. Full work-row digests (with clock fields normalized)
and event counters matched in every comparison. Both lateral ACK variants
repeated broad-index scans per requested ID and were rejected; neither changed
production ACK SQL.

The independently measured fanout variant materializes the 900 active
scope/generation pairs once. Consumer eligibility still joins the exact claimed
lease and locks work rows in C order before draining the completion horizon.
The plan retains the complete consumer-lock and event-capture barriers.
Identity fanout server execution was 148.735 versus 70.405 ms and CI/CD fanout
68.511 versus 42.305 ms in single rollback samples, with 1,800 and 900 matching
consumer rows respectively. These samples establish neither p95 nor scale-gate
success. Production checkpoint `87860a397ce6e5b3c73c3da7a76b22cacf8c7993` uses that
measured active-generation relation. All seven recursive contention-gate tests
were listed and passed with the CI-only DSN: 40 overlapping trials, nine ordered
lock/stale/dirty arms, four capture schedules, four audit-FK writers, three
positive ACK EPQ arms, and both telemetry tests. Listing and live exits were 0.
A separate bounded review found no correctness gap in the fanout delta and
confirmed unchanged statement-snapshot semantics; it is not a promotion verdict.
The full scale rerun below still leaves ACK p95 unresolved. No gate threshold
changed.

At the same checkpoint, the 435 tracked cassette, B-12 snapshot and ecosystem
fixture blobs were compared by path, mode and SHA-256 against main
`043143bde0a744ae73500c9a92165b5a2358f2b3`; all were byte-identical. This is an
input-integrity check, not a substitute for the still-pending full B-7 run.

## Seven-pair fanout candidate scale result

The unchanged production-shaped gate ran seven alternating-order pairs of
baseline `f84496a062c5fc01fff6c48e279ac958d6b9d1f6` and candidate `87860a397` on
the same quiet host and 4 GiB PostgreSQL fixture. All seven baselines failed the
100 ms identity-fanout bound. Six candidates passed; trial seven failed because
CI/CD ACK p95 was 5.002624 ms against the unchanged 5 ms bound. The complete
collection exited 1; this is not a GREEN gate result.

Baseline/candidate medians were: identity ACK 2.014665/2.300026 ms; CI/CD ACK
4.630461/4.894135 ms; identity fanout 128.962132/66.245694 ms; CI/CD fanout
63.663072/39.270248 ms; total wall 1.028121078/0.970186687 seconds; WAL
15,744,304/15,576,936 bytes. Candidate ACK p95 ranged from 4.886622 to
5.002624 ms. All candidate fanout, wall and WAL values met their bounds, and
all terminal row-state checks passed. The remaining ACK failure is retained;
neither a threshold change nor repeated runs until GREEN is an acceptable fix.

## CI/CD primary-key lock lookup, pending runtime validation

Before this production edit, diagnostic
`237e03d597271ca77c77be7c241b3f5ca1ee8707` ran 24 rollback plans and matching
row-effect comparisons with statistics refresh disabled. A separate control
had found 67,500 live rows with absent table statistics; refreshing statistics
changed CI/CD's access path, but those changed-fixture timings were never used
as gate evidence. Explicit collation and a row-specific ID alone did not solve
CI/CD's broad partial-index choice and were rejected.

The measured variant puts the fixed reducer stage and CI/CD domain in the
materialized requested-ID relation, checking those same values inside each
locking lookup. The actual plan sorts requested IDs in C order, then performs
15/16 dependent primary-key lookups with owner, status, stage and domain checks
below `LockRows`. The lock-count dependency still drains every lookup before
UPDATE. First/middle CI/CD rollback samples changed from 5.124/4.467 ms to
1.357/1.384 ms; these are neither p95 nor a production speedup claim.

Only the CI/CD ACK builder adopts this shape. Generic and identity ACK SQL,
arguments, workers, batch sizes, triggers and completion counts remain as
previously tested. No table statistics, indexes or planner settings changed.
Actual contention, stale-claim and positive EPQ proof plus the unchanged paired
scale gate must pass before this revision can be promoted.
