# Reducer admission bound evidence (#10485)

No-Regression Evidence (admission bound, #10485): the bound changes only what
happens after a wait that previously never ended; it adds no new goroutine,
lock, worker, batch, SQL, or queue-schema change. Baseline (the stall recorded
in #10480): with reducer depth held above the marks, 8 projector workers sat at
`intent_enqueue` past the first graph writes with heartbeats renewing and zero
completions — the claim parked forever and the queue never turned over. After:
one Enqueue waits at most `ESHU_REDUCER_ADMISSION_MAX_WAIT` (default 5m,
mirroring the 5m projector claim lease), then returns
`ErrReducerAdmissionTimeout`, which `failure.IsRetryable` accepts through the
production `enqueue reducer intents: %w` wrap, so `ProjectorQueue.Fail`
releases the claim (attempt++) instead of parking. Below the marks the path
adds one `time.Now` plus a comparison per Enqueue; the admitted write path is
byte-identical. Input shape: reducer depth at or above the marks sustained
past the bound. Terminal queue outcome: items retry within the attempt budget
(default 3 attempts); a backlog sustained past the budget dead-letters instead
of parking — that is the stated #10485 design, not a regression. Verified by
`go test ./cmd/ingester -run 'TestReducerAdmission|TestLoadReducerAdmissionConfig|TestReducerIntentWriterWithAdmission' -count=1`
(including the wrapped-error `IsRetryable` pin on the production wrap chain)
and the same run with `-race`.

Observability Evidence (admission bound, #10485): expiry emits a `Warn` log
naming `reason`, `queue_depth`, `waited`, `max_wait`, and `intent_count`, and
the released claim carries the existing `projection_retryable` class in the
queue `failure_message` — the same signal every other retryable projection
failure already leaves. The existing per-poll deferral counter
(`eshu_dp_reducer_admission_deferrals_total`) keeps counting the wait; no
counter or span is added or removed. An operator tells bound-expiry apart from
an ordinary deferral by the timeout `Warn` line plus the attempt++ on the
item.

## Separate admission-wait budget (#10671)

No-Regression Evidence (admission budget, #10671): the split changes only
which budget a retryable admission-timeout failure spends. The admitted write
path, the claim fence (`attempt_count` still increments on every claim under
the shared `ingester` lease owner), the exponential-backoff formula, and the
dead-letter classes are untouched. Baseline (QA 2026-11-20): with reducer
depth above 10,000 for 7+ hours, chunk-2 and chunk-3 dead-lettered after
exactly 3x5-minute admission waits on the shared budget of 3 — data loss
while the queue stayed full. After: the same waits retry against a dedicated
48-wait budget (derivation: 48 x 5m waits = 4h, plus `ComputeRetryDelay`
backoff of 30s doubling to the 1h cap after ~7 doublings, so ~41 further
waits add ~41h — roughly 44-46h wall-clock to exhaust, more than 6x the
observed 7h episode), while genuine projection failures keep the full shared
budget measured as `attempt_count - admission_wait_attempts`. Terminal queue
outcomes are unchanged for non-admission failures (shared budget) and for
admission waits past 48 (dead-letter with class `admission_timeout`); a
permanently stalled queue still dead-letters inside ~2 days per #10485 —
bounded, not infinite parking. Cost per wait-fail-reclaim cycle: the cycle
re-runs LoadFacts, Project (canonical/content upsert writers — a re-run
writes the same rows idempotently), and the 5-minute Enqueue wait; nothing
in the cycle scales with the budget, and each cycle is paced by the
admission wait plus the capped backoff, so 48 cycles cannot hot-loop.
Input shape: reducer depth sustained at or above the marks. The change adds
one same-row counter increment (`admission_wait_attempts = ... + 1`; the
column carries no index, the row is already locked by the fence) to the
already-fenced retry UPDATE and one column to the claim SELECT's RETURNING
and final SELECT; no predicate, join, ordering, or lock changes, so the
claim plan is unchanged. Verified by hermetic Fail budget tests
(retry/refund/boundary-1/exhaustion, `go test
./internal/storage/postgres/ -run 'TestProjectorQueueFail|TestProjectorQueueAdmission'`),
the emission-capture test, live Postgres proof on a scratch PG 18.7
cluster (claim populates both counters, admission Fail increments the
counter exactly once including under a concurrent double-Fail race with
the loser claim-rejected, stale-owner Fail rejected without touching the
row, refunded budget exhausts to a `retry_exhausted` dead-letter,
migration 325 applies cleanly with the NOT NULL DEFAULT 0 counter and a
no-op re-apply), plus green `go test` and `-race` on
`internal/storage/postgres`, `internal/projector`, `cmd/ingester`,
`internal/telemetry`, and `internal/envregistry` (only pre-existing
environmental failures remain: Linux-only `/proc/loadavg` live probes
and a missing pinned deploy checkout, both failing identically on the
untouched base), `go vet` clean, `git diff --check` clean, and the
telemetry-coverage verifier green.

Observability Evidence (admission budget, #10671): admission-wait retries
emit the new unlabeled `eshu_dp_projector_admission_wait_retries_total`
counter (proven by the emission-capture test); projection retries keep
`eshu_dp_projector_retry_surge_total` labeled
`failure_class="projection_retryable"`; dead letters keep class
`admission_timeout` on `eshu_dp_queue_dead_letters_total`. Per-item
budget position is the `fact_work_items.admission_wait_attempts` row
itself, queryable directly — the counter carries no per-item labels by
design. Coverage row added to
`docs/public/observability/telemetry-coverage-projector-parser.md`;
`scripts/verify-telemetry-coverage.sh` passes.

## Shared queue-depth read (#11233)

Performance Evidence (shared depth read, #11233): the gate used to run
`queueDepthQuery` on every admission decision, and each deferred Enqueue
re-ran it every poll interval, so calls = enqueuers x poll rate. Baseline on
QA (`.claude/shim-11233-11234-2026-11-22.md`, query id -1132648068480399287):
910 calls in 317.4 s (2.87 per second, 696.9 ms mean, 171,076 buffer hits per
call), 4.50 per second in the attribution window, and 1,525 deferrals in 10
minutes (2.54 per second, all `reason=high_water`, `intent_count=1`, depth
10,000 to 10,209 against the 10,000 mark). Conflict domain: the process-wide
holder in `reducer_admission_depth.go` (one per ingester; the projector
workers, `ESHU_PROJECTOR_WORKERS`, share it). Settings are unchanged: mark
10,000, poll interval 1 s (the max age A), max wait 5 m; the backoff cap is
10 s (or half the max wait) and is not a setting. Hermetic before and after
on the virtual clock (`reducer_admission_depth_test.go`, reads are instant):

| Scenario | Before (per-decision read) | After |
| --- | --- | --- |
| 8 enqueuers deferring at the mark for 60 s | 488 reads | at most 9 (one per backoff step) |
| 1, 8 or 32 enqueuers deferring for 5 m | 301 x K reads | 30 reads for any K (0.1 per second) |
| 16 admitted enqueues within one max age | 16 reads | 1 read |
| 100 admitted enqueues per second for 30 s | 3,000 reads (one per decision) | 28 reads |
| one waiter, max wait 20 s at the mark | 21 reads | 5 reads, timeout at exactly 20 s |

Restated contract: queue depth keeps its definition and the statement
constant is unchanged. The gate defers when the depth it holds is at or above
the mark. An admit always uses a depth at most A old, because the window is A
whenever the last decision admitted; a defer may use a depth up to the
current backoff old, which only delays a producer. Overshoot above the mark is
bounded by the intents that arrive within A (before: by the concurrent
enqueuers). After depth falls below the mark a waiter is admitted within the
backoff cap plus one read. A sleep never runs past the time left before the
max wait. A failed read reaches every waiter and is never held; the next
decision reads again. The read runs detached from the starter's cancellation;
the last waiter out cancels it. The QA bar is a post-deploy reading, not a
claim here: that query id's calls fall from 2.9 to 4.5 per second to at most
0.2 per second, and its share of statement time is under 1 percent. No saving
of primary CPU is claimed before that reading.

Observability Evidence (shared depth read, #11233):
`eshu_dp_reducer_admission_depth_reads_total{operation,outcome}` counts one
lookup per signal per decision; `outcome=read` plus `outcome=error` is the SQL
rate, and `held` and `shared` are decisions served without a new statement.
`eshu_dp_reducer_admission_depth_age_seconds` shows how old the depth was at
each decision, and `eshu_dp_reducer_admission_read_backoff_seconds` the
current read interval (0 while admitting; with the defaults it pins at 10
when the queue sits at the mark). `eshu_dp_reducer_admission_deferrals_total` and its `Warn` log
still count each deferral, now at most one per waiter per backoff step, so
the deferral rate falls with the read rate. A read error still returns to the
caller through the existing `read reducer admission queue depth` wrap.
