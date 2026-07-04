# graphbackpressure

## Purpose

`graphbackpressure` wires the graph write-path backpressure control into the
reducer, projector, and bootstrap-index binaries. It is the root-cause fix for
the recurring NornicDB write/retract timeouts that fed the dead-letter backlog
in issue #3560.

When the graph backend is slow, every reducer/projector/bootstrap-index worker
can drive a concurrent write that exceeds its deadline. Those timeouts
dead-letter recoverable work. `Wrap` bounds the number of in-flight graph
writes so a slow backend holds its permits longer, which blocks additional
workers at the write boundary and slows intake (closed-loop backpressure)
instead of converting transient slowness into a dead-letter flood.

This is deliberately not a serialization fix. The ceiling
(`ESHU_GRAPH_WRITE_MAX_IN_FLIGHT`) is configurable and greater than one, sized to
backend headroom, so useful write concurrency is preserved and only the surplus
that would overload the backend is gated.

## Wiring

`Wrap` and `WrapExecutorWithGate` are called at the outermost position of a
`GroupExecutor`-capable write executor chain (above the retry/timeout layer)
so a single permit covers a whole write attempt, including all retries and the
deadline. The reducer wires the canonical gate onto the semantic-entity and
canonical executors in `cmd/reducer/main.go`; the projector wires it onto the
canonical executor in `cmd/projector/runtime_wiring.go`.

`WrapExecutorWithGate` preserves whichever grouped-write interface `inner`
implements: `cypher.GroupExecutor` (the reducer's, projector's, and Neo4j's
atomic-group path) or neither (`ExecuteOnlyExecutor`). It does not have a
`cypher.PhaseGroupExecutor` case: nothing in this package's callers wraps a
bare `PhaseGroupExecutor` outermost (see the bootstrap-index section below for
why).

### bootstrap-index wiring (issue #4515, Lane B)

bootstrap-index's canonical NornicDB write path
(`bootstrapNornicDBPhaseGroupExecutor` in `cmd/bootstrap-index/nornicdb_wiring.go`)
implements `cypher.PhaseGroupExecutor`, not `cypher.GroupExecutor`, and its
`ExecutePhaseGroup` fans a single call out into up to
`ESHU_NORNICDB_ENTITY_PHASE_CONCURRENCY` concurrent `ge.ExecuteGroup` calls
against its inner `GroupExecutor` for the `entities`/`entity_containment`
phases (`executeEntityPhaseGroupConcurrently` in
`cmd/bootstrap-index/nornicdb_entity_phase_group_concurrent.go`).

Gating the outer `ExecutePhaseGroup` call would only acquire ONE permit per
call, leaving every concurrent inner `ExecuteGroup` call in that fan-out
unbounded — `ESHU_GRAPH_WRITE_MAX_IN_FLIGHT=2` would still allow up to
`ESHU_NORNICDB_ENTITY_PHASE_CONCURRENCY` (default up to 16) simultaneous Bolt
writes. bootstrap-index therefore wires the gate around the INNER
`GroupExecutor`-capable layer instead: `bootstrapCanonicalExecutorForGraphBackend`
(`cmd/bootstrap-index/nornicdb_wiring.go`) wraps `bounded` (the
`TimeoutExecutor` over the instrumented/retrying raw executor, which
implements `GroupExecutor`) with `graphbackpressure.WrapExecutorWithGate`
BEFORE assigning it to `bootstrapNornicDBPhaseGroupExecutor.inner`. Every
concurrent `ge.ExecuteGroup` call the fan-out makes then independently draws
from the same shared gate, so the ceiling bounds actual concurrent backend
writes regardless of `entityPhaseConcurrency`. The same wrap applies to the
non-NornicDB (Neo4j) return path, which is a bare `GroupExecutor` with no
fan-out wrapper.

The gate itself (`newBootstrapCanonicalGate` in
`cmd/bootstrap-index/graph_write_backpressure_wiring.go`) is constructed
exactly ONCE per bootstrap-index run, in `openBootstrapCanonicalWriter`
(`cmd/bootstrap-index/wiring.go`), and threaded into
`bootstrapCanonicalExecutorForGraphBackend` as a single shared instance. This
matters because bootstrap-index builds one canonical writer per run that is
shared by every `projector.Service` worker goroutine, so a single gate here
already bounds the whole run's in-flight canonical writes.

## Passthrough Semantics

The gate is a passthrough (inner executor returned unchanged, zero-overhead
no-op) only when the EFFECTIVE per-class ceiling resolves to non-positive.
`ClassMaxInFlight(getenv, classEnv)` reads the class-specific env
(`ESHU_GRAPH_WRITE_CANONICAL_MAX_IN_FLIGHT` for the canonical class) first,
and when that is unset, blank, non-numeric, or non-positive, FALLS BACK to the
shared `ESHU_GRAPH_WRITE_MAX_IN_FLIGHT`. This means:

- Both unset (or both non-positive): gate is nil, passthrough. This is
  bootstrap-index's shipped default.
- Only `ESHU_GRAPH_WRITE_MAX_IN_FLIGHT` set to a positive value: the canonical
  class (and, for the reducer, the semantic class too, unless split via
  per-class env — see `AnyClassMaxInFlightSet`/`AggregateMaxInFlight`) is
  bounded to that shared ceiling. It is NOT a passthrough.
- `ESHU_GRAPH_WRITE_CANONICAL_MAX_IN_FLIGHT` set to a positive value: it wins
  outright regardless of the shared knob.

