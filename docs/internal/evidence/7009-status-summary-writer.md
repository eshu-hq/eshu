# Status summary read model: periodic writer (#7009, PR-B)

## Scope

This note covers the second slice of the #7009 status read model: the
reducer-owned periodic writer in `go/internal/reducer/status/summary`, its
wiring in `go/cmd/reducer/status_summary_wiring.go`, and two exports in
`go/internal/storage/postgres/status.go`:

- `ReadActiveWorkSummaryEntries` runs the live active-work statement byte for
  byte and validates every row with the live decoder before returning it;
- `ActiveWorkSummarySourceSHA256` is the digest of that statement text, the
  rolling-upgrade fence the reader compares (PR-C).

The writer is off by default (`ESHU_STATUS_SUMMARY_WRITER_ENABLED=false`): no
goroutine starts and no writer SQL runs. Nothing reads the row yet; the reader
switch is PR-C. The table and store are PR-A
(`7009-status-summary-read-model.md`).

## Design as built

One pass is one READ COMMITTED transaction on the primary, pinned with
`SET TRANSACTION ISOLATION LEVEL READ COMMITTED` whatever the cluster default
(REPEATABLE READ would raise 40001 on the upsert), then `SET LOCAL jit = off`,
`pg_try_advisory_xact_lock(WriterLockKey)` (skip when held), one round trip for
`clock_timestamp()` and `to_regclass('status_summary_snapshots')`, the
statement with that clock as `$1`, and one guarded single-row upsert. The pass
has a deadline of two intervals. Passes never overlap or queue: the next pass
starts on the first interval boundary after the previous one ends.

The interval default is 10 s, minimum 5 s. The QA read-replica probe
(2026-10-06) measured the active-work statement at a 1,020 ms median
(1,013-1,028 ms, 160,522 shared hits). Labels for that figure: 1 warm-up plus 3
timed runs, median of 3; `EXPLAIN (ANALYZE, BUFFERS)` timing with warm buffers;
`jit = off`; time on the read replica (a hot standby), not on the primary where
the writer runs; not a p95 and not a deployed route latency. The ruling's P4
mapping puts 1-2.5 s at a 10 s interval. A 2 s cadence raised claim p95 on the fixture
shim, so values below 5 s fail reducer startup.

## Proof

Hermetic, `go test -race -count=1 ./internal/reducer/status/summary/`:
statement order in one transaction, lock skip without the statement, missing
table skipped with one warning per process (both the pre-check and a 42P01 on
the upsert), guard rejection committed and counted, every failure step rolled
back with the SQLSTATE logged, the two-interval deadline, pacing on interval
boundaries with overrun counting, the 10 s default, shutdown mid-pass, the
interval floor, the metrics, and the enrollment guard with its seeded RED
cases. Six mutants of `runner.go` were each killed by a named test: lock skip
removed, pacing replaced by a fixed interval, table check removed, host clock
for `as_of`, guard outcome dropped, deadline widened.

Storage, `go test ./internal/storage/postgres/ -run
'TestReadActiveWorkSummaryEntries|TestActiveWorkSummarySourceSHA256'`: the
exact statement and `$1` in UTC, rows kept in order with their JSON text
untouched, undecodable rows rejected (bad JSON, unknown section, bad count),
an empty result as an empty slice, and the digest bound to the statement text.

Wiring, `go test ./cmd/reducer/ -run StatusSummary`: off by default (no
runner for unset, false, or unparsable switch), built with the storage
statement and digest when enabled, 10 s default, 2 s / 4999 ms / 0 / negative /
unparsable intervals rejected, a database without transactions refused, and
`withStatusSummaryWriter` (called by `buildObservedReducerService`) sets only
the writer field, nil by default. `go test ./internal/reducer/
-run StatusSummary` proves `Service` starts the writer as a side runner and an
empty `Service` starts nothing.

Live on PostgreSQL 18.6 (Homebrew, native, loopback), every test creating its
own database and applying all 180 migrations, `go test -race -count=1 -v
./internal/reducer/status/summary/` with the proof DSN, the disposable opt-in,
and `ESHU_REQUIRE_STATUS_SUMMARY_WRITER_PROOF=1`, run at f0fb295f3 (this branch
on e5dfb548a): 39 PASS lines (27 top-level tests and 12 subtests), no FAIL and
no SKIP, rc=0.

- the stored row equals the live statement at its `as_of`, read in one
  `REPEATABLE READ` snapshot, at 1/600, 300/600 and 600/600 live rows (every
  section present at 50 and 100 percent) and with every input deleted (the
  `mode` and `queue` entries only); a changed work item makes the same comparison fail;
- a backend terminated between the statement and the upsert leaves the stored
  `as_of` unchanged and the row still equal to live; the next pass replaces it;
- a stored row one hour newer is kept and the pass reports `rejected_guard`;
- with the table dropped, passes report `skipped_missing_table`; after
  migration 161 is reapplied the next pass writes an equal row;
