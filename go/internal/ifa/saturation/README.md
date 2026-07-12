# internal/ifa/saturation

The Ifá Layer 3 saturation Odù. It drives more recoverable graph writes than a
permit pool admits and asserts the #3560 failure shape: the real
`cypher.BackpressureGate` engages, work retries instead of executing, nothing
dead-letters spuriously, and the queue drains to the B-12 residual (zero
non-terminal work) after pressure releases.

## Why it exists

Issue #3560 was a slow graph backend dead-lettering recoverable work: when every
reducer/projector worker drives a write at once, an oversubscribed backend times
out those writes, and the ones that keep timing out exhaust their retry budget
and dead-letter. `graph_write_timeout` is a *counting* retry class, so the fix is
not "retry forever" — it is the permit gate bounding concurrent writes to backend
capacity so a write is never oversubscribed in the first place. This package is
the permanent regression proof for that class.

## What it is

`Run(ctx, Config)` drives `Config.WorkItems` writes through a real
`cypher.BackpressureGate` against a capacity-bounded backend whose
over-subscription timeout is the real `cypher.GraphWriteTimeoutError`. The
retry-vs-dead-letter decision uses the real `reducer.IsRetryable` classification
with the counting attempt budget, and the "no spurious dead letters" assertion
reuses `ifa.DeadLetterSetsEqual`.

- `PermitPool <= 0` disables the gate (the pre-#3560 control): the surplus over
  capacity times out and floods.
- `PermitPool <= BackendCapacity` bounds in-flight so nothing is oversubscribed:
  backpressure engages, everything retries or waits, and the queue drains clean.

It is hermetic and credential-free — no Postgres, graph backend, or network. The
backend is a model; the gate, the timeout error, and the retry classifier are
the real production seams, so the assertion tracks the real code.

## Determinism

The scenario is deterministic under `-race`. The ungated flood forces every
offered write to overlap with a per-round barrier, so exactly
`WorkItems - MaxAttempts*BackendCapacity` items dead-letter. The gated round pins
the first cohort of permit-holders until the surplus has blocked on `Acquire`, so
the gate's wait observer fires without a scheduler race; the pass/fail assertions
depend on the structural `PermitPool`-vs-capacity bound, not on timing.

## Evidence

- No-Regression Evidence: `go/internal/ifa/saturation` and
  `go/internal/ifa/throughput` are hermetic conformance scenario runners
  exercised only by `go test` and the `ifa-load-saturation` gate; no runtime
  binary (ingester, reducer, api, mcp, bootstrap) imports them, so they add no
  production Cypher, graph-write, queue, lease, or worker path. The hot-path gate
  flags them only for concurrency vocabulary (permit gate, retries,
  backpressure); the real `cypher.BackpressureGate`, `cypher.GraphWriteTimeoutError`,
  and `reducer.IsRetryable` seams they call are unchanged by this PR. Baseline vs
  after, in-memory backend model (no NornicDB/Postgres), input shape 8 work items
  / capacity 2 / permit pool 2: with the gate the queue drains to dead-letter=0,
  residual=0; without it exactly `WorkItems − MaxAttempts·BackendCapacity` = 2
  items dead-letter — deterministic and stable 80/80 over `-count=20 -race`. The
  throughput Odù commits identical scope/fact totals at 1/2/4 workers over a
  4-scope amplified GCP corpus.
- No-Observability-Change: no new `eshu_dp_*` metric, span, or log is added.
  The `countingObserver` is a test-local `cypher.BackpressureObserver` used only
  inside this hermetic scenario to assert the gate engaged; it is never wired
  into a runtime service's telemetry.

## Boundaries

This package models a queue; it does not run the real reducer queue or Postgres.
The full-scale saturation calibration (how far past the pool, for how long) is
operator-gated and out of scope here — this is the hermetic shape proof.
