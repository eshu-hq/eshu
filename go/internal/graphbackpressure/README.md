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

`Wrap` and `WrapExecutorWithGate` are called at the outermost position of the
write executor chain (above the retry/timeout layer) so a single permit covers
a whole write attempt, including all retries and the deadline. The reducer
wires the canonical gate onto the semantic-entity and canonical executors in
`cmd/reducer/main.go`; the projector wires it onto the canonical executor in
`cmd/projector/runtime_wiring.go`; bootstrap-index wires the same canonical
gate onto its canonical executor in
`cmd/bootstrap-index/graph_write_backpressure_wiring.go` (issue #4515, Lane B),
reusing `ESHU_GRAPH_WRITE_MAX_IN_FLIGHT` / `ESHU_GRAPH_WRITE_CANONICAL_MAX_IN_FLIGHT`.

`WrapExecutorWithGate` preserves whichever grouped-write interface `inner`
implements: `cypher.GroupExecutor` (the reducer's and Neo4j's atomic-group
path), `cypher.PhaseGroupExecutor` (bootstrap-index's NornicDB canonical
executor, `bootstrapNornicDBPhaseGroupExecutor`, which bounds writes per
dependency phase rather than atomically), or neither (`ExecuteOnlyExecutor`).
Without the `PhaseGroupExecutor` case, wrapping a phase-group-only executor
would silently strip `ExecutePhaseGroup` and degrade
`CanonicalNodeWriter.Write` to its per-statement sequential fallback — a
forbidden serialization regression the project's "Serialization Is Not A Fix"
rule bars.

A non-positive `ESHU_GRAPH_WRITE_MAX_IN_FLIGHT` (and, for bootstrap-index and
the reducer's canonical class, an unset `ESHU_GRAPH_WRITE_CANONICAL_MAX_IN_FLIGHT`)
returns the inner executor unchanged, so the wrapper is a zero-overhead no-op
until an operator opts in. bootstrap-index ships with the gate disabled by
default.

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

## Bootstrap-index PhaseGroupExecutor Wiring (issue #4515, Lane B)

Conflict domain: bootstrap-index's canonical NornicDB graph write path
(`bootstrapNornicDBPhaseGroupExecutor` in `cmd/bootstrap-index/wiring.go`),
which implements `cypher.PhaseGroupExecutor` (`ExecutePhaseGroup`), not
`cypher.GroupExecutor`. Before this change, `WrapExecutorWithGate` recognized
only `GroupExecutor`, so wrapping this executor outermost fell through to
`ExecuteOnlyBackpressureExecutor`, which `CanonicalNodeWriter.Write` cannot
distinguish from a plain `Executor` — every bootstrap canonical write would
have silently degraded to the per-statement sequential `Execute` fallback, a
serialization regression the "Serialization Is Not A Fix" rule forbids.

No-Regression Evidence: `BackpressureExecutor.ExecutePhaseGroup` (added to
`internal/storage/cypher/backpressure_executor.go`) mirrors `ExecuteGroup`
exactly — one permit acquired for the whole call, released on completion,
covering retries and the deadline
(`TestBackpressureExecutorPhaseGroupRespectsBound`,
`TestBackpressureExecutorPhaseGroupReleasesPermitOnError`). It fails closed
(`errInnerNoExecutePhaseGroup`) rather than silently degrading when the inner
executor lacks phase-group support
(`TestBackpressureExecutorPhaseGroupErrorsWhenInnerLacksSupport`).
`WrapExecutorWithGate`'s new fourth case
(`TestWrapExecutorWithGatePreservesPhaseGroupExecutor`) proves the wrapped
value still implements `cypher.PhaseGroupExecutor` and routes calls through to
the inner executor rather than degrading; the bootstrap-index wiring test
(`TestBoundBootstrapCanonicalExecutorPreservesPhaseGroupExecutor`) proves the
same end-to-end through `newBootstrapGraphWriteGate(...).boundExecutor`. The
existing `GroupExecutor` and `ExecuteOnlyExecutor` cases, and the reducer's
`GroupExecutor`-based wiring, are unchanged and covered by the full existing
`internal/storage/cypher`, `internal/graphbackpressure`, and `cmd/reducer`
suites (`go test ./internal/storage/cypher ./internal/graphbackpressure
./cmd/bootstrap-index ./cmd/reducer -race -count=1`, 936 tests, 4 packages).

Performance Evidence (bounded-concurrency proof; bootstrap ships this gate
disabled by default because the #4515 895-repo full-corpus proof showed 0
graph-write-timeout pressure, so no corpus-scale before/after timing applies):
`TestWrapExecutorWithGatePhaseGroupBoundsConcurrency` and
`TestBoundBootstrapCanonicalExecutorBoundsPeakConcurrency` drive 10 concurrent
`ExecutePhaseGroup` callers at a ceiling of 2 and assert peak concurrent inner
calls never exceeds 2 (before: unbounded, up to 10 concurrent; after: capped at
2) while every caller still completes.
`TestWrapExecutorWithGatePhaseGroupDeadlockFreeUnderPressure` and
`TestBoundBootstrapCanonicalExecutorTerminatesUnderMixedPressure` drive 20
concurrent callers (a mix of blocking-then-succeeding and immediately-failing
with a graph-write-timeout-shaped error) at a ceiling of 3 and assert the whole
run terminates within a 10s deadline with peak concurrency never exceeding 3 —
proving the permit releases on both the success and the error/timeout path, so
a saturated pool cannot deadlock or starve remaining callers.
`TestWrapExecutorWithGatePhaseGroupDisabledIsPassthrough` and
`TestBoundBootstrapCanonicalExecutorDisabledIsPassthrough` prove the default
(unset ceiling) path returns the inner executor unchanged (identity), so
bootstrap-index behavior is byte-identical until an operator opts in.

Observability Evidence: bootstrap-index's gate reuses the existing
`eshu_dp_graph_write_backpressure_engaged_total{gate="canonical"}` counter and
`eshu_dp_graph_write_backpressure_wait_seconds{gate="canonical"}` histogram via
`graphbackpressure.NewGate(..., CanonicalGateName)` — the same instruments and
`gate="canonical"` label the reducer and projector already emit under. No new
metric, span, or log field is introduced; an operator distinguishes
bootstrap-index's engagement only by which binary is running, matching how the
reducer's and projector's canonical gates are already indistinguishable from
each other in this shared instrument.

## Why this package exists separately

The observer adapter must import both `internal/storage/cypher` and
`internal/telemetry`. It cannot live in `internal/runtime` because the cypher
package's internal tests import `internal/runtime`, which would create an import
cycle. Only the cmd layer consumes this package.