- a row written under another statement digest is replaced by the next
  production pass;
- with the database default set to REPEATABLE READ, the pass still runs
  READ COMMITTED (`SHOW transaction_isolation` inside the pass);
- a second writer that ticks while the first holds the lock reports
  `skipped_lock` without running the statement;
- two writers at the 5 s minimum for 30 s beside two workers that claim and Ack
  through the production `ReducerQueue` (2,000 enqueued `workload_identity`
  intents, 80 percent live, re-opened to 100 percent at 12 s): zero claim
  errors, zero writer-attributable lock waits over about 290 samples, a
  monotone stored `as_of`, no overlapping computes, only `ok` and
  `skipped_lock` outcomes, and overruns only for passes over the interval. A
  seeded writer-held lock wait first proves the sampler can count one (its
  first version could not: the waiter ran `LOCK TABLE` outside a transaction).

With `ESHU_REQUIRE_STATUS_SUMMARY_WRITER_PROOF=1` and no DSN, the live proofs
fail (rc=1) instead of skipping. Replacing the READ COMMITTED pin with a no-op
makes `TestWriterPassRunsReadCommittedLive` fail with `pass transaction
isolation = "repeatable read"`.

Performance Evidence: no before figure exists, because the writer is new and
off by default; enabling it adds one active-work pass per interval on the
primary. Its cost is the statement's: 300-440 ms per pass on the 2-CPU fixture
shim and a 1,020 ms median on the QA read replica (P4 probe above), at the
10 s default about 6 passes a minute. The live contention run is correctness
evidence only: it ran at load1 23-33 on an 18-CPU shared laptop, so its claim
rates and latencies (logged per run) are not a measurement. The deployed
claim-latency comparison with the writer off and on is PR-F (ruling D3.1).

Observability Evidence: `eshu_dp_status_summary_writer_passes_total` and
`eshu_dp_status_summary_writer_pass_duration_seconds` (`model_key`, `outcome` =
ok, skipped_lock, skipped_missing_table, rejected_guard, error),
`eshu_dp_status_summary_writer_overrun_total{model_key}`,
`eshu_dp_status_summary_writer_up{model_key}`, the `reducer.status_summary.pass`
span, and logs for start, overrun, guard rejection, missing table (once), and
failure with `sqlstate`. The hermetic tests assert the counter, histogram,
overrun, and up-gauge values.

## Enrollment

The live proofs run in the blocking `reducer-contention-gate` workflow (its own
step, `ESHU_REQUIRE_STATUS_SUMMARY_WRITER_PROOF=1`, filter guarded by
`TestWriterLiveProofsRunInTheReducerContentionGate`) and, because the
live-test ledger requires every `postgres_ci` row to name that runner, in the
advisory `live-postgres-readiness` job.

## Reducer contention gate budget

The summary proofs add steps to the `reducer-contention-gate.yml` job. The job
timeout was 25 minutes (1,500 s); an arbiter rule says to split the job or raise
the timeout when the worst sample plus the new step goes above 20 minutes, so the
timeout is now 30 minutes. Step times from `gh run view` (seconds; only steps of
5 s or more listed; the three main runs are push events on main and predate PR-A,
the two PR-A runs carry its store proofs step):

| run | event | job total | gate step | story proof | store proofs |
| --- | --- | --- | --- | --- | --- |
| 37541975907 | main push | 950 | 866 | 39 | none |
| 37523581703 | main push | 755 | 681 | 31 | none |
| 37509101393 | main push | 1147 | 1024 | 45 | none |
| 37545390693 | merge group (PR-A) | 1108 | 1000 | 44 | 13 |
| 37539664298 | pull request (PR-A) | 1082 | 989 | 44 | 13 |

Setup steps (containers, checkout, Go) add 30-80 s per run and are inside the
job totals. The writer proofs step has no CI timing yet. Estimate: the package
took 47 s locally with a warm build cache and a local PostgreSQL (the 30 s
contention proof plus about a dozen database setups of 180 migrations each), so
60-90 s on a cold runner. Worst case: 1,147 s (worst main run) + 13 s + 90 s,
about 1,250 s or 20.8 minutes. The PR-B CI run's real writer-step time replaces
the estimate.

## NOT_CHECKED

- Writer pass cost on the QA primary (the probe ran on the read replica).
- Claim latency with the writer on, on a quiet host or deployed (PR-F).
- The reader side: model selection, age correction, and fallback (PR-C).
- Design change from the ruling (D4): the reducer-side
  `eshu_dp_status_summary_age_seconds` gauge is not in this slice. Writer health
  is covered by the pass counter and duration histogram by outcome, the overrun
  counter, the `writer_up` gauge, and the Warn and Error logs. PR-C and PR-F
  must land an age signal before the `EshuStatusSummaryStale` alert that uses it.
- Behaviour beside #7647's gated statement: the writer runs whatever statement
  the binary carries, and the digest changes with it.
