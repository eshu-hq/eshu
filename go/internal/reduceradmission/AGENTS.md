# AGENTS: reduceradmission

Scoped agent instructions for `go/internal/reduceradmission`.

## What this package owns

The reducer-intent admission backpressure gate: `WrapIntentWriter` wraps a
`projector.ReducerIntentWriter` with the two-gate policy (graph-write-pressure
+ total-depth), `LoadConfig` parses the operator env knobs, and the
`Deferral*`/`GraphWriteTimeoutFailureClass` constants are the canonical
telemetry/log labels. Both `cmd/ingester` and `cmd/bootstrap-index` call this
package so admission behavior is identical across both producers (issue #4515
parity contract).

## Rules

- **Preserve the `postgres.Queryer` vs `postgres.ExecQueryer` distinction.**
  `WrapIntentWriter` takes `postgres.Queryer` (read-only) because it only
  constructs `postgres.NewQueueObserverStore` for depth reads. Do NOT widen
  this to `postgres.ExecQueryer` or collapse the two interfaces — callers that
  need to construct the underlying reducer queue writer (which does need
  `ExecQueryer`) do so themselves before calling `WrapIntentWriter`.
- **Do not move the ingester's local-lightweight bypass into this package.**
  `ingesterLocalLightweight` / `ESHU_QUERY_PROFILE=local_lightweight` /
  `ESHU_DISABLE_NEO4J=true` is an ingester-only concept
  (`cmd/ingester/local_lightweight.go`). bootstrap-index has no equivalent and
  must not gain one implicitly through this package.
- **The graph-write-pressure gate MUST stay scoped to
  `GraphWriteTimeoutFailureClass`.** It must read
  `ReducerGraphWriteTimeoutDepth`, not the general `QueueDepths` retrying
  bucket, or a readiness-not-ready backlog (`secrets_iam_endpoint_not_ready`
  and other `*_n` classes) will false-throttle unrelated admission again
  (regression of issue #3560).
- **Keep the hysteresis invariant**: `RetryingLowWaterMark < RetryingHighWaterMark`
  whenever the graph-write-pressure gate is enabled. `LoadConfig` validates
  this and clamps an unset/out-of-range low mark; do not remove that
  validation.
- **`deferralState` must stay pointer-shared and mutex-guarded.** Both
  producers run concurrent projection workers sharing one admission writer
  value. Copying the writer value must not fork the hysteresis flag.
- A writer with both gates disabled MUST return the inner writer unchanged
  (no wrapper indirection) — this keeps the disabled-gate path a true no-op.

## Verification

```bash
cd go && go test ./internal/reduceradmission/... -count=1
cd go && go test ./internal/reduceradmission/... -count=1 -race
cd go && golangci-lint run ./internal/reduceradmission/...
```

Changes here also require re-running `cmd/ingester` and `cmd/bootstrap-index`
wiring tests, since both call `WrapIntentWriter` directly.
