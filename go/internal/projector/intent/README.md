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

## Related docs

- [Package restructure](../../../../docs/internal/design/package-restructure.md)
- [Source layout](../../../../docs/public/reference/source-layout.md)
