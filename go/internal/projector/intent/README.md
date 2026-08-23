# Projector intent contract

## Purpose

This package defines the small contract shared by projector assembly and the
intent-family packages that assembly calls.

## Ownership boundary

The package owns the reducer-intent value and the read-only fact-selection
port. The root `internal/projector` package still owns fact indexing, family
order, projection lifecycle, writes, retries, and telemetry.

## Exported surface

- `ReducerIntent` carries one reducer queue work item.
- `FactLookup` gives family builders order-preserving reads over one immutable
  fact generation.

See `doc.go` for the godoc-rendered contract.

## Dependencies

The contract imports `internal/facts` for fact envelopes and `internal/reducer`
for the stable reducer domain type. It does not import the root projector
package.

## Telemetry

This package emits no metrics, spans, or logs. The root projector records
projection and reducer-intent enqueue telemetry.

## Gotchas / invariants

`FactLookup` methods that inspect several kinds must return the earliest
accepted fact in the original generation order. A caller's kind argument order
must not change which fact wins.

No-Regression Evidence: a disposable `internal/projector/packageregistry`
package imported this contract and held the complete package-source-correlation
intent builder while root assembly called back through `FactLookup`. The four
focused contract, family, and ordered fan-out tests passed, followed by
`go test ./internal/projector/... -count=1`, `go build ./...`, and
`go vet ./...`; every command exited 0. The scratch family package was then
removed. The committed boundary changes no intent values or assembly order,
which the 42-domain ordered characterization now checks directly. No graph
backend, queue row, or runtime setting participates in this compile proof.

No-Observability-Change: the boundary adds no metric, span, log, status field,
queue behavior, or runtime setting. Existing projection and reducer-intent
enqueue telemetry remains owned by the root projector package.

## Related docs

- [Package restructure](../../../../docs/internal/design/package-restructure.md)
- [Source layout](../../../../docs/public/reference/source-layout.md)
