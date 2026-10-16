# #8211 step (a): shutdown abandonment of stuck handlers — no-regression note

## Problem

`executeWithTelemetry` (per-item) and `executeAndReport` (batch) called
`Executor.Execute` synchronously with no timeout. A handler that ignores its
context and never returns holds its work item's lease via successful
heartbeats, so the item stays `claimed` and the worker never reaches `Fail`.

## Change

The worker runs the handler in a goroutine (`executeHandler` in
`go/internal/reducer/service_heartbeat.go`) and waits for either its return
or shutdown. When shutdown arrives first with no handler outcome available,
the worker stops the lease heartbeat BEFORE failing the intent through the
existing `WorkSink.Fail` triage. A handler outcome already available at
shutdown still wins over abandonment. There is deliberately no deadline
branch yet (worst-case durations unmeasured); shutdown is the only release.
Step (a) also records the non-enforcing handler-duration histogram and the
default/per-domain timeout config slot without enforcing them. No SQL, queue
schema, Cypher, worker count, batch size, or retry-policy default changed.
The #7816 runtime fence is untouched.

No-Regression Evidence (#8211 step a): baseline is the unpatched tree at
`origin/main` (commit `900cba4`, the pre-rebase base): the two stuck-handler regression tests
(`TestServiceRunAbandonsStuckHandlerAtShutdown` per-item,
`TestServiceRunBatchAbandonsStuckHandlerAtShutdown` batch) fail there —
`Run`/`runMainLoop` never return within the 10 s bound and `Fail` is never
called while heartbeats keep succeeding, proving the stuck-with-live-lease
shape. After the change, at this HEAD: all 8 new tests in
`go/internal/reducer/service_handler_abandon_test.go` pass (stuck variants
now release exactly once: `failCalls == 1`, `ackCalls == 0`, zero heartbeats
after `Fail` started; cooperative variants still fail exactly once with no
post-`Fail` beat); the ordering tails are pinned by
`TestFailAbandonedHandlerStopsHeartbeatBeforeFail` and its batch twin, both
verified to go red when the stop/Fail order is swapped and green when
restored; the full `go/internal/reducer` suite passes, including
`TestServiceBatchCanceledBeforeEnqueueHasNoSuccess` 20/20 (a completed
handler outcome at shutdown is recorded `ack_outcome_unknown`, never failed
into a duplicate retry); `-race` passes over the abandonment, heartbeat,
claim-rejection, and shutdown tests; `go/cmd/reducer` (config loaders,
wiring) and `go/internal/telemetry` (new instrument registration) suites
pass; `go vet` is clean on all three packages. Backend/version: Go
`1.26.9` per `go/go.mod`; no Postgres statement changed, so no live-DB
proof is owed — the `failIntent` retry-vs-dead-letter branch the new
retryable class flows through is covered by the existing fake-DB triage
tests (`TestReducerQueueFailRetriesRetryableErrorWithinAttemptBudget`,
`...ReadinessBacklogKeepsOwnFailureClass`,
`...GraphWriteTimeoutWithinAttemptBudget`), all green. Input shape: one
claimed intent per run, claim-once stub source, 2 ms heartbeat interval,
batch path with 2 workers. Terminal counts on the stuck runs: one `Fail`
carrying `failure_class=reducer_handler_abandoned_at_shutdown`
(`IsRetryable == true`, so the item retries under `ESHU_REDUCER_MAX_ATTEMPTS`
instead of dead-lettering), zero `Ack`s. Concurrency: one extra goroutine
plus one buffered (size 1) channel per execution; no shared state, no lock
ordering change, no worker-count or batch-size change; the abandoned
goroutine's late result is dropped unread and its late queue mutations are
rejected by the lease-owner/epoch fence (#7142). Safe because shutdown is
the only new trigger, completed outcomes win whenever observable, and every
changed dispatch variant (per-item sequential, per-item concurrent, batch)
has a dedicated passing test.

Observability Evidence (#8211 step a): `TestReducerHandlerAbandonmentMetricsEmitted`
collects through a `ManualReader` and asserts exactly one
`eshu_dp_reducer_handler_duration_seconds` point and one
`eshu_dp_reducer_handler_abandoned_total` point, each labeled with the intent
domain; `ESHU_TELEMETRY_COVERAGE_BASE=origin/main bash
scripts/verify-telemetry-coverage.sh` passes with the new coverage row naming
both metrics; each abandonment also emits the standard `recordReducerResult`
ERROR `reducer execution failed` log line carrying domain, intent id,
`failure_class=reducer_handler_abandoned_at_shutdown`, and elapsed handler
time. Residual risk (documented in code and in
`go/internal/reducer/queue-and-runners.md`): an abandoned handler that later
returns may still commit graph writes after `Fail`; only idempotent (MERGE)
handler writes are safe against the retry that follows.
