# Reducer Admission

## Purpose

`internal/reduceradmission` owns the reducer admission gate the ingester
and the standalone projector share ([#10882](https://github.com/eshu-hq/eshu/issues/10882)
decision 3): one writer wrapper that paces reducer intent enqueue behind
a global database-protection backstop and a per-source slowed leg.

## Ownership boundary

This package owns the admission decision (when to defer, when to admit,
when the bounded wait times out) and the per-source intent partitioning.
It does not own the queue depth signal (`QueueObserverStore`), the
slowed-state verdict (`workflow.DecideSourceSlowed`), the shared latch
(`WorkflowControlStore`), or the inner queue write. Deployment-profile
selection (for example the ingester local-lightweight bypass) stays
with the calling binary.

## Exported surface

See `doc.go` for the godoc contract.

- `AdmissionIntentWriter` wraps an inner `runtime.ReducerIntentWriter`
  with the gate. A fully disabled gate returns the inner writer
  unwrapped.
- The per-source backpressure env contract lives in
  `go/internal/collector` (`LoadSourceBackpressureConfig`); the global
  admission env contract lives here.

## Telemetry

Deferrals record `eshu_dp_reducer_admission_deferrals_total` with
`reason` (including `source_behind` plus a `source_system` label for the
per-source leg) and a structured warn log. Consult failures fail open
with a debug record; they are never silent and never throttle.

## Gotchas / invariants

- Deferral is never a drop: mixed-source batches partition by source so
  a healthy source lands while a slowed one waits.
- A high-water timeout after partial admission returns the retryable
  admission error with the admitted count applied; enqueue is idempotent
  on the reducer queue conflict key.
- `ProjectorAdmissionIntentWriter` is the standalone projector's
  constructor: the global marks default to 0 there (the projector never
  ran the global gate), so only an explicit env mark enables it and the
  per-source leg alone never does.
- Poll cadence: when every deferred group is `source_behind` the wait is
  max(PollInterval, the earliest verdict RetryAfter), capped by what
  remains of MaxWait; any global reason keeps PollInterval (`pollDelay`).
- A `source_behind` wait past MaxWait returns `ErrReducerSourcePaced`,
  not the admission timeout. The projector queue retries it without
  spending either attempt budget and never dead-letters it, so a source
  slowed for longer than MaxWait times the admission budget loses no
  work (`fact_work_items.source_paced_waits`).
- Consult the two S0 probes before touching the signal path:
  `TestSourceBackpressureS0SignalCost` (cost) and
  `TestSourceBackpressureProbeMatchesGroupedAndVerdicts` (equivalence).
