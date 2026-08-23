# Projector intent contract

## Purpose

This package defines the small contract that root projector assembly and
extracted intent-family packages share.

## Ownership boundary

The package owns the reducer-intent value and the immutable fact lookup. Root
`internal/projector` constructs one lookup per generation and owns its lifetime,
family order, projection lifecycle, writes, retries, and telemetry.

## Exported surface

- `ReducerIntent` carries one reducer queue work item.
- `FactLookup` is the concrete, order-preserving index family builders read.
- `NewFactLookup` constructs that index once for an immutable fact generation.
- `SourceSystem` applies the shared source-ref-first, collector-fallback label
  rule without requiring a family to import root projector assembly.

See `doc.go` for the godoc-rendered contract.

## Dependencies

The contract imports `internal/facts` for fact envelopes and `internal/reducer`
for the stable reducer domain type. It does not import the root projector
package.

## Telemetry

This package emits no metrics, spans, or logs. The root projector records
projection and reducer-intent enqueue telemetry.

## Gotchas / invariants

`FactLookup` methods that inspect several kinds return the earliest accepted
fact in the original generation order. A caller's kind argument order must not
change which fact wins. The concrete value is intentional: the interface form
added one heap allocation per extracted builder on the 44-probe fan-out.

Performance Evidence: on Apple M5 Max with Go 1.26.5 darwin/arm64, six isolated
runs of `BenchmarkAppendScopeGenerationReducerIntentsFanOut` used the same
fixture before and after the move: 5,000 interleaved source-code decoys plus the
trigger facts that produce 42 ordered intents across 44 builder probes. Base
`62a40a802ae79e4bd75ba9cbb8a0b128a6801b92` measured
153,775-156,662 ns/op, 74,928-74,929 B/op, and 178 allocs/op. The extracted
boundary measured 153,377-155,926 ns/op, 74,928-74,930 B/op, and 178 allocs/op.
No graph backend participates in this in-process benchmark, and it creates no
queue rows; the terminal count is the parity test's unchanged 42 intents.

No-Regression Evidence: `internal/projector/azure` imports this contract for
its complete resource and relationship intent builders while root assembly
passes the shared `FactLookup`. Focused family and ordered fan-out tests plus the
full projector tree preserve exact trigger, value, and order behavior. The
measurement above shows no allocation or byte regression and comparable CPU
cost on the same host and fixture.

No-Observability-Change: the boundary adds no metric, span, log, status field,
queue behavior, or runtime setting. Existing projection and reducer-intent
enqueue telemetry remains owned by the root projector package; the telemetry
coverage verifier confirms the moved stages remain mapped to
`eshu_dp_reducer_intents_enqueued_total`.

## Related docs

- [Package restructure](../../../../docs/internal/design/package-restructure.md)
- [Source layout](../../../../docs/public/reference/source-layout.md)
