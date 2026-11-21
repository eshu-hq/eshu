# AGENTS.md - internal/reduceradmission guidance

## Read first

1. `go/internal/reduceradmission/README.md`
2. `docs/internal/design/10882-source-backpressure.md`
3. `go/internal/workflow/source_backpressure.go` (verdict semantics)
4. `docs/internal/agent-guide.md` "Prove-The-Theory-First" for signal changes

## Invariants this package enforces

- Pacing changes when work is admitted per source only. Never reduce
  worker counts, batch sizes, or concurrency here (serialization is not
  a fix).
- Deferral is never a drop: every intent lands or the wait fails as a
  retryable admission error. Do not add a path that discards intents.
- Consult failures fail open with the debug record. Do not fail closed
  on an unreadable signal, and do not silence the record.
- The per-source leg consults per intent-batch source through
  `CheckSourceBackpressureForSource`. A batch that cannot attribute a
  source skips the leg; it must not whole-batch throttle.
- Partial config fails startup. Do not add lenient defaults for
  half-set thresholds.
- The bounded consult probe (`boundedSourceSignalQuery` in
  `internal/storage/postgres`) is the S0 verdict. Do not point the
  per-source consult back at the grouped scan without a new S0
  measurement.

## Common changes

- Add a deferral reason by extending the admission decision, wiring the
  reason through `recordDeferral`, and adding a failing test first that
  pins the reason label.
- Change a threshold by updating the loader, the env registry entries,
  and the telemetry/env reference docs in the same PR.
- Touch the signal path only after running the two S0 probes against a
  live Postgres and recording the numbers.

## Failure modes

- `source_behind` deferrals with no slowed source mean the latch is
  stuck: check `workflow_claim_pacing` rows with
  `signal = 'reducer_source'` before touching the writer.
- Admission timeouts after release mean the attempt budget times the
  max engagement wrong: recheck the arithmetic, do not raise MaxWait
  blindly.
- Whole-family throttles mean the consult resolved the wrong source set:
  the fix is in the store's family-to-source resolution, not here.

## What not to change without design review

- Do not import graph, Cypher, MCP, or query packages here. The
  dependency direction stays from the binaries into this package, not
  the reverse.
- Do not move deployment-profile bypasses into this package. Profiles
  belong to the calling binary.
