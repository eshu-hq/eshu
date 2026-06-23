# Ingester Composite Runner Failure Isolation (issue #3501)

This ADR supersedes the prior `cmd/ingester` "compositeRunner first-error
cancel" invariant. It records the decided design for removing unbounded
fail-fast teardown between the collector and projector ingester services and
replacing it with retry-aware isolation plus a bounded graceful drain.

Status: accepted. Scope: `cmd/ingester/composite_runner.go`,
`internal/collector/service.go`, and the `internal/collector` retry classifier.

## Problem

The collector and projector run concurrently under one `compositeRunner`. The
original runner returned the *first* result it received and discarded the rest:

```go
err := <-errc
cancel()
for i := 1; i < len(c.runners); i++ {
    <-errc
}
return err
```

This had two defects, both flagged by issue #3501:

1. **Fatal-error masking.** `err` was whatever arrived first. When a healthy
   sibling returned `nil` on cancellation before the failing sibling's error
   landed, `Run` returned `nil` and the real failure was dropped. The drain loop
   `<-errc` discarded every other terminal error unconditionally, so a
   simultaneous collector and projector failure surfaced at most one of them.
2. **Fail-fast teardown of a healthy sibling.** Any collector commit error —
   including a *transient* one already destined for durable dead-letter replay —
   tore down the collector, which canceled the shared context and killed the
   projector running alongside it. One transient fault in one generation stopped
   all ingestion.

## Decided design

A layered fix. Each layer is independently testable.

### L1 — collector honors the retry convention

`internal/collector` gains a `RetryableError`/`IsRetryable` classifier mirroring
the existing `projector.IsRetryable` (issue #3513) and `reducer.IsRetryable`
conventions, and also treating `RegistryFailure` with the
`registry_retryable_failure` class as retryable. On a commit failure the
collector `Run` loop now:

- quarantines the generation for durable replay via the dead-letter sink, and
- if the commit error is retryable **and** the dead-letter write succeeded,
  logs a retryable record and continues polling instead of returning.

A non-retryable commit error, or a dead-letter *store* failure (fatal
infrastructure breakage), still tears the service down. The transaction scope
(one generation commit) is separated from the retry scope (durable queue
replay): retryable per-unit faults never escape `Run`, so they never reach the
composite runner as fatal.

The projector already honored this convention before this change: a transient
projection or fact-load failure calls `WorkSink.Fail` (durable queue retry,
where `IsRetryable` is applied at the queue/triage level) and returns `nil`.
Only genuine infrastructure failures (claim error, ack failure) escape its
`Run`. No projector change was required for L1.

### L2 — composite runner: join all results, bounded graceful drain

`compositeRunner.Run` now:

- Tags each runner result with its index and delivers it on a channel buffered
  to `len(runners)`, so every goroutine can always send even after `Run` stops
  receiving. The send side never blocks and never leaks.
- Aggregates **every** terminal error with `errors.Join`. No sibling error is
  masked or dropped. A clean context-driven shutdown where all runners return
  `nil` yields `nil`.
- On the first fatal error, cancels the shared context so siblings stop claiming
  new work and finish their in-flight unit, then waits up to a bounded
  `drainGrace` (default 30s) for siblings to return.
- If a sibling ignores cancellation, abandons the wait at `drainGrace`, joins
  `errCompositeDrainTimeout` onto the result, and returns. Teardown stays
  bounded; process exit reaps the abandoned goroutine. The buffered channel
  keeps the abandoned runner's eventual send non-blocking.

### L3 — this ADR plus lockstep doc updates

This ADR and the updated `cmd/ingester/AGENTS.md` and `cmd/ingester/README.md`
replace the old "first-error cancel is correct; do not change it" guidance with
the new contract: failure isolation between siblings, retry-aware continuation,
errors joined not masked, and a bounded drain.

## Why isolation does not hide failures

The old invariant existed to ensure neither service's failure was silenced. The
new design strengthens that guarantee rather than weakening it:

- A *fatal* error from either sibling is always surfaced (joined), and still
  cancels the other sibling so the ingester exits non-zero and restarts.
- A *transient* error is no longer fatal at all; it is durably quarantined and
  replayed, with an operator-facing retryable log. It is therefore correct for
  it not to tear down the peer.
- The drain is bounded, so a wedged sibling cannot convert isolation into a
  hang; `errCompositeDrainTimeout` makes a forced teardown distinguishable from
  a clean one.

## Concurrency analysis

- **Conflict domain:** the shared cancel context and the result channel. No
  shared mutable state is written by the runner goroutines; each sends exactly
  one `compositeResult`. Aggregation happens only on the `Run` goroutine.
- **Channel sizing:** buffered to `len(runners)` guarantees non-blocking sends
  even after a bounded-drain abandon, so there is no send-side goroutine leak.
- **Lock ordering:** none — there are no locks in the runner; coordination is
  via context cancellation and a single-reader channel.
- **Deadlock/leak:** `TestCompositeRunnerNoGoroutineLeak` (run with `-race`)
  proves no lingering goroutines after clean shutdown.
  `TestCompositeRunnerBoundsDrainOnWedgedSibling` proves `Run` returns within
  the grace window when a sibling never observes cancellation.

## Telemetry

Operator-facing signals added by this change (3 AM debuggability):

- Collector retryable commit: `WARN` "collector commit retryable; quarantined
  for replay, continuing" with `failure_class=commit_retryable`,
  `retryable=true`, scope/generation attributes, and `phase=emission`.
- Composite fatal: `ERROR` "composite runner sibling failed; draining peers"
  with `runner_index`, `remaining_runners`,
  `failure_class=composite_runner_fatal`, `retryable=false`, and `drain_grace`.
- Composite drain timeout: `ERROR` "composite runner sibling drain exceeded
  grace window" with `remaining_runners`,
  `failure_class=composite_runner_drain_timeout`, and `drain_grace`; the
  returned error carries `errCompositeDrainTimeout`.

## Evidence

No-Regression Evidence: `go build ./...` (exit 0);
`go test ./cmd/ingester/... ./internal/runtime/... -count=1 -race` (369 passed);
`go test ./internal/collector/ -count=1 -race` (379 passed). Focused regression
tests added first (TDD): `composite_runner_test.go` proves fatal errors are not
masked, all terminal errors are joined, the surviving sibling drains within a
bounded grace, a wedged sibling is bounded with `errCompositeDrainTimeout`,
clean shutdown returns `nil`, and there is no goroutine leak under `-race`.
`service_retryable_test.go` proves a retryable commit failure is dead-lettered
and the loop continues (no teardown), a non-retryable commit error still
propagates, and the `IsRetryable` classifier covers explicit-retryable,
registry-retryable, registry-terminal, plain, nil, and wrapped variants.

This change touches no Cypher, graph write shape, worker count, batch size,
lease, or queue ordering. It removes a serialized fail-fast teardown rather than
adding one; there is no throughput regression to measure, and concurrency is
preserved (the projector keeps running through a transient collector fault).

Observability Evidence: see the Telemetry section above. The three operator
signals (retryable commit, composite fatal, composite drain timeout) let an
operator distinguish a retried generation, a fatal teardown, and a bounded
forced drain without code inspection, using existing structured-log fields.
