# Azure projector intents

## Purpose

This package recognizes Azure cloud-resource and relationship facts and builds
the reducer intents that materialize canonical Azure graph nodes and edges.

## Ownership boundary

The package owns only Azure trigger selection and intent values. The root
`internal/projector` package owns lookup construction and lifetime,
deterministic family assembly, projection lifecycle, queue writes, retries, and
telemetry. Reducer handlers own graph materialization and ARM-ID endpoint
resolution.

## Exported surface

- `BuildResourceMaterializationReducerIntent` recognizes Azure resource facts.
- `BuildRelationshipMaterializationReducerIntent` recognizes Azure relationship
  facts.

## Dependencies

Builders depend on `internal/projector/intent.FactLookup`; this package must not
import the root projector package. Both builders use the same
`azure_resource_materialization:<scope>` acceptance-unit key so relationship
projection waits for the corresponding canonical-node readiness publication.

## Gotchas / invariants

- A generation without the exact Azure fact kind emits no family intent.
- The earliest matching fact anchors `FactID` through the order-preserving
  lookup contract.
- A nonblank source-ref system wins; collector kind is the trimmed fallback.
- Empty generations and duplicate matching facts follow the neutral lookup
  contract: no match emits no intent, and the earliest match wins. Root owns
  family assembly order; reducer queue contracts continue to own retries.

## Telemetry

This package emits no signal directly. Root intent enqueue remains covered by
`eshu_dp_reducer_intents_enqueued_total`; reducer handlers retain execution and
graph-write telemetry.

## Verification

Run the package tests, the root Azure assembly tests, ordered fan-out parity,
`go test ./internal/projector/... -count=1`, and the golden-corpus gates selected
for projector changes.

## Related docs

- [Projector architecture](../README.md)
- [Intent contract](../intent/README.md)
- [Package restructure](../../../../docs/internal/design/package-restructure.md)
