# GCP projector intents

## Purpose

This package recognizes GCP cloud-resource and relationship facts and builds
the reducer intents that materialize canonical GCP graph nodes and edges.

## Ownership boundary

The package owns only GCP trigger selection and intent values. The root
`internal/projector` package owns lookup construction and lifetime,
deterministic family assembly, projection lifecycle, queue writes, retries, and
telemetry. Reducer handlers own graph materialization and canonical resource
identity or relationship endpoint resolution.

## Exported surface

- `BuildResourceMaterializationReducerIntent` recognizes GCP resource facts.
- `BuildRelationshipMaterializationReducerIntent` recognizes GCP relationship
  facts.

See `doc.go` for the caller contract shared by both builders.

## Dependencies

Builders depend on `internal/projector/intent.FactLookup`; this package must not
import the root projector package. Both builders use the same
`gcp_resource_materialization:<scope>` acceptance-unit key so relationship
projection waits for the corresponding canonical-node readiness publication.

## Telemetry

No-Observability-Change: this package emits no signal directly. Root intent
enqueue remains covered by `eshu_dp_reducer_intents_enqueued_total`; reducer
handlers retain execution and graph-write telemetry. Moving the pure builders
does not add a queue, retry, storage, graph, span, metric, or log boundary.

## Gotchas / invariants

- A generation without the exact GCP fact kind emits no family intent.
- The earliest matching fact anchors `FactID` through the order-preserving
  lookup contract.
- A nonblank source-ref system wins; collector kind is the trimmed fallback.
- Empty generations and duplicate matching facts follow the neutral lookup
  contract: no match emits no intent, and the earliest match wins.
- Root owns family assembly order and deterministic final sorting; reducer
  queue contracts continue to own retries and idempotency.

## Performance

The extraction keeps lookup construction in root and passes the same concrete
`intent.FactLookup` value to each GCP builder. The same-shape performance gate
is `BenchmarkAppendScopeGenerationReducerIntentsFanOut`: 5,000 interleaved
source-code decoys plus trigger facts producing 42 ordered intents across 44
builder probes. On Apple M5 Max with Go 1.26.5 darwin/arm64, six runs at base
`c26cd5b9977dc356781e16843940ee3f3c14684f` measured 152,304-153,338 ns/op,
74,928 B/op, and 178 allocs/op. Six same-host post-move runs measured
153,797-159,567 ns/op, 74,928 B/op, and 178 allocs/op. Allocation and byte
counts are identical; timing remains in the same narrow operating band. No
graph backend or queue participates in this in-process benchmark.

## Verification

Run the package tests, root ordered fan-out parity and probe-count tests, the
full projector package tree, selected Ifá and CI-gate path mirrors, dirgate and
telemetry coverage checks, and the six-run exact same benchmark.

## Related docs

- [Projector architecture](../README.md)
- [Intent contract](../intent/README.md)
- [GCP resource design](../../../../docs/internal/gcp-cloud-resource-materialization-design.md)
- [GCP relationship design](../../../../docs/internal/gcp-cloud-relationship-edge-materialization-design.md)
- [Package restructure](../../../../docs/internal/design/package-restructure.md)