An unset `ESHU_GRAPH_WRITE_CANONICAL_MAX_IN_FLIGHT` alone does **not** disable
the gate when the shared knob is set — do not read "unset" on the per-class
knob as "disabled" without also checking the shared knob.

## Telemetry

`NewObserver` records two operator-facing metrics, labeled by operation:

- `eshu_dp_graph_write_backpressure_engaged_total` — writes that blocked for a
  permit. A non-zero rate is the 3 AM signal that backpressure is active.
- `eshu_dp_graph_write_backpressure_wait_seconds` — how long a blocked write
  waited. Rising p95 is the precursor to write timeouts.

## Evidence

Conflict domain: the graph write boundary shared by all reducer/projector/
bootstrap-index workers (canonical/semantic NornicDB writes, and for
bootstrap-index specifically, every concurrent chunk in the entity-phase
fan-out). The bound is applied at the outermost `GroupExecutor`-capable
executor position for each binary, so one permit covers a whole write attempt
including retries and the deadline.

No-Regression Evidence: the bound is opt-in and defaults to disabled. With the
effective ceiling unset/non-positive, `Wrap`/`WrapExecutorWithGate` return the
inner executor unchanged (identity), so the wrapper adds no allocation, no
indirection, and no behavior change on the hot path — proven by
`TestWrapDisabledIsPassthrough` (returns the same executor pointer) and the
reducer/projector wiring tests that still observe the unwrapped
`TimeoutExecutor`/`GroupExecutor` types when the knob is unset. When enabled,
the inner write is invoked exactly as before, just gated by a permit; the
permit is released on success and on error
(`TestBackpressureExecutorReleasesPermitOnError`), so a backend stuck timing
out cannot leak permits or starve the path. `go test ./internal/storage/cypher
./internal/graphbackpressure ./cmd/bootstrap-index ./cmd/reducer -race
-count=1` (930 tests, 4 packages) proves the reducer's and projector's
existing `GroupExecutor`-based wiring is unchanged while bootstrap-index's new
inner-layer wiring correctly bounds writes.

Performance Evidence (bounded-concurrency proof, not a corpus timing run):
`TestBackpressureExecutorBoundsConcurrentWrites` drives 24 racing callers at a
ceiling of 3 and asserts peak concurrent inner writes reaches exactly the
ceiling (not less — a lower observed peak than the ceiling under a blocking
inner executor would indicate an accidental serialization regression) and
in-flight returns to 0 after drain; `TestWrapBoundsConcurrency` proves the
same through this package's `Wrap`. The grouped-write path shares the same
permit pool (`TestBackpressureExecutorGroupRespectsBound`) so `ExecuteGroup`
cannot bypass the ceiling. A blocked caller observes context cancellation
rather than hanging and does not consume a permit
(`TestBackpressureExecutorContextCancelWhileWaiting`).

For bootstrap-index specifically,
`TestBootstrapCanonicalGateBoundsConcurrentEntityFanOut`
(`cmd/bootstrap-index/nornicdb_canonical_gate_fanout_test.go`) drives
`ExecutePhaseGroup` with `entityPhaseConcurrency=8` and a gate ceiling of 2,
and asserts peak concurrent inner `ExecuteGroup` calls reaches exactly 2
(before the inner-layer fix: unbounded up to 8; after: capped at, and reaching,
2), with every chunk still executing.
`TestBootstrapCanonicalGateTerminatesUnderMixedFanOutPressure` drives a mix of
succeeding and failing concurrent chunks and asserts the whole
`ExecutePhaseGroup` call terminates within a bounded deadline, proving the
permit releases on both the success and the error path so a saturated pool
cannot deadlock.
`TestBootstrapCanonicalGateDisabledFanOutIsUnbounded` proves the default (nil
gate) path lets the fan-out reach its full configured
`entityPhaseConcurrency`, matching pre-existing behavior with the gate
disabled.

A repo-scale before/after NornicDB timing run on the full corpus (dead-letter
rate before vs. after with a tuned ceiling) is the remaining operator-side
validation tracked under #3560 and must be captured on a host with the graph
backend running; it is not reproducible in unit CI. bootstrap-index ships this
gate disabled by default because the #4515 895-repo full-corpus proof showed 0
graph-write-timeout pressure on bootstrap's canonical write path, so there is
no evidence yet that bootstrap needs this bound enabled. It exists as
insurance for a future run that does show graph-write-timeout pressure.

Observability Evidence: backpressure engagement and wait time are exported as
`eshu_dp_graph_write_backpressure_engaged_total` (counter, labeled by
operation and `gate`) and `eshu_dp_graph_write_backpressure_wait_seconds`
(histogram, labeled the same way), recorded together so their counts stay
equal. An operator reads a non-zero engaged rate as "backpressure is active"
and rising wait p95 as the precursor to write timeouts. The in-flight gauge is
observable via `BackpressureExecutor.InFlight()`. bootstrap-index's gate
reuses these existing instruments under `gate="canonical"` — the same label
the reducer and projector already emit under, so an operator distinguishes
bootstrap-index's engagement only by which binary is running.

## Why this package exists separately

The observer adapter must import both `internal/storage/cypher` and
`internal/telemetry`. It cannot live in `internal/runtime` because the cypher
package's internal tests import `internal/runtime`, which would create an import
cycle. Only the cmd layer consumes this package.
