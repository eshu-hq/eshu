# Governance Audit Async Appender

## Purpose

`governanceauditasync` decouples a hot request path from the durable
governance-audit store. It exists specifically for the F-9 (#5170) allowed-read
audit emission: adding a synchronous Postgres round trip to every successful
MCP read would couple read latency to Postgres health (see
`docs/internal/design/1900-hosted-governance-policy-model.md` and the F-9
design addendum). `AsyncAppender` buffers events in a bounded channel and
flushes them from one background worker, trading a bounded, observable amount
of best-effort data loss for a sub-microsecond enqueue cost on the caller's
path.

## Ownership boundary

This package owns the buffering, batching, drop, and shutdown-flush policy for
best-effort audit events. It does not own event shape validation (that is
`governanceaudit`), durable storage (that is the caller's `Appender`, normally
`storage/postgres.GovernanceAuditStore`), or the decision of which requests get
audited (that is the caller — see `go/internal/query/auth_audit.go`).

## Exported surface

See `doc.go` for the godoc contract.

- `AsyncAppender` implements the same `Append(ctx, []governanceaudit.Event)
  error` shape as the durable store, so it can be used anywhere a synchronous
  appender is expected.
- `NewAsyncAppender(sink, metrics, opts...)` constructs a running appender; the
  background worker starts immediately.
- `Appender` is the minimal durable-sink interface `AsyncAppender` flushes to.
- `Metrics` holds the three drop-observability counters the caller registers
  against `go/internal/telemetry`.
- `Option` values (`WithBufferCapacity`, `WithBatchMax`, `WithFlushTimeout`,
  `WithShutdownTimeout`) override the defaults for tests and non-default
  deployments.
- `Close()` stops accepting new events, drains the buffer, performs a final
  bounded flush, and returns once the worker has exited or the shutdown
  timeout elapses.

## Semantics callers must understand

- **Never blocks.** `Append` always returns quickly: a struct copy plus one
  non-blocking channel send per event. A full buffer or a closed appender
  drops the event and increments `Metrics.Dropped`; it never applies
  backpressure.
- **Single worker, FIFO.** Exactly one goroutine drains the buffer, so events
  enqueued by a single caller are flushed to the sink in the order they were
  enqueued. Cross-goroutine ordering across concurrent callers is not
  guaranteed (never was, for audit events — see `Event.OccurredAt`).
- **Best-effort persistence.** A batch the sink fails to persist is dropped
  after incrementing `Metrics.PersistFailures`; there is no retry queue.
  Allowed-read events are corroborating evidence, not the security-critical
  durable class (denials remain synchronous elsewhere).
- **Bounded shutdown.** `Close()` is safe to call once or many times. It
  returns within `WithShutdownTimeout` (default 5s) regardless of sink
  behavior, so a stuck sink cannot hang process shutdown.

## Dependencies

`governanceaudit` (the `Event` type) and `go.opentelemetry.io/otel/metric`
(the counter type used by `Metrics`). No dependency on `query`, `storage`, or
any concrete sink implementation — callers provide the sink as an `Appender`.

## Telemetry

Three counters, registered by the caller in `go/internal/telemetry` and
passed in via `Metrics`:

- `eshu_dp_governance_audit_allowed_emitted_total` — events accepted into the
  buffer.
- `eshu_dp_governance_audit_allowed_dropped_total` — events rejected because
  the buffer was full or the appender was closed. Non-zero means governance
  data loss is happening.
- `eshu_dp_governance_audit_allowed_persist_failures_total` — events accepted
  but the sink's `Append` call failed. Non-zero means the durable store itself
  is rejecting or unreachable for these events.

A nil `Metrics` field is a safe no-op (tests may omit counters they do not
assert on).

## Gotchas / invariants

- Do not close the caller-owned `sink`; `AsyncAppender.Close()` only stops its
  own worker and buffer.
- `Append`'s returned error is always `nil` — callers that need to observe
  loss must read the `Dropped`/`PersistFailures` counters, not the return
  value.
- The buffer's channel is never closed directly (only a separate signal
  channel is); enqueue-after-`Close()` is guarded by that signal so it drops
  cleanly instead of panicking on a closed channel.

## Related docs

- `docs/internal/design/1900-hosted-governance-policy-model.md`
- `go/internal/governanceaudit/README.md`
