# AGENTS.md - internal/governanceauditasync guidance

## Read first

1. `go/internal/governanceauditasync/README.md` - purpose, ownership boundary,
   and semantics callers must understand.
2. `go/internal/governanceauditasync/doc.go` - godoc contract.
3. `go/internal/governanceauditasync/appender.go` - buffering, batching, drop,
   and shutdown-flush implementation.
4. `go/internal/governanceaudit/README.md` - the event shape this package
   moves; that package explicitly forbids adding storage or telemetry to
   itself, which is why this package exists separately.

## Invariants this package enforces

- **Append never blocks.** No lock, retry, or backpressure path may make
  `Append` wait on the sink or on buffer space. A full buffer must drop, not
  block.
- **Exactly one worker.** Do not add a worker pool; a single Postgres sink and
  the FIFO-within-process ordering guarantee both depend on one drain
  goroutine.
- **Bounded shutdown.** `Close()` must return within its configured timeout
  even if the sink hangs. Do not remove the outer timeout guard around the
  worker-done wait.
- **No storage or validation logic here.** This package flushes to whatever
  `Appender` the caller provides; it must not import
  `storage/postgres`, gain SQL-shaped knowledge, or duplicate
  `governanceaudit.NormalizeEvent`'s validation.

## Common changes and how to scope them

- Changing buffer capacity, batch size, or timeouts: use the existing
  `Option` functions; do not hardcode a second set of constants.
- Adding a new drop-observability signal: add a field to `Metrics`, thread it
  through the same nil-guarded `Add` pattern as the existing three counters,
  and update `go/internal/telemetry/instruments.go` plus
  `docs/public/observability/telemetry-coverage.md` in the same change (see
  `.claude/skills/telemetry-coverage-discipline`).
- Adding a new consumer beyond the F-9 allowed-read audit path: keep the
  `Appender` interface as the only coupling point; do not have this package
  import the consumer's package.

## Failure modes and how to debug

- Non-zero `..._dropped_total` in production: the buffer is undersized for
  traffic, or the sink is persistently slow. Increase `WithBufferCapacity` or
  investigate sink latency; do not silently raise the drop tolerance without
  recording why.
- Non-zero `..._persist_failures_total`: the durable sink itself is rejecting
  batches (bad schema, bad connection, validation failure inside the sink).
  Check the sink's own error, not this package.
- `Close()` returning `ErrShutdownFlushIncomplete`: the sink did not finish
  its flush within the shutdown timeout. The worker goroutine may still be
  running in the background; this is a diagnostic signal, not a panic.

## What not to change without review

- Do not add a retry queue for failed batches; the design addendum explicitly
  rejects retry for this best-effort class (allowed-read events are
  corroborating evidence, not the durable security-critical class). The
  per-event fallback in `flush` is NOT a retry queue: it is a single
  isolation pass that appends a failed batch's events individually so one
  poison event does not drop its well-formed siblings (the durable store's
  Append is all-or-nothing). Keep it a single pass — do not re-enqueue or
  loop failed events.
- Do not make `Append`'s return value carry drop/failure information; callers
  must read the counters.
