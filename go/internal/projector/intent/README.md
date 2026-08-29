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

Performance Evidence: on Apple M5 Max with Go 1.26.6 darwin/arm64 and no other
Go build or test process running, six same-command samples of
`BenchmarkAppendScopeGenerationReducerIntentsFanOut` used distinct isolated
`GOCACHE` directories. Exact base
`f172823e99a0dcedea6a295e1ce7b0ef2fbf9cf0` measured 160,813-171,019 ns/op,
74,928-74,930 B/op, and 178 allocs/op. Published extracted checkpoint
`6aab9ee0e17e7b79406ca43abb3cf609bce73a01` measured 156,646-159,664 ns/op,
74,928-74,929 B/op, and 178 allocs/op. The latency ranges overlap and the
allocation count is identical; this in-process benchmark observed no
allocation regression. Its fixture contains 5,000 interleaved source-code
decoys and produces 42 ordered intents across 44 builder probes. No graph
backend participates, and it creates no queue rows.

No-Regression Evidence: `internal/projector/azure`, `internal/projector/gcp`,
`internal/projector/kubernetes`, and `internal/projector/security` import this
contract for their extracted intent builders while root assembly passes the
shared `FactLookup`. Focused
family and ordered fan-out tests plus the full projector tree preserve exact
trigger, value, and order behavior. The exact-base fan-out measurements above
preserve the allocation count with overlapping latency ranges.

No-Observability-Change (Azure, GCP, Kubernetes, and security extractions): the
boundary adds no metric, span, log, status field, queue behavior, or runtime
setting.
Existing projection and reducer-intent enqueue telemetry remains owned by the
root projector package; the telemetry coverage verifier confirms the moved
stages remain mapped to `eshu_dp_reducer_intents_enqueued_total`.

## Related docs

- [Package restructure](../../../../docs/internal/design/package-restructure.md)
- [Source layout](../../../../docs/public/reference/source-layout.md)
