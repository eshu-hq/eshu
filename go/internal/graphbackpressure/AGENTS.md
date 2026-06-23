# AGENTS: graphbackpressure

Scoped agent instructions for `go/internal/graphbackpressure`.

## What this package owns

The graph write-path backpressure wiring: the `ESHU_GRAPH_WRITE_MAX_IN_FLIGHT`
operator knob (`MaxInFlight`), the telemetry observer adapter (`NewObserver`),
and the `Wrap` helper that both the reducer and projector binaries call to bound
in-flight graph writes (issue #3560).

## Rules

- Keep this package free of an import cycle. It may import
  `internal/storage/cypher` and `internal/telemetry`. It MUST NOT be imported by
  either of those, nor by `internal/runtime` (whose test surface the cypher
  package imports). Only the cmd layer consumes it.
- `Wrap` MUST sit at the outermost position of the write executor chain (above
  retry/timeout) so one permit covers a whole write attempt. Do not move it
  below the timeout layer.
- A non-positive ceiling MUST stay a true passthrough (return inner unchanged):
  no wrapper, no indirection, preserving the inner executor's interfaces.
- This is not a serialization fix. Do not lower the default or cap the ceiling
  to a small constant to paper over write-conflict or non-idempotent writes.
  Per repo policy, the bound is a configurable headroom limit, not a single-flight
  drain.
- The engaged counter and wait histogram are recorded together so their counts
  stay equal. Keep that invariant if you add labels.

## Verification

```bash
cd go && go test ./internal/graphbackpressure/... -count=1
cd go && go test ./internal/graphbackpressure/... -count=1 -race
cd go && golangci-lint run ./internal/graphbackpressure/...
```

Changes that touch the env knob, metric names, or wiring position also require
re-running the reducer/projector wiring tests and updating
`docs/public/reference/telemetry/metrics-reducer-storage.md`.
