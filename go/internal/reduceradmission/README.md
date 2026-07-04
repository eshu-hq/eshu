# reduceradmission

## Purpose

`reduceradmission` wraps a `projector.ReducerIntentWriter` with a two-gate
backpressure policy so a producer (the ingester's collector/projection
workers, or bootstrap-index's one-shot projector) slows itself down instead of
piling recoverable work into an already-overloaded reducer queue or a
timing-out graph backend.

This was originally ingester-only logic (`cmd/ingester/reducer_admission.go`).
It moved to `internal/reduceradmission` so bootstrap-index can wrap its own
reducer intent writer with the identical policy (issue #4515: bootstrap-index
had no admission gate at all, so a bootstrap run could drive unbounded reducer
queue growth that the ingester would have throttled).

## The two gates

1. **Graph-write-pressure gate** (leading indicator). Counts only retrying
   reducer rows whose `failure_class` is `graph_write_timeout`
   (`GraphWriteTimeoutFailureClass`), read via a failure-class-scoped depth
   query rather than the stage/status-only `QueueDepths` bucket. Deferring
   engages at `RetryingHighWaterMark` and releases only below
   `RetryingLowWaterMark` — hysteresis so the producer does not flap on every
   partial recovery. Scoping to the graph-write-timeout class specifically is
   the issue #3560 fix: a backlog of readiness-not-ready retrying rows
   (`secrets_iam_endpoint_not_ready` and other `*_n` classes) reports zero
   graph-write-timeout depth and therefore never false-throttles unrelated
   admission.
2. **Total-depth gate** (trailing safeguard). Defers once total outstanding
   reducer queue depth (all statuses, all failure classes) reaches
   `HighWaterMark`.

Both gates are independently configurable and independently disable-able
(`HighWaterMark <= 0` disables gate 2; `RetryingHighWaterMark <= 0` disables
gate 1). The gate is a no-op — `WrapIntentWriter` returns the inner writer
unchanged — when both are disabled.

## Configuration

| Env var | Default | Meaning |
| --- | --- | --- |
| `ESHU_REDUCER_ADMISSION_HIGH_WATER_MARK` | `10000` | Total-depth gate ceiling. `0` disables gate 2. |
| `ESHU_REDUCER_ADMISSION_POLL_INTERVAL` | `1s` | How long a deferred Enqueue call sleeps before re-checking depth. |
| `ESHU_REDUCER_ADMISSION_RETRYING_HIGH_WATER_MARK` | `500` | Graph-write-pressure gate engage threshold. `0` disables gate 1. |
| `ESHU_REDUCER_ADMISSION_RETRYING_LOW_WATER_MARK` | `100` | Graph-write-pressure gate release threshold. Must be less than the high mark; an unset or out-of-range low mark is clamped to one fifth of the high mark. |

Both the ingester and bootstrap-index read the same env vars, so an operator
tunes admission behavior once for both binaries.

## Wiring

`WrapIntentWriter(database postgres.Queryer, inner projector.ReducerIntentWriter, getenv, instruments, logger)`
takes the narrow read-only `postgres.Queryer` surface — it only constructs a
`postgres.NewQueueObserverStore` for depth reads, never writes. Callers build
their own reducer intent writer (`postgres.NewReducerQueue` or an
ingester-local lightweight bypass) and pass it as `inner`; this package never
constructs the underlying writer itself.

- The ingester wires this in `cmd/ingester/reducer_admission.go`
  (`ingesterReducerIntentWriter`), which additionally applies the
  local-lightweight bypass (`ESHU_QUERY_PROFILE=local_lightweight` or
  `ESHU_DISABLE_NEO4J=true`) before calling `WrapIntentWriter`. That bypass is
  ingester-specific; bootstrap-index has no equivalent concept.
- bootstrap-index wires this in `cmd/bootstrap-index/wiring.go`, wrapping the
  `reducerQueue` it already constructs before assigning it to
  `projector.Runtime.IntentWriter`.

## Concurrency

The graph-write-pressure hysteresis flag (`deferralState`) is shared via
pointer across all copies of the returned writer value and guarded by a mutex,
because both producers run concurrent projection workers against one admission
writer. `TestReducerAdmissionGraphWritePressureConcurrentEnqueueShareState`
proves this is race-free; run it with `-race`.

## Verification

```bash
cd go && go test ./internal/reduceradmission/... -count=1
cd go && go test ./internal/reduceradmission/... -count=1 -race
cd go && golangci-lint run ./internal/reduceradmission/...
```
