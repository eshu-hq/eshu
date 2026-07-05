# Evidence: wire the ingester canonical writer into the graph-write gate (#4729 / #4456)

## Problem

After #4728 (#4456) shipped the default graph-write in-flight budget, only
bootstrap-index and the reducer honored it. The ingester's canonical writer
(`openIngesterCanonicalWriter`) built its executor via
`canonicalExecutorForGraphBackend` without wrapping it in the
`graphbackpressure` gate — `rg graphbackpressure go/cmd/ingester` returned
nothing — so `ESHU_GRAPH_WRITE_MAX_IN_FLIGHT` was inert for the ingester,
leaving it an unbounded concurrent graph writer that could still push NornicDB
past the throughput knee (see #4728 sweep: collapse near 12-16 concurrent
writers) during continuous incremental re-projection.

## Fix

`newIngesterCanonicalGate` builds one shared gate from
`ClassMaxInFlight(getenv, CanonicalMaxInFlightEnv)` and threads it INTO
`canonicalExecutorForGraphBackend`, which wraps the **inner GroupExecutor layer**
(the `TimeoutExecutor` over the instrumented/retrying raw executor) with
`graphbackpressure.WrapExecutorWithGate` before building the outer
`nornicDBPhaseGroupExecutor` — exactly mirroring
`bootstrapCanonicalExecutorForGraphBackend`.

This layer matters. The outer `nornicDBPhaseGroupExecutor` fans one
`ExecutePhaseGroup` into up to `ESHU_NORNICDB_ENTITY_PHASE_CONCURRENCY` (cap 16)
concurrent inner `ExecuteGroup` goroutines. Gating must be on the inner layer for
two reasons: (1) gating only the outer call would bound calls, not the concurrent
backend writes the fan-out issues, so `=8` would still admit up to 16 in-flight
writes; and (2) the outer executor is a `PhaseGroupExecutor`, not a
`GroupExecutor`, so wrapping it with `WrapExecutorWithGate` would demote it to an
Execute-only wrapper, stripping the phase-group capability and silently
regressing the canonical writer to per-statement sequential writes. Gating the
inner layer bounds the actual concurrent writes AND preserves the phase-group
path. A nil gate (unset ceiling) leaves the inner executor unchanged.

The full-refresh DETACH DELETE **drain path** is gated too. Drain-marked
retracts route through `nornicDBPhaseGroupExecutor.drainReader.RunWrite` on the
raw executor, bypassing the inner `bounded` layer; `gatedDrainReader` wraps that
reader so each drain `RunWrite` acquires a permit from the same canonical gate.
Without it, with multiple projector workers and a ceiling below the worker count,
concurrent drains could still exceed the in-flight limit (bootstrap-index has no
drainReader, so this is ingester-specific). A nil gate leaves the raw reader
unwrapped (passthrough).

The writer is opened once at process startup and shared, so a single gate bounds
the whole process (fan-out ExecuteGroup + drain writes). The shipped Compose
default now sets `ESHU_GRAPH_WRITE_MAX_IN_FLIGHT=8` on the ingester service too.

Performance Evidence: this reuses the exact backpressure mechanism proven in
#4728, whose measured NornicDB concurrent-writer sweep showed write throughput
peaks near 12-16 writers then collapses; bounding to the knee gives peak
throughput with zero 30s timeouts. #4728's 909-repo E2E cut canonical failures
100→13 and dead-letters 55→3 with bootstrap+reducer gated; this change extends
the same bound to the third writer process so a continuously-running ingester
cannot re-introduce the unbounded-concurrency collapse on the same backend.

No-Regression Evidence: a nil gate (non-positive ceiling) leaves the inner
executor unchanged, so a deployment that has not set
`ESHU_GRAPH_WRITE_MAX_IN_FLIGHT` (or the canonical-class override) sees zero
behavior change. Unit tests run the REAL production path
(`canonicalExecutorForGraphBackend` with the NornicDB backend):
`TestCanonicalExecutorGatesInnerLayerAndPreservesPhaseGroup` (gated → outer stays
`nornicDBPhaseGroupExecutor` AND its `.inner` is `*BackpressureExecutor` that
still satisfies `GroupExecutor`), `TestCanonicalExecutorPassthroughWhenUnset`
(unset → inner is the ungated `TimeoutExecutor`),
`TestCanonicalExecutorGatesViaCanonicalClassEnv` (per-class override → inner
gated). These fail against the earlier outer-wrap revision (the executor would
not be a `nornicDBPhaseGroupExecutor`), catching the phase-group-strip regression.

No-Observability-Change: no new instruments; the wrap reuses the existing
`graphbackpressure` canonical-gate telemetry (wait samples under the `canonical`
gate name, #4448), which now also reports the ingester's permit waits when the
budget is set. Existing spans/metrics/logs are unchanged.
