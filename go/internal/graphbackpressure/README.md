# graphbackpressure

## Purpose

`graphbackpressure` wires the graph write-path backpressure control into the
reducer and projector binaries. It is the root-cause fix for the recurring
NornicDB write/retract timeouts that fed the dead-letter backlog in issue #3560.

When the graph backend is slow, every reducer/projector worker can drive a
concurrent write that exceeds its deadline. Those timeouts dead-letter
recoverable work. `Wrap` bounds the number of in-flight graph writes so a slow
backend holds its permits longer, which blocks additional workers at the write
boundary and slows intake (closed-loop backpressure) instead of converting
transient slowness into a dead-letter flood.

This is deliberately not a serialization fix. The ceiling
(`ESHU_GRAPH_WRITE_MAX_IN_FLIGHT`) is configurable and greater than one, sized to
backend headroom, so useful write concurrency is preserved and only the surplus
that would overload the backend is gated.

## Wiring

`Wrap` is called at the outermost position of the write executor chain (above the
retry/timeout layer) so a single permit covers a whole write attempt, including
all retries and the deadline. The reducer wires it onto the semantic-entity
executor in `cmd/reducer/main.go`; the projector wires it onto the canonical
executor in `cmd/projector/runtime_wiring.go`.

A non-positive `ESHU_GRAPH_WRITE_MAX_IN_FLIGHT` returns the inner executor
unchanged, so the wrapper is a zero-overhead no-op until an operator opts in.

## Telemetry

`NewObserver` records two operator-facing metrics, labeled by operation:

- `eshu_dp_graph_write_backpressure_engaged_total` — writes that blocked for a
  permit. A non-zero rate is the 3 AM signal that backpressure is active.
- `eshu_dp_graph_write_backpressure_wait_seconds` — how long a blocked write
  waited. Rising p95 is the precursor to write timeouts.

## Evidence

Conflict domain: the graph write boundary shared by all reducer/projector
workers (canonical/semantic NornicDB writes). The bound is applied at the
outermost executor position, so one permit covers a whole write attempt
including retries and the deadline.

No-Regression Evidence: the bound is opt-in and defaults to disabled. With
`ESHU_GRAPH_WRITE_MAX_IN_FLIGHT` unset/non-positive, `Wrap` returns the inner
executor unchanged (identity), so the wrapper adds no allocation, no indirection,
and no behavior change on the hot path — proven by
`TestWrapDisabledIsPassthrough` (returns the same executor pointer) and the
reducer/projector wiring tests that still observe the unwrapped
`TimeoutExecutor`/`GroupExecutor` types when the knob is unset. When enabled, the
inner write is invoked exactly as before, just gated by a permit; the permit is
released on success and on error (`TestBackpressureExecutorReleasesPermitOnError`),
so a backend stuck timing out cannot leak permits or starve the path.

Performance Evidence (bounded-concurrency proof, not a corpus timing run):
`TestBackpressureExecutorBoundsConcurrentWrites` drives 24 racing callers at a
ceiling of 3 and asserts peak concurrent inner writes never exceeds the ceiling
and in-flight returns to 0 after drain; `TestWrapBoundsConcurrency` proves the
same through this package's `Wrap`. The grouped-write path shares the same permit
pool (`TestBackpressureExecutorGroupRespectsBound`) so `ExecuteGroup` cannot
bypass the ceiling. A blocked caller observes context cancellation rather than
hanging and does not consume a permit
(`TestBackpressureExecutorContextCancelWhileWaiting`). A repo-scale before/after
NornicDB timing run on the full corpus (dead-letter rate before vs. after with a
tuned ceiling) is the remaining operator-side validation tracked under #3560 and
must be captured on a host with the graph backend running; it is not reproducible
in unit CI.

Observability Evidence: backpressure engagement and wait time are exported as
`eshu_dp_graph_write_backpressure_engaged_total` (counter, labeled by operation)
and `eshu_dp_graph_write_backpressure_wait_seconds` (histogram, labeled by
operation), recorded together so their counts stay equal. An operator reads a
non-zero engaged rate as "backpressure is active" and rising wait p95 as the
precursor to write timeouts. The in-flight gauge is observable via
`BackpressureExecutor.InFlight()`.

## Why this package exists separately

The observer adapter must import both `internal/storage/cypher` and
`internal/telemetry`. It cannot live in `internal/runtime` because the cypher
package's internal tests import `internal/runtime`, which would create an import
cycle. Only the cmd layer consumes this package.
