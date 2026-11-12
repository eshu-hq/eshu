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
