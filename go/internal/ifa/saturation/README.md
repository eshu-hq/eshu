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

## Boundaries

This package models a queue; it does not run the real reducer queue or Postgres.
The full-scale saturation calibration (how far past the pool, for how long) is
operator-gated and out of scope here — this is the hermetic shape proof.
